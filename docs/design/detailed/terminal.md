# 詳細設計: Terminal インターフェース & WezTerminal (v2)

## 概要

`internal/terminal/` が担うターミナル抽象化レイヤーの v2 仕様。
主な変更点は **Pane 型の int 化**・**CWD 正規化**・**FocusPane シグネチャ変更**・**JSON パース簡素化** の 4 点。

---

## v1 との差分サマリ

| 項目 | v1 | v2 |
|------|----|----|
| `Pane.ID` 型 | `string` | `int` |
| `Pane.TabID` 型 | `string` | `int` |
| `Pane.TTYName` | なし | `string` を追加 |
| `Pane.IsActive` | なし | `bool` を追加 |
| `Pane.Workspace` | なし | `string` を追加 |
| CWD 正規化 | なし | `ListPanes()` 内で実施 |
| `FocusPane` 引数 | `string` | `int` |
| JSON パース方式 | `json.RawMessage` + 型分岐 | 直接 `int` アンマーシャル |

---

## Terminal インターフェース (v2)

```go
// Terminal は各ターミナル実装で共通利用するインターフェースを定義する。
type Terminal interface {
    // ListPanes はターミナル上の全ペイン情報を返す。CWD は正規化済み。
    ListPanes() ([]Pane, error)
    // FocusPane は指定 paneID のペインをアクティブにする。
    FocusPane(paneID int) error
    // GetPaneText は指定ペインの画面テキスト末尾を返す。
    GetPaneText(paneID int) (string, error)
    // IsAvailable はターミナルが利用可能かを返す。
    IsAvailable() bool
    // Name はターミナル識別子（例: "wezterm"）を返す。
    Name() string
}
```

---

## Pane 型 (v2)

```go
// Pane はターミナルのペイン（タブ情報含む）を表す。
type Pane struct {
    ID         int    // WezTerm CLI が返す数値 pane_id
    Title      string // ペインタイトル
    TabID      int    // WezTerm CLI が返す数値 tab_id
    WorkingDir string // 正規化済み CWD (file:// プレフィックスなし)
    TTYName    string // TTY デバイス名 (例: /dev/ttys003)
    IsActive   bool   // そのペインがフォーカス中か
    Workspace  string // WezTerm ワークスペース名 (例: "baton", "default")
}
```

### フィールド追加の理由

| フィールド | 追加理由 |
|-----------|---------|
| `TTYName` | Ambiguous セッションのサブメニューで TTY を表示し、ユーザーがペインを識別できるようにする |
| `IsActive` | 将来的なアクティブペインの自動選択、デバッグ表示に使用 |
| `Workspace` | ワークスペース駆動運用において、プロジェクトのグルーピングキーおよび表示名として使用する |

---

## CWD 正規化

### 背景

WezTerm CLI が返す `cwd` フィールドは `file://` スキームを含む URI 形式の場合がある。
Go 側のパス比較でプレフィックスが混在すると Project のマッチングが壊れるため、
`ListPanes()` の内部で正規化してから返す。

### 正規化ルール

| 入力例 | 出力例 |
|--------|--------|
| `file:///Users/foo/baton` | `/Users/foo/baton` |
| `file://localhost/Users/foo/baton` | `/Users/foo/baton` |
| `/Users/foo/baton/` | `/Users/foo/baton` |
| `/Users/foo/baton` | `/Users/foo/baton` |
| `""` | `""` |

### 実装

```go
// normalizeCWD は file:// URI を絶対パスに正規化し、末尾スラッシュを除去する。
func normalizeCWD(cwd string) string {
    switch {
    case strings.HasPrefix(cwd, "file://localhost/"):
        cwd = cwd[len("file://localhost"):]
    case strings.HasPrefix(cwd, "file://"):
        cwd = cwd[len("file://"):]
    }
    // ルートパス "/" が空文字にならないよう、長さ 1 以下では除去しない
    if len(cwd) > 1 {
        cwd = strings.TrimRight(cwd, "/")
    }
    return cwd
}
```

`ListPanes()` の返却前に各 Pane に適用する：

```go
pane.WorkingDir = normalizeCWD(rawPane.WorkingDir)
```

### 空文字列の扱い

CWD が空の場合はそのまま空文字列として返す。
呼び出し側（state.go など）でプロジェクト名が特定できない場合は `(unknown)` を使用する。

---

## WezTerminal 実装 (v2)

### ListPanes の JSON パース変更

v1 では `json.RawMessage` を使い文字列/数値の両方に対応していたが、
WezTerm CLI は常に数値で `pane_id` / `tab_id` を返すことが確認されたため、直接 `int` でアンマーシャルする。

```go
var rawPanes []struct {
    ID         int    `json:"pane_id"`
    Title      string `json:"title"`
    TabID      int    `json:"tab_id"`
    WorkingDir string `json:"cwd"`
    TTYName    string `json:"tty_name"`
    IsActive   bool   `json:"is_active"`
    Workspace  string `json:"workspace"`
}
if err := json.Unmarshal(out, &rawPanes); err != nil {
    return nil, err
}
```

変換後：

```go
panes = append(panes, Pane{
    ID:         rawPane.ID,
    Title:      rawPane.Title,
    TabID:      rawPane.TabID,
    WorkingDir: normalizeCWD(rawPane.WorkingDir),
    TTYName:    rawPane.TTYName,
    IsActive:   rawPane.IsActive,
    Workspace:  rawPane.Workspace,
})
```

### FocusPane シグネチャ変更

v2 では同一ワークスペース内の直接フォーカスに加え、別ワークスペースへの切り替えにも対応する（詳細は ADR-0007 参照）。

```go
// FocusPane は指定ペインをアクティブ化する。
// 別ワークスペースのペインの場合は、トリガーファイル経由で WezTerm Lua に
// ワークスペース切り替えを依頼してから activate-pane を実行する。
func (w *WezTerminal) FocusPane(paneID int) error {
    // 対象ペインと現在ペインのワークスペースを比較する
    // 別 WS の場合: /tmp/wezterm-alfred-workspace.json にトリガーを書き込み、
    //               Lua の SwitchToWorkspace 完了を 2 秒待機してから activate-pane
    // 同一 WS の場合: 直接 activate-pane
}
```

### GetPaneText

ToolUse 承認待ち検出のために追加したメソッド。`wezterm cli get-text` でペインの画面テキストを取得し、末尾 30 行を返す。

```go
// GetPaneText は指定ペインの画面テキスト末尾を返す。
func (w *WezTerminal) GetPaneText(paneID int) (string, error) {
    out, err := w.execFn("cli", "get-text", "--pane-id", strconv.Itoa(paneID))
    // 末尾 30 行を返す（承認プロンプトの検出に十分）
}
```

state.go はこのテキストを取得し、`Allow`・`y/n` 等の承認プロンプトパターンを検出した場合にセッション状態を `Waiting` に上書きする。

---

## WezTerm CLI の出力例 (参考)

```json
[
  {
    "window_id": 0,
    "tab_id": 1,
    "pane_id": 3,
    "workspace": "default",
    "size": {"rows": 50, "cols": 220},
    "title": "baton",
    "cwd": "file:///Users/foo/baton",
    "tty_name": "/dev/ttys003",
    "is_active": true,
    "cursor_x": 0,
    "cursor_y": 0
  }
]
```

---

## エラー定義（変更なし）

```go
var (
    ErrTerminalNotFound = errors.New("terminal not found")
    ErrPaneNotFound     = errors.New("pane not found")
)
```

---

## 設計判断

### 1. ID 型を int に統一する理由

**問題**: v1 では WezTerm CLI が数値で返す `pane_id` を `json.RawMessage` で受け、
文字列変換してから保持していた。セッションとのマッチング時に `strconv.Atoi` が随所に散らばる原因になっていた。

**判断**: WezTerm CLI の実際の出力形式が数値であることを確認済みのため、`int` で直接受ける。
`FocusPane` 引数も `int` に統一することで、型変換エラーが compile time に検出できる。

**トレードオフ**: 将来 string ID を返す別 terminal 実装が登場した場合は、その実装内で `int` への変換を担う。
インターフェースは `int` で統一し、実装側が吸収する責務を持つ。

### 2. CWD 正規化を ListPanes() に配置する理由

**問題**: v1 では CWD の正規化が呼び出し側（state.go）に漏れ出すリスクがあった。

**判断**: Terminal の責務として「外部 CLI の生データを Go 型に変換して返す」と定義し、
正規化も `ListPanes()` 内に閉じ込める。
呼び出し側は正規化済みパスを受け取ることを前提にでき、重複実装を防ぐ。

---

## ファイル配置

```
internal/terminal/
  terminal.go    # Terminal IF + Pane 型 + エラー定義
  wezterm.go     # WezTerminal 実装 (normalizeCWD を含む)
```

`normalizeCWD` は `wezterm.go` 内のパッケージ非公開関数として配置する。
他の terminal 実装が同じ正規化を必要とする場合は `terminal.go` に昇格させる。

# 詳細設計: Terminal インターフェース & 実装 (v3)

## 概要

`internal/terminal/` が担うターミナル抽象化レイヤーの v3 仕様。
主な変更点は **Pane.ID の string 化**・**tmux 実装の追加**・**CurrentCommand フィールド追加**・**デフォルト terminal の tmux 変更** の 4 点。

---

## v2 → v3 差分サマリ

| 項目 | v2 | v3 |
|------|----|----|
| `Pane.ID` 型 | `int` | `string` |
| `Pane.TabID` | `int` | 削除 |
| `Pane.Workspace` | `string` | `SessionName` に改名 |
| `Pane.CurrentCommand` | なし | `string` を追加 |
| `Pane.SessionAttached` | なし | `bool` を追加 |
| `Pane.WindowIndex` | なし | `int` を追加 |
| `Pane.PaneIndex` | なし | `int` を追加 |
| `FocusPane` / `GetPaneText` 引数 | `int` | `string` |
| デフォルト terminal | wezterm | tmux |
| tmux 実装 | なし | `tmux.go` 追加 |

---

## Terminal インターフェース (v3)

```go
type Terminal interface {
    ListPanes() ([]Pane, error)
    FocusPane(paneID string) error
    GetPaneText(paneID string) (string, error)
    IsAvailable() bool
    Name() string
}
```

---

## Pane 型 (v3)

```go
type Pane struct {
    ID             string // tmux: "%5", wezterm: "42"(stringified)
    Title          string
    WorkingDir     string // 正規化済み CWD
    TTYName        string // TTY デバイス名
    IsActive       bool
    CurrentCommand string // tmux: pane_current_command

    // tmux 固有フィールド
    SessionName     string
    SessionAttached bool
    WindowIndex     int
    PaneIndex       int
}
```

### フィールド追加の理由

| フィールド | 追加理由 |
|-----------|---------|
| `CurrentCommand` | Scanner の事前フィルタに使用。AI ツール以外のペインで `ps` をスキップし、パフォーマンスを向上 |
| `SessionName` | tmux セッション名。プロジェクトグルーピングキーおよび FocusPane のルーティングに使用 |
| `SessionAttached` | hook セッション除外判定に使用（unattached + パターンマッチで除外） |
| `WindowIndex` / `PaneIndex` | FocusPane で select-window / select-pane のターゲット指定に使用 |

### 削除フィールド

| フィールド | 削除理由 |
|-----------|---------|
| `TabID` | WezTerm 固有。tmux には対応する概念がなく、v3 では未使用 |

---

## TmuxTerminal 実装 (v3)

### ListPanes

```bash
tmux list-panes -a -F '#{session_name}\t#{session_attached}\t#{window_index}\t#{pane_index}\t#{pane_id}\t#{pane_title}\t#{pane_current_command}\t#{pane_current_path}\t#{pane_tty}'
```

- タブ区切り 9 フィールドをパース
- hook セッション除外: `^claude-.*-\d{4,}$` パターンの unattached セッションをフィルタ

### FocusPane

```bash
tmux switch-client -t {session_name}
tmux select-window -t {session_name}:{window_index}
tmux select-pane -t {pane_id}
```

- tmux は同期的（WezTerm の 2 秒 sleep 不要）
- 対象ペインが見つからない場合は `ErrPaneNotFound` を返す

### GetPaneText

```bash
tmux capture-pane -t {pane_id} -p -J
```

- 末尾 80 行を返す（承認プロンプト検出用）

### IsAvailable

- `exec.LookPath("tmux")` で CLI の存在のみ確認

---

## WezTerminal 実装 (v3 での変更)

- `Pane.ID`: `strconv.Itoa(rawPane.ID)` で string 化
- `Pane.Workspace` → `Pane.SessionName` にマッピング
- `FocusPane` / `GetPaneText`: 引数が `int` → `string` に変更（内部では string のまま使用）

---

## Hook セッション除外

ai-orchestra が Claude Code の hook 実行に使用する tmux セッション（`claude-hook-12345` 等）を自動除外する。

### 除外条件

```
!sessionAttached && hookSessionPattern.MatchString(sessionName)
```

- パターン: `^claude-.*-\d{4,}$`
- attached セッションは除外しない（ユーザーが明示的に接続している場合）

---

## 設計判断

### 1. ID 型を string に統一する理由

**問題**: v2 では `int` で統一していたが、tmux の pane_id は `%N` 形式の string。

**判断**: `string` に変更し、WezTerm 側で `strconv.Itoa` で変換する。
- tmux の `%5` を自然に表現できる
- マップキーとしての利用が容易
- WezTerm 側の変換コストは無視できるレベル

### 2. tmux 固有フィールドを Pane 構造体に含める理由

**問題**: `SessionName`, `WindowIndex` 等は tmux 固有であり、WezTerm では未使用。

**判断**: 現時点では Pane 構造体に直接含める。
- ターミナル実装が 2 つしかない段階で抽象化は過剰
- 未使用フィールドのゼロ値は無害
- 3 つ目の実装が追加された時点で `TerminalMetadata` 等への分離を検討

### 3. デフォルト terminal を tmux にする理由

**判断**: ユーザーの運用が tmux に完全移行したため、デフォルトを tmux に変更。
- WezTerm は `config.yaml` で `terminal: wezterm` を設定すれば引き続き利用可能
- `main.go` の `initTerminal` で空文字列と `"tmux"` を同一視

---

## ファイル配置

```
internal/terminal/
  terminal.go      # Terminal IF + Pane 型 + エラー定義
  tmux.go          # TmuxTerminal 実装（デフォルト）
  tmux_test.go     # tmux テスト
  wezterm.go       # WezTerminal 実装（レガシー対応）
  wezterm_test.go  # WezTerm テスト
```

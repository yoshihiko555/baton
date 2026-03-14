# 詳細設計: Exporter (v2)

## 概要

`internal/core/exporter.go` が担う JSON ステータス書き出し機能の v2 仕様。
v1 からの主な変更点は **DTO 分離**・**version フィールド追加**・**FormattedStatus 生成** の 3 点。

---

## v1 との差分サマリ

| 項目 | v1 | v2 |
|------|----|----|
| 出力スキーマバージョン | なし | `"version": 2` |
| 出力型 | `StatusOutput` (内部型と共通) | DTO 型を別途定義 |
| セッション状態 | `SessionState` (int) | `string` (`.String()` で変換) |
| ツール種別 | `ToolType` (int) | `string` (`.String()` で変換) |
| PaneID | `int` | `string` (strconv.Itoa) |
| FormattedStatus | なし | Go template で生成 |
| アトミック書き込み | あり | 継続 |
| パーミッション | 0600 | 継続 |

---

## DTO 型定義

### StatusOutput (v2)

```go
// StatusOutput は /tmp/baton-status.json に書き出す外部 DTO。
// 内部型 (core.Project 等) を直接使わず、Lua/外部ツール向けに正規化する。
type StatusOutput struct {
    Version         int             `json:"version"`          // 固定値 2
    Timestamp       string          `json:"timestamp"`        // ISO8601 (UTC)
    Projects        []ProjectOutput `json:"projects"`
    Summary         SummaryOutput   `json:"summary"`
    FormattedStatus string          `json:"formatted_status"` // WezTerm 表示用文字列
}
```

### ProjectOutput

```go
type ProjectOutput struct {
    Name      string          `json:"name"`
    Path      string          `json:"path"`
    Workspace string          `json:"workspace,omitempty"` // WezTerm ワークスペース名（未設定時は省略）
    Sessions  []SessionOutput `json:"sessions"`
}
```

### SessionOutput

```go
type SessionOutput struct {
    // --- 必須フィールド ---
    PaneID     string `json:"pane_id,omitempty"` // strconv.Itoa(Session.PaneID)。Ambiguous 時は省略
    Tool       string `json:"tool"`         // ToolType.String(): "claude" | "codex" | "gemini"
    State      string `json:"state"`        // SessionState.String(): "idle" | "thinking" | "tool_use" | "waiting" | "error"
    PID        int    `json:"pid"`          // プロセス PID
    WorkingDir string `json:"working_dir"`  // 作業ディレクトリ

    // --- 任意フィールド（取得不能時はキー自体を省略） ---
    Branch       string `json:"branch,omitempty"`        // Git ブランチ名
    CurrentTool  string `json:"current_tool,omitempty"`  // 実行中のツール名
    FirstPrompt  string `json:"first_prompt,omitempty"`  // タスク概要
    InputTokens  int    `json:"input_tokens,omitempty"`  // 入力トークン数
    OutputTokens int    `json:"output_tokens,omitempty"` // 出力トークン数
}
```

### SummaryOutput

```go
type SummaryOutput struct {
    TotalSessions int            `json:"total_sessions"` // 全セッション数
    Active        int            `json:"active"`         // Thinking + ToolUse + Waiting
    Waiting       int            `json:"waiting"`        // Waiting のみ
    ByTool        map[string]int `json:"by_tool"`        // ツール別セッション数 {"claude": 2, "codex": 1}
}
```

---

## 変換ロジック

### Session → SessionOutput

```go
func toSessionOutput(s core.Session) SessionOutput {
    out := SessionOutput{
        PID:        s.PID,
        Tool:       s.Tool.String(),
        State:      s.State.String(),
        WorkingDir: s.WorkingDir,
    }

    if s.PaneID != 0 && !s.Ambiguous {
        out.PaneID = strconv.Itoa(s.PaneID)
    }
    if s.Branch != "" {
        out.Branch = s.Branch
    }
    if s.CurrentTool != "" {
        out.CurrentTool = s.CurrentTool
    }
    if s.FirstPrompt != "" {
        out.FirstPrompt = s.FirstPrompt
    }
    if s.InputTokens != 0 {
        out.InputTokens = s.InputTokens
    }
    if s.OutputTokens != 0 {
        out.OutputTokens = s.OutputTokens
    }
    return out
}
```

### SummaryOutput の算出

全プロジェクトのセッションを走査し、状態ごとにカウント：

```
total_sessions = 全セッション数
active         = Thinking + ToolUse + Waiting の合計
waiting        = Waiting のみ
by_tool        = ツール別セッション数 {"claude": N, "codex": M, ...}
```

---

## FormattedStatus の生成

### 目的

WezTerm の Lua スクリプトで文字列を組み立てる処理を baton 側に移譲し、Lua を単純な JSON 読み取り + 表示に限定する。

### Config 定義（参照）

```yaml
# ~/.config/baton/config.yaml
statusbar:
  format: "{{.Active}} active / {{.TotalSessions}} total{{if .Waiting}} | {{.Waiting}} waiting{{end}}"
  tool_icons:
    claude:  ""
    codex:   ""
    gemini:  ""
    default: "●"
  show_tool_counts: true
```

### テンプレート変数

| 変数 | 型 | 説明 |
|------|----|------|
| `.TotalSessions` | int | 全セッション数 |
| `.Active` | int | アクティブセッション数 (Thinking + ToolUse + Waiting) |
| `.Waiting` | int | Waiting セッション数 |
| `.ByTool` | map[string]int | ツール別セッション数 (show_tool_counts=true 時のみ) |

### 生成手順

1. `statusbar.format` が空の場合はデフォルトフォーマットを使用
2. `text/template` でテンプレートをパース
3. `SummaryOutput` の値をテンプレートに渡して展開
4. `show_tool_counts: true` の場合は全セッションのツール種別を集計し `.ByTool` に設定

### デフォルトフォーマット

```
"{{.Active}}/{{.TotalSessions}}"
```

---

## Exporter.Write の設計

### Exporter 構造体

```go
type Exporter struct {
    destPath string
    cfg      config.StatusbarConfig
}

func NewExporter(destPath string, cfg config.StatusbarConfig) *Exporter
```

### Write メソッド

v2 では `StateReader` から最新状態を読み取り、DTO 変換から書き出しまでを一括で行う：

```go
// Write は StateReader から状態を読み取り、DTO に変換してアトミック書き出しする。
func (e *Exporter) Write(sr core.StateReader) error
```

`main.go` / ヘッドレスモードから `exporter.Write(stateReader)` として呼び出される。

### 処理フロー

```
1. sr.Projects() → []ProjectOutput に変換 (toSessionOutput を適用)
2. sr.Summary() → SummaryOutput に変換
3. FormattedStatus を生成 (Go template + cfg)
4. StatusOutput{Version: 2, Timestamp: now.UTC().RFC3339, ...} を組み立て
5. os.CreateTemp で一時ファイル生成
6. json.NewEncoder + SetIndent("", "  ") でエンコード
7. os.Rename で置換 (アトミック)
8. os.Chmod(destPath, 0600)
```

---

## 出力例

```json
{
  "version": 2,
  "timestamp": "2026-03-07T12:00:00Z",
  "projects": [
    {
      "name": "baton",
      "path": "/Users/foo/baton",
      "workspace": "baton",
      "sessions": [
        {
          "pane_id": "3",
          "tool": "claude",
          "state": "tool_use",
          "pid": 12346,
          "working_dir": "/Users/foo/baton",
          "branch": "feature/v2",
          "current_tool": "Bash",
          "input_tokens": 12500,
          "output_tokens": 8300
        }
      ]
    }
  ],
  "summary": {
    "total_sessions": 1,
    "active": 1,
    "waiting": 0,
    "by_tool": { "claude": 1 }
  },
  "formatted_status": "1/1"
}
```

---

## 設計判断

### 1. DTO を内部型と分離する理由

**問題**: v1 では `core.Project` / `core.Session` を直接 JSON 出力していた。
内部型に enum (int) が含まれており、Lua から扱いづらい。

**判断**: 外部出力専用の DTO 型 (`*Output`) を定義し、変換関数で文字列化する。
内部型を変更しても DTO が吸収するため、出力スキーマの安定性が上がる。

### 2. FormattedStatus を baton 側で生成する理由

**問題**: v1 では Lua スクリプトが JSON を解析して文字列を組み立てていた。
Lua でのテンプレート処理は煩雑で、ユーザーがカスタマイズしにくい。

**判断**: Go の `text/template` で生成し `formatted_status` に含める。
Lua は `status.formatted_status` を読むだけでよく、ロジックが baton に集約される。

### 3. version フィールドの互換性戦略

**方針**:
- `version: 1` は暗黙的 (フィールドなし) とみなす
- `version: 2` が存在する場合のみ新スキーマとして処理
- Lua スクリプト側で `if status.version == 2 then` のガードを追加する
- v3 以降が必要になった場合も同様のパターンで拡張する

---

## ファイル配置

```
internal/core/
  exporter.go      # Exporter 構造体 + Write メソッド + FormattedStatus 生成
  dto.go           # StatusOutput, ProjectOutput, SessionOutput, SummaryOutput
```

`dto.go` を分離することで、exporter.go はファイル I/O のみに集中できる。

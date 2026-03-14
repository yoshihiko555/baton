# Phase 1: 基盤型 `cc:TODO`

> 他のすべての Phase の前提となる型・設定定義。

## 設計書

- `docs/design/detailed/model.md`
- `docs/design/detailed/parser.md`
- `docs/design/detailed/migration.md` (Phase 1 セクション)

---

## タスク

### 1-1: model.go — ドメイン型再定義 `cc:TODO`

**対象**: `internal/core/model.go`
**設計書**: `docs/design/detailed/model.md`

- `cc:TODO` ToolType 列挙型の追加（ToolClaude/ToolCodex/ToolGemini/ToolUnknown + String()）
- `cc:TODO` SessionState に Waiting 状態を追加（5値に拡張）
- `cc:TODO` Session 構造体の全面変更（PID/CWD/Tool/State ベースに）
- `cc:TODO` Scanner/ProcessScanner/StateResolver インターフェース定義
- `cc:TODO` DetectedProcess 構造体の新規定義
- `cc:TODO` ScanResult 構造体の新規定義
- `cc:TODO` WatchEvent / WatchEventMsg 系の型削除
- `cc:TODO` 出力 DTO 型（StatusOutput/SummaryOutput/ProjectOutput/SessionOutput）の追加

### 1-2: parser.go — JSONL パーサー拡張 `cc:TODO`

**対象**: `internal/core/parser.go`
**設計書**: `docs/design/detailed/parser.md`

- `cc:TODO` Entry 型の拡張（SubType/SessionID/GitBranch/Data(ProgressData) 追加）
- `cc:TODO` Message 構造体に StopReason 追加、ContentBlock に Name 追加
- `cc:TODO` DetermineSessionState に Waiting 状態の判定ロジック追加
- `cc:TODO` assistant stop_reason=tool_use → Waiting、後続 progress → ToolUse の判定

### 1-3: config.go — 設定項目追加 `cc:TODO`

**対象**: `internal/config/config.go`
**設計書**: `docs/design/detailed/migration.md` (設定移行セクション)

- `cc:TODO` refresh_interval → scan_interval に改名（time.Duration 型）
- `cc:TODO` claude_projects_dir 追加（デフォルト: ~/.claude/projects）
- `cc:TODO` session_meta_dir 追加
- `cc:TODO` statusbar セクション追加（format: Go template 文字列、tool_icons: map[string]string）

---

## 完了条件

- `go build ./...` が通ること（テストの一時的なビルドエラーは許容）
- 新規追加した型のゼロ値が適切に初期化されること

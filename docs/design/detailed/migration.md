# v1 → v2 移行計画

## 概要

本文書は baton v1 から v2 へのファイル別移行計画を記述する。
各ファイルの変更分類・変更概要・推奨実施順序を示す。

---

## ファイル分類

### 新規作成

| ファイル | 内容 |
|---------|------|
| `internal/core/scanner.go` | `Scanner` インターフェース + `DefaultScanner` 実装。プロセス一覧とターミナル情報を組み合わせてスナップショットを生成する |
| `internal/core/process.go` | `ProcessScanner` 実装。`ps` コマンドの出力を解析し、Claude プロセス一覧と CWD を取得する |
| `internal/core/resolver.go` | `StateResolver` 実装。CWD から JSONL ファイルパスを解決し、セッション状態を判定する |
| `internal/core/scanner_test.go` | `Scanner` / `DefaultScanner` のユニットテスト。モック `ProcessScanner` を使用 |
| `internal/core/process_test.go` | `ProcessScanner` のユニットテスト。`ps` 出力をフィクスチャとして与えるテーブル駆動テスト |
| `internal/core/resolver_test.go` | `StateResolver` のユニットテスト。`t.TempDir()` で JSONL フィクスチャを配置してテスト |

---

### 大幅改修

#### `internal/core/model.go`

| 変更項目 | 内容 |
|---------|------|
| `ToolType` 追加 | AI クライアント種別（`ToolClaude` / `ToolCodex` / `ToolGemini` / `ToolUnknown`）を表す int enum 型を追加 |
| `SessionState` 拡張 | `Waiting` 状態を追加（ツール承認待ち）。v1 の `Idle` / `Thinking` / `ToolUse` / `Error` に加えて 5 値に拡張 |
| `Session` 全面変更 | v1 の `WatchEvent` ベースの構造から、プロセスベースの構造（`PID`, `CWD`, `Tool`, `State` 等）に全面変更 |
| インターフェース定義 | `Scanner`, `ProcessScanner`, `StateResolver` の各インターフェースを定義 |
| `WatchEvent` 系削除 | `WatchEvent`, `WatchEventMsg` など fsnotify ベースの型を削除 |

#### `internal/core/state.go`

| 変更項目 | 内容 |
|---------|------|
| `HandleEvent` 削除 | fsnotify イベントを受け取るメソッドを削除 |
| `UpdateFromScan` 追加 | `Scanner.Scan()` の結果（スナップショット）を受け取り、前回スナップショットと照合して状態を更新するメソッドを追加 |
| `Watcher` 依存削除 | コンストラクタから `Watcher` を除去。`StateResolver` を受け取る形に変更 |
| スナップショット照合方式 | プロセス PID をキーとして前回との差分を検出。新規プロセスは初期化、消滅プロセスはクリーンアップ |

#### `internal/core/parser.go`

| 変更項目 | 内容 |
|---------|------|
| `Entry` 拡張 | `SubType`, `SessionID`, `GitBranch`, `Data(ProgressData)` を追加。`Message` に `StopReason` 追加。`ContentBlock` に `Name` 追加 |
| `DetermineSessionState` ロジック変更 | `Waiting` 状態の判定ロジックを追加。`assistant stop_reason=tool_use` を Waiting、後続の `progress` エントリを ToolUse と判定 |
| `Message` 構造変更 | Claude の応答メッセージ構造を v2 の JSONL フォーマットに合わせて更新 |

#### `internal/core/exporter.go`

| 変更項目 | 内容 |
|---------|------|
| DTO 追加 | `StatusOutput` / `SummaryOutput` / `ProjectOutput` / `SessionOutput` 等の出力専用構造体を追加 |
| `version: 2` スキーマ | JSON 出力に `"version": 2` フィールドを追加 |
| `formatted_status` 生成 | `config.yaml` の `statusbar.format`（Go template）を評価して `formatted_status` 文字列を生成 |
| `tool_icons` 解決 | `statusbar.tool_icons` マッピングを適用してアイコン付き文字列を生成 |

#### `internal/tui/model.go`

| 変更項目 | 内容 |
|---------|------|
| `Watcher` 依存削除 | フィールドから `Watcher` を除去 |
| `Scanner` 依存追加 | `core.Scanner` インターフェースをフィールドとして追加 |
| `ScanResultMsg` 変更 | `WatchEventMsg` の代わりに `ScanResultMsg`（スキャン結果全体を格納）を定義 |

#### `internal/tui/update.go`

| 変更項目 | 内容 |
|---------|------|
| `WatchEventMsg` 削除 | fsnotify イベントのハンドラを削除 |
| `doScan` 駆動 | `TickMsg` 受信時に `doScan()` を呼び出し、結果を `ScanResultMsg` として処理 |
| サブメニュー対応 | セッション詳細・プロジェクト絞り込みのキー操作ハンドラを追加 |

#### `internal/tui/view.go`

| 変更項目 | 内容 |
|---------|------|
| `Waiting` 色追加 | `Waiting` 状態のセッションを視覚的に区別する色（例: オレンジ系）を追加 |
| 2 行セッション表示 | セッション行を「アイコン + ツール種別 + 状態 + ブランチ」「ツール名 + トークン数」の 2 行構成に変更 |
| ステータスバー変更 | 下部ステータスバーに `summary`（sessions / active / waiting 数）を表示 |

#### `main.go`

| 変更項目 | 内容 |
|---------|------|
| `Watcher` → `Scanner` | `core.NewWatcher()` を `core.NewDefaultScanner()` に置き換え |
| コンポーネント初期化順序変更 | `processScanner` → `scanner` → `resolver` → `stateManager` → `exporter` の順に初期化 |
| `doScan()` 関数追加 | モード非依存のスキャン関数を `main.go` レベルで定義 |
| ヘッドレス goroutine 変更 | `watcher.Events()` チャネル監視から `time.Ticker` + `doScan()` に変更 |

#### `wezterm/baton-status.lua`

| 変更項目 | 内容 |
|---------|------|
| `formatted_status` 対応 | `status_chunks()` の自前集計ロジックを削除し、`data.formatted_status` をそのまま使用 |
| `version` チェック追加 | `data.version ~= 2` の場合に `"baton: unknown format"` を表示 |
| `Waiting` 強調色 | `data.summary.waiting > 0` の場合に `formatted_status` 全体をオレンジ（`#FF8800`）で表示 |

---

### 軽微な改修

#### `internal/terminal/terminal.go`

| 変更項目 | 内容 |
|---------|------|
| `Pane` 型拡張 | `TTYName`（string）、`IsActive`（bool）フィールドを追加。`ID` を `int` 型に変更 |
| `FocusPane` 引数変更 | `PaneID` の型を `string` から `int` に変更 |

#### `internal/terminal/wezterm.go`

| 変更項目 | 内容 |
|---------|------|
| JSON パース変更 | `wezterm cli list --format json` の出力パース対象フィールドを追加（`TTYName`, `IsActive`） |
| CWD 正規化追加 | `file://` プレフィックスの除去、末尾スラッシュの除去を `normalizeCWD` 関数として `ListPanes()` 内で適用 |

#### `internal/config/config.go`

| 変更項目 | 内容 |
|---------|------|
| `scan_interval` 追加 | 旧 `refresh_interval` を改名。`time.Duration` 型 |
| `claude_projects_dir` 追加 | Scanner が JSONL を探すルートディレクトリ |
| `session_meta_dir` 追加 | 将来のセッションメタデータ参照用ディレクトリ |
| `statusbar` セクション追加 | `format`（Go template 文字列）と `tool_icons`（map[string]string）を追加 |

---

### 保持・不使用

| ファイル | 理由 |
|---------|------|
| `internal/core/watcher.go` | v2 では使用しない。ただし将来的に JSONL ファイルの変更を即時検知する用途（スキャン間隔の短縮・レイテンシ改善）に活用できるため削除しない |

---

### テストの移行

| ファイル | 変更概要 |
|---------|---------|
| `internal/core/model_test.go` | `ToolType` / `Waiting` 状態 / 新 `Session` 構造体に合わせて更新 |
| `internal/core/state_test.go` | `HandleEvent` のテストを全面廃棄し、`UpdateFromScan` のスナップショット照合テストに書き換え |
| `internal/core/parser_test.go` | `Entry` 拡張（`tool_name` 等）と `DetermineSessionState` の `Waiting` 判定テストを追加 |
| `internal/core/exporter_test.go` | DTO 変換・`version: 2` スキーマ・`formatted_status` 生成のテストを追加 |
| `internal/core/watcher_test.go` | 保持（`watcher.go` が保持されるため。変更なし） |
| `internal/terminal/wezterm_test.go` | `int` 型 `Pane.ID` パース・CWD 正規化（`file://` 除去・末尾スラッシュ除去）のテストを追加 |
| `internal/config/config_test.go` | `scan_interval` / `claude_projects_dir` / `statusbar` 等の新設定項目のパーステストを追加 |
| `internal/tui/update_test.go` | `ScanResultMsg` ハンドラ・サブメニューのキー操作テストを追加 |
| `internal/tui/view_test.go` | `Waiting` 状態の色表示・2 行セッション表示のスナップショットテストを追加 |

---

## 移行順序（推奨）

依存関係の方向に従い、以下の順序で実施する。

### Phase 1: 基盤型

> 他のすべての Phase の前提となる型・設定定義。

1. `internal/core/model.go` — `ToolType` / `Waiting` / `Session` 再定義・インターフェース定義・`WatchEvent` 系削除
2. `internal/core/parser.go` — `Entry` 拡張・`DetermineSessionState` の `Waiting` 判定追加
3. `internal/config/config.go` — `scan_interval` / `claude_projects_dir` / `session_meta_dir` / `statusbar` 追加

### Phase 2: インフラ層

> Phase 1 の型に依存する低レベルコンポーネント。

4. `internal/terminal/terminal.go` — `Pane` 型拡張・`FocusPane` 引数変更
5. `internal/terminal/wezterm.go` — JSON パース変更・CWD 正規化追加
6. `internal/core/process.go` — 新規作成（`ProcessScanner`）
7. `internal/core/scanner.go` — 新規作成（`Scanner` インターフェース + `DefaultScanner`）

### Phase 3: ドメイン層

> Phase 1・2 に依存するビジネスロジック。

8. `internal/core/resolver.go` — 新規作成（`StateResolver`）
9. `internal/core/state.go` — `HandleEvent` 削除・`UpdateFromScan` 追加・`Watcher` 依存削除
10. `internal/core/exporter.go` — DTO 追加・`version: 2` スキーマ・`formatted_status` 生成

### Phase 4: プレゼンテーション層

> Phase 3 に依存する UI・エントリポイント・Lua プラグイン。

11. `internal/tui/model.go` — `Watcher` 削除・`Scanner` 追加・`ScanResultMsg` 変更
12. `internal/tui/update.go` — `WatchEventMsg` 削除・`doScan` 駆動・サブメニュー対応
13. `internal/tui/view.go` — `Waiting` 色追加・2 行セッション表示・ステータスバー変更
14. `main.go` — `Watcher` → `Scanner`・初期化順序変更・`doScan()` 追加
15. `wezterm/baton-status.lua` — `formatted_status` 対応・`version` チェック・`Waiting` 強調

### Phase 5: テスト・品質

> 全 Phase 完了後に実施する。

16. 新規テスト作成: `scanner_test.go` / `process_test.go` / `resolver_test.go`
17. 既存テスト更新: `model_test.go` / `state_test.go` / `parser_test.go` / `exporter_test.go`
18. 既存テスト更新: `wezterm_test.go` / `config_test.go` / `update_test.go` / `view_test.go`
19. `go vet ./...` — 静的解析パス確認
20. `go test ./... -v` — 全テストパス確認
21. テストカバレッジ確認（`go test ./... -cover`）

---

## 移行上の注意事項

### フェーズ間の依存制約

```
Phase 1 (基盤型)
    ↓
Phase 2 (インフラ層)
    ↓
Phase 3 (ドメイン層)
    ↓
Phase 4 (プレゼンテーション層)
    ↓
Phase 5 (テスト・品質)
```

- **Phase 1 → Phase 2** は必須前提（`Pane` 型拡張は `model.go` の型に依存）
- **Phase 2 → Phase 3** は必須前提（`StateResolver` は `ProcessScanner` に依存）
- **Phase 3 → Phase 4** は必須前提（TUI / `main.go` は `Scanner` / `Exporter` に依存）
- 同一 Phase 内のファイルは並列実施可能

### watcher.go の扱い

- `internal/core/watcher.go` および `internal/core/watcher_test.go` は **削除しない**
- v2 では使用しないが、将来のスキャン間隔短縮（JSONL 変更の即時検知）に活用できる
- v2 の `Scanner` / `StateManager` とは疎結合であり、後から組み合わせ可能

### テストの一時的な破損について

- Phase 1 で `model.go` の型を変更すると、`state_test.go` / `parser_test.go` 等が一時的にビルドエラーになる
- これは許容する。Phase 5 で全テストを修正・パスさせることを目標とする
- ビルドエラーが長期間続かないよう、Phase 1〜3 は連続して実施することを推奨する

### v1 からの設定移行（ユーザー向け）

| v1 設定キー | v2 設定キー | 備考 |
|------------|------------|------|
| `refresh_interval` | `scan_interval` | 値の形式は同一（例: `2s`） |
| `watch_path` | （削除） | v2 では不要 |
| `terminal.type` | `terminal.type` | 変更なし |
| `export_path` | `export_path` | 変更なし |
| （なし） | `claude_projects_dir` | 新規追加。デフォルト: `~/.claude/projects` |
| （なし） | `statusbar.format` | 新規追加。省略時はデフォルトテンプレートを使用 |

---

## 関連ドキュメント

- `docs/design/detailed/main-entrypoint.md` — main.go ワイヤリング・ライフサイクル詳細設計
- `docs/design/detailed/lua-plugin.md` — WezTerm Lua プラグイン v2 対応詳細設計
- `docs/requirements/requirements-v2.md` — v2 要件定義（参照元）
- `docs/architecture/overview.md` — v2 アーキテクチャ概要（参照元）
- `docs/design/basic-design.md` — v2 基本設計（参照元）

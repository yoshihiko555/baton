# Phase 3: ドメイン層 `cc:TODO`

> Phase 1・2 に依存するビジネスロジック。

## 前提

- Phase 1, 2 完了済みであること

## 設計書

- `docs/design/detailed/state-resolver.md`
- `docs/design/detailed/state-manager.md`
- `docs/design/detailed/exporter.md`
- `docs/design/detailed/migration.md` (Phase 3 セクション)

---

## タスク

### 3-1: resolver.go — StateResolver 新規作成 `cc:TODO`

**対象**: `internal/core/resolver.go`（新規）
**設計書**: `docs/design/detailed/state-resolver.md`

- `cc:TODO` StateResolver インターフェース実装
- `cc:TODO` CWD → スラッグ化 → JSONL ディレクトリ特定
- `cc:TODO` JSONL ファイルの mtime フィルタリング（アクティブ候補絞り込み）
- `cc:TODO` JSONL 末尾エントリからの状態判定（Idle/Thinking/ToolUse/Waiting/Error）
- `cc:TODO` gitBranch / currentTool の JSONL エントリからの抽出
- `cc:TODO` session-meta 取得（firstPrompt/inputTokens/outputTokens）
- `cc:TODO` 同一 CWD 複数プロセス時の Ambiguous フラグ設定

### 3-2: state.go — StateManager 全面改修 `cc:TODO`

**対象**: `internal/core/state.go`
**設計書**: `docs/design/detailed/state-manager.md`

- `cc:TODO` HandleEvent メソッド削除
- `cc:TODO` UpdateFromScan メソッド追加（スナップショット照合方式）
- `cc:TODO` Watcher 依存の除去、StateResolver 受け取りに変更
- `cc:TODO` PID キーでの前回スナップショットとの差分検出
- `cc:TODO` 新規プロセス初期化・消滅プロセスクリーンアップ
- `cc:TODO` Summary 計算（Active = Thinking + ToolUse、Waiting カウント）

### 3-3: exporter.go — v2 スキーマ対応 `cc:TODO`

**対象**: `internal/core/exporter.go`
**設計書**: `docs/design/detailed/exporter.md`

- `cc:TODO` DTO 変換ロジック（内部型 → StatusOutput/ProjectOutput/SessionOutput）
- `cc:TODO` version: 2 スキーマでの JSON 出力
- `cc:TODO` formatted_status 生成（config.yaml の statusbar.format Go template 評価）
- `cc:TODO` tool_icons マッピング適用（アイコン付き文字列生成）
- `cc:TODO` pane_id フィールドの SessionOutput への追加

---

## 完了条件

- `go build ./...` が通ること
- StateManager がスナップショット照合で正しく差分検出できること

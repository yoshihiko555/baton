# Phase 5: テスト・品質 `cc:TODO`

> 全 Phase 完了後に実施する。

## 前提

- Phase 1〜4 完了済みであること

## 設計書

- `docs/design/detailed/migration.md` (テスト移行セクション)

---

## タスク

### 5-1: 新規テスト作成 `cc:TODO`

- `cc:TODO` scanner_test.go — Scanner/DefaultScanner のユニットテスト（モック ProcessScanner 使用）
- `cc:TODO` process_test.go — ProcessScanner のユニットテスト（ps 出力フィクスチャのテーブル駆動テスト）
- `cc:TODO` resolver_test.go — StateResolver のユニットテスト（t.TempDir() で JSONL フィクスチャ配置）

### 5-2: 既存テスト更新（core） `cc:TODO`

- `cc:TODO` model_test.go — ToolType/Waiting 状態/新 Session 構造体に合わせて更新
- `cc:TODO` state_test.go — HandleEvent テスト全面廃棄 → UpdateFromScan スナップショット照合テストに書き換え
- `cc:TODO` parser_test.go — Entry 拡張 + DetermineSessionState の Waiting 判定テスト追加
- `cc:TODO` exporter_test.go — DTO 変換・version:2 スキーマ・formatted_status 生成テスト追加

### 5-3: 既存テスト更新（周辺） `cc:TODO`

- `cc:TODO` wezterm_test.go — int 型 Pane.ID パース・CWD 正規化テスト追加
- `cc:TODO` config_test.go — scan_interval/claude_projects_dir/statusbar 新設定項目テスト追加
- `cc:TODO` update_test.go — ScanResultMsg ハンドラ・サブメニューキー操作テスト追加
- `cc:TODO` view_test.go — Waiting 状態色表示・2行セッション表示のスナップショットテスト追加

### 5-4: 静的解析・全テスト実行 `cc:TODO`

- `cc:TODO` `go vet ./...` パス確認
- `cc:TODO` `go test ./... -v` 全テストパス確認
- `cc:TODO` `go test ./... -cover` カバレッジ確認

---

## 完了条件

- `go vet ./...` エラーなし
- `go test ./... -v` 全テストパス
- 新規コンポーネント（scanner/process/resolver）のカバレッジが妥当な水準であること

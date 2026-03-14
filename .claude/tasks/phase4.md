# Phase 4: プレゼンテーション層 `cc:TODO`

> Phase 3 に依存する UI・エントリポイント・Lua プラグイン。

## 前提

- Phase 1, 2, 3 完了済みであること

## 設計書

- `docs/design/detailed/tui.md`
- `docs/design/detailed/main-entrypoint.md`
- `docs/design/detailed/lua-plugin.md`
- `docs/design/detailed/migration.md` (Phase 4 セクション)

---

## タスク

### 4-1: tui/model.go — TUI Model 変更 `cc:TODO`

**対象**: `internal/tui/model.go`
**設計書**: `docs/design/detailed/tui.md`

- `cc:TODO` Watcher フィールド削除
- `cc:TODO` Scanner フィールド追加（core.Scanner インターフェース）
- `cc:TODO` ScanResultMsg 定義（WatchEventMsg の代替）

### 4-2: tui/update.go — イベントハンドリング変更 `cc:TODO`

**対象**: `internal/tui/update.go`
**設計書**: `docs/design/detailed/tui.md`

- `cc:TODO` WatchEventMsg ハンドラ削除
- `cc:TODO` TickMsg 受信時に doScan() 呼び出し → ScanResultMsg として処理
- `cc:TODO` サブメニュー対応（セッション詳細・プロジェクト絞り込みのキー操作）

### 4-3: tui/view.go — 表示変更 `cc:TODO`

**対象**: `internal/tui/view.go`
**設計書**: `docs/design/detailed/tui.md`

- `cc:TODO` Waiting 状態の色追加（オレンジ系）
- `cc:TODO` 2行セッション表示（アイコン+ツール種別+状態+ブランチ / ツール名+トークン数）
- `cc:TODO` ステータスバー変更（summary: sessions/active/waiting 数の表示）

### 4-4: main.go — エントリポイント再設計 `cc:TODO`

**対象**: `main.go`
**設計書**: `docs/design/detailed/main-entrypoint.md`

- `cc:TODO` Watcher → Scanner に置き換え（NewDefaultScanner）
- `cc:TODO` コンポーネント初期化順序変更（processScanner→scanner→resolver→stateManager→exporter）
- `cc:TODO` doScan() 関数の main.go レベル定義
- `cc:TODO` ヘッドレス goroutine を watcher.Events() から time.Ticker + doScan() に変更

### 4-5: baton-status.lua — Lua プラグイン v2 対応 `cc:TODO`

**対象**: `wezterm/baton-status.lua`
**設計書**: `docs/design/detailed/lua-plugin.md`

- `cc:TODO` formatted_status 対応（自前集計ロジック削除、data.formatted_status をそのまま使用）
- `cc:TODO` version チェック追加（data.version ~= 2 の場合 "baton: unknown format" 表示）
- `cc:TODO` Waiting 強調色（summary.waiting > 0 のとき全体をオレンジ #FF8800 で表示）

---

## 完了条件

- `go build -o baton .` でビルド成功
- `./baton --version` が正常動作
- `./baton --once` がエラーなく実行完了

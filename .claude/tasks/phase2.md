# Phase 2: インフラ層 `cc:TODO`

> Phase 1 の型に依存する低レベルコンポーネント。

## 前提

- Phase 1 完了済みであること

## 設計書

- `docs/design/detailed/terminal.md`
- `docs/design/detailed/scanner.md`
- `docs/design/detailed/migration.md` (Phase 2 セクション)

---

## タスク

### 2-1: terminal.go — Pane 型拡張 `cc:TODO`

**対象**: `internal/terminal/terminal.go`
**設計書**: `docs/design/detailed/terminal.md`

- `cc:TODO` Pane 構造体に TTYName(string)、IsActive(bool) フィールド追加
- `cc:TODO` Pane.ID を string → int 型に変更
- `cc:TODO` FocusPane 引数を string → int に変更

### 2-2: wezterm.go — JSON パース変更 `cc:TODO`

**対象**: `internal/terminal/wezterm.go`
**設計書**: `docs/design/detailed/terminal.md`

- `cc:TODO` wezterm cli list --format json の出力パース対象にTTYName/IsActive追加
- `cc:TODO` normalizeCWD 関数追加（file:// プレフィックス除去、末尾スラッシュ除去）
- `cc:TODO` ListPanes() 内で CWD 正規化を適用

### 2-3: process.go — ProcessScanner 新規作成 `cc:TODO`

**対象**: `internal/core/process.go`（新規）
**設計書**: `docs/design/detailed/scanner.md`

- `cc:TODO` ProcessScanner インターフェース実装
- `cc:TODO` ps コマンド出力のパース（PID/TTY/COMMAND 抽出）
- `cc:TODO` Claude/Codex/Gemini プロセスのフィルタリング
- `cc:TODO` DetectedProcess 構造体への変換
- `cc:TODO` CWD 取得ロジック（lsof または /proc ベース）

### 2-4: scanner.go — Scanner 新規作成 `cc:TODO`

**対象**: `internal/core/scanner.go`（新規）
**設計書**: `docs/design/detailed/scanner.md`

- `cc:TODO` Scanner インターフェース + DefaultScanner 実装
- `cc:TODO` Terminal.ListPanes() → ProcessScanner.Find() のオーケストレーション
- `cc:TODO` ScanResult への集約ロジック
- `cc:TODO` Scan(ctx) メソッド実装

---

## 完了条件

- `go build ./...` が通ること
- Terminal インターフェースの後方互換性に注意

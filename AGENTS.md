
# baton - AI Session Monitor

**概要**: Claude Code のセッション状態をリアルタイム監視し、TUI ダッシュボードと WezTerm ステータスバーに表示する Go アプリケーション

---

## Tech Stack

- **Language**: Go 1.22+
- **Framework**: bubbletea (TUI), bubbles (components), lipgloss (styling)
- **File Watching**: fsnotify
- **Config**: yaml.v3
- **Package Manager**: Go Modules

---

## Commands

```bash
# ビルド
go build -o baton .

# テスト実行
go test ./... -v

# 静的解析
go vet ./...

# TUI 起動
./baton

# ヘッドレスモード（JSON出力のみ）
./baton --no-tui

# ワンショット（1回だけ状態出力して終了）
./baton --once

# バージョン表示
./baton --version
```

---

## Project Structure

```
.
├── main.go                          # エントリポイント（--no-tui/--once/--config フラグ）
├── internal/
│   ├── core/
│   │   ├── model.go                 # ドメイン型（SessionState, Session, Project, StatusOutput）
│   │   ├── parser.go                # JSONL パーサー + IncrementalReader
│   │   ├── watcher.go               # fsnotify ファイルウォッチャー + デバウンス
│   │   ├── state.go                 # 状態集約マネージャー
│   │   └── exporter.go              # アトミック JSON 書き出し
│   ├── terminal/
│   │   ├── terminal.go              # Terminal インターフェース定義
│   │   └── wezterm.go               # WezTerm CLI 実装
│   ├── config/
│   │   └── config.go                # YAML 設定読み込み
│   └── tui/
│       ├── model.go                 # bubbletea Model + Init
│       ├── update.go                # キー入力・イベントハンドリング
│       └── view.go                  # 左右ペイン + ステータスバー描画
└── wezterm/
    └── baton-status.lua             # WezTerm ステータスバー Lua プラグイン
```

---

## Coding Conventions

- Go 標準の命名規則（exported = PascalCase, unexported = camelCase）
- Early return パターン
- エラーは呼び出し元に返す（log.Fatal は main のみ）
- テストは `_test.go` に配置、`t.TempDir()` でテストフィクスチャ作成

---

## Notes

- 設定ファイル: `~/.config/baton/config.yaml`（オプション）
- ステータス出力: `/tmp/baton-status.json`（アトミック書き込み）
- データフロー: watcher → channel → tea.Sub → bubbletea Msg → Update()
- state.go は集約のみ、JSON 書き出しは exporter.go に分離

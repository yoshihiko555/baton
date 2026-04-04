
# baton - AI Session Monitor

**概要**: Claude Code, Codex, Gemini のセッション状態をリアルタイム監視し、TUI ダッシュボードと tmux (デフォルト) / WezTerm ステータスバーに表示する Go アプリケーション

---

## Tech Stack

- **Language**: Go 1.25.5+
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
├── main.go                          # エントリポイント（--no-tui/--once/--exit/--config フラグ）
├── internal/
│   ├── core/
│   │   ├── model.go                 # ドメイン型（SessionState, Session, Project, StatusOutput）
│   │   ├── parser.go                # JSONL パーサー + IncrementalReader
│   │   ├── process.go               # プロセス検出（ps / pgrep）
│   │   ├── resolver.go              # Claude JSONL / session-meta 解決
│   │   ├── scanner.go               # ペイン走査 + CurrentCommand フィルタ
│   │   ├── state.go                 # 状態集約マネージャー
│   │   ├── exporter.go              # アトミック JSON 書き出し
│   │   ├── tmux_status.go           # tmux ステータスライン文字列生成
│   │   └── watcher.go               # fsnotify ファイルウォッチャー（互換用途）
│   ├── terminal/
│   │   ├── terminal.go              # Terminal インターフェース定義
│   │   ├── tmux.go                  # tmux CLI 実装（デフォルト）
│   │   └── wezterm.go               # WezTerm CLI 実装（レガシー）
│   ├── config/
│   │   └── config.go                # YAML 設定読み込み
│   └── tui/
│       ├── model.go                 # bubbletea Model + Init
│       ├── update.go                # キー入力・イベントハンドリング
│       └── view.go                  # 左右ペイン + ステータスバー描画
└── wezterm/
    └── baton-status.lua             # WezTerm ステータスバー Lua プラグイン（レガシー）
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
- デフォルト terminal: `tmux`（`terminal: wezterm` でレガシー切り替え可能）
- データフロー: `ticker` → `Scanner.Scan()` → `StateManager.UpdateFromScan()` → `RefineToolUseState()` → TUI / Exporter
- 状態判定: Claude は pane text 優先 + JSONL 補助、Codex は子プロセス検査、Gemini は pane text で `Idle` / `Waiting` を精緻化

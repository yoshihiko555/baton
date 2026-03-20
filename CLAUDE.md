# baton - AI Session Monitor

**概要**: AI コーディングセッション（Claude Code / Codex / Gemini）の状態をリアルタイム監視し、TUI ダッシュボードとステータスバーに表示する Go アプリケーション

---

## Tech Stack

- **Language**: Go 1.22+
- **Framework**: bubbletea (TUI), bubbles (components), lipgloss (styling)
- **Terminal**: tmux（デフォルト）、WezTerm（レガシー対応）
- **File Watching**: fsnotify
- **Config**: yaml.v3
- **Package Manager**: Go Modules

---

## Commands

```bash
# ビルド
go build -o baton .

# ビルド＆ローカルインストール（~/.local/bin）
# 重要: macOS では cp 後に codesign が必須。省略するとカーネルが SIGKILL する。
go build -o baton . && cp baton ~/.local/bin/baton && codesign -f -s - ~/.local/bin/baton

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
│   │   ├── process.go               # プロセス検出（ps/pgrep ベース）
│   │   ├── scanner.go               # DefaultScanner（ペイン走査 + CurrentCommand フィルタ）
│   │   ├── watcher.go               # fsnotify ファイルウォッチャー + デバウンス
│   │   ├── state.go                 # 状態集約マネージャー（ResolveMultiple 方式）
│   │   └── exporter.go              # アトミック JSON 書き出し
│   ├── terminal/
│   │   ├── terminal.go              # Terminal インターフェース定義（Pane 構造体含む）
│   │   ├── tmux.go                  # tmux CLI 実装（デフォルト）
│   │   └── wezterm.go               # WezTerm CLI 実装（レガシー対応）
│   ├── config/
│   │   └── config.go                # YAML 設定読み込み
│   └── tui/
│       ├── model.go                 # bubbletea Model + Init
│       ├── update.go                # キー入力・イベントハンドリング（ペインジャンプ）
│       └── view.go                  # 左右ペイン + ステータスバー描画
└── wezterm/
    └── baton-status.lua             # WezTerm ステータスバー Lua プラグイン（active/total 表示）
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
- デフォルトターミナル: tmux（config.yaml の `terminal` で "wezterm" に変更可能）
- データフロー: Ticker → doScan → ScanResultMsg → Update()（ポーリング方式、2秒間隔）
- state.go は集約のみ、JSON 書き出しは exporter.go に分離
- 同一 CWD の複数セッション: ResolveMultiple 方式で ModTime 上位 N 件から状態分布を取得
- slug 生成: CWD の "/" と "." を "-" に変換（Claude Code のディレクトリ命名規則に準拠）
- Pane.ID は string 型（tmux: "%5" 形式、WezTerm: "42" 形式）
- Scanner 最適化: tmux の `CurrentCommand` で AI ペインのみ `ps` 実行（不要な呼び出しを削減）
- Codex 状態検出: `pgrep -P` で子プロセス有無を検査（子プロセスあり → Thinking、なし → Idle）
- ペインジャンプ:
  - tmux: switch-client → select-window → select-pane（同期的、sleep 不要）
  - WezTerm: 同一 WS → `wezterm cli activate-pane`、別 WS → トリガーファイル経由
- ToolUse 承認待ち検出: `tmux capture-pane` / `wezterm cli get-text` でペインテキストを取得し承認プロンプトを検出すると Waiting に変換
- Hook セッション除外: tmux の `claude-*-<digits>` パターン（unattached）を自動除外
- --no-tui モード: 起動メッセージと初回スキャン結果を標準出力に表示

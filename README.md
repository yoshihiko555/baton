# baton

Claude Code のセッション状態をリアルタイム監視する Go アプリケーション。TUI ダッシュボードと WezTerm ステータスバーで確認できます。

## Features

- Claude Code の JSONL ログをリアルタイム監視（fsnotify）
- プロジェクト・セッション単位の状態集約（idle / thinking / tool_use / error）
- bubbletea ベースの TUI ダッシュボード（左右ペイン + ステータスバー）
- ヘッドレスモード（`/tmp/baton-status.json` への JSON 出力）
- WezTerm ステータスバー連携（Lua プラグイン）

## Requirements

- Go 1.22+

## Install

```bash
go install github.com/yoshihiko555/baton@latest
```

または手動ビルド:

```bash
git clone https://github.com/yoshihiko555/baton.git
cd baton
go build -o baton .
```

## Usage

```bash
# TUI ダッシュボード起動
./baton

# ヘッドレスモード（JSON 出力のみ、バックグラウンド実行向け）
./baton --no-tui

# ワンショット（1回だけ状態を出力して終了）
./baton --once

# 設定ファイルを指定
./baton --config ~/.config/baton/config.yaml

# バージョン表示
./baton --version
```

### TUI キー操作

| キー | 動作 |
|------|------|
| `Tab` | 左右ペインの切り替え |
| `q` / `Ctrl+C` | 終了 |

## Configuration

設定ファイル（オプション）: `~/.config/baton/config.yaml`

```yaml
# 監視対象ディレクトリ（デフォルト: Claude Code のセッションディレクトリ）
watch_path: ""

# ステータス JSON の出力先
status_output_path: "/tmp/baton-status.json"

# ヘッドレスモードの更新間隔
refresh_interval: "1s"

# 使用するターミナル（現在 wezterm のみ対応）
terminal: "wezterm"
```

## WezTerm Integration

WezTerm のステータスバーに baton の監視情報を表示できます。

### セットアップ

1. `wezterm/baton-status.lua` を WezTerm の設定ディレクトリにコピーまたはシンボリックリンク:

```bash
ln -s /path/to/baton/wezterm/baton-status.lua ~/.config/wezterm/baton-status.lua
```

2. WezTerm の設定で読み込み:

```lua
-- wezterm.lua または config/statusbar.lua
local baton_status = require 'baton-status'
baton_status.setup({
  path = '/tmp/baton-status.json', -- 省略可
  interval = 5,                     -- 省略可（秒）
})
```

3. baton をヘッドレスモードで起動:

```bash
./baton --no-tui &
```

## Project Structure

```
.
├── main.go                    # エントリポイント
├── internal/
│   ├── core/
│   │   ├── model.go           # ドメイン型定義
│   │   ├── parser.go          # JSONL パーサー
│   │   ├── watcher.go         # ファイルウォッチャー
│   │   ├── state.go           # 状態集約マネージャー
│   │   └── exporter.go        # JSON エクスポーター
│   ├── terminal/
│   │   ├── terminal.go        # Terminal インターフェース
│   │   └── wezterm.go         # WezTerm 実装
│   ├── config/
│   │   └── config.go          # 設定読み込み
│   └── tui/
│       ├── model.go           # bubbletea Model
│       ├── update.go          # イベントハンドリング
│       └── view.go            # 描画ロジック
└── wezterm/
    └── baton-status.lua       # WezTerm プラグイン
```

## Development

```bash
# テスト
go test ./... -v

# 静的解析
go vet ./...
```

## License

MIT

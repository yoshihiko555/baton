# baton

tmux 上の AI コーディングセッション (Claude Code / Codex / Gemini) をリアルタイム監視する TUI ダッシュボード。

![baton TUI](assets/preview.png)

## 概要

baton は tmux ペイン上で動作している AI コーディングセッションを自動検出し、状態をグループ化して表示します。**ペインインデクサ + ステータストラッカー + スイッチャー** として設計されており、セッションの起動は行いません。

設計思想:

- **ペイン中心**: 主キーは `TMUX_PANE`。同一 tmux セッション内の複数 AI セッションを個別に追跡
- **非侵入的**: セッションは手動起動。baton は `ps` + JSONL ログ解析で後追い検出
- **Hook 不要**: 状態は JSONL ログ・子プロセス検出・画面スクレイピングから導出。Claude Code hooks の設定は不要

## 機能

- リアルタイム状態監視: `Thinking` / `ToolUse` / `Waiting` / `Idle` / `Error`
- 状態別グループ化セッションリスト + ターミナルプレビューペイン
- ペインジャンプ: セッションを選択して対象 tmux ペインに移動
- マルチツール対応: Claude Code, Codex CLI, Gemini CLI
- TUI セッションフィルタ: `/` でインクリメンタル絞り込み（`waiting`, `!idle` など）
- Claude Code 承認操作: `a` / `d` / `A` / `D` で承認・拒否
- 承認プロンプト検出: `tmux capture-pane` による画面スクレイピング
- Codex アイドル/作業中検出: 子プロセス有無の検査
- Hook セッション除外: ai-orchestra が作成する `claude-*-<digits>` セッションを自動フィルタ
- ステータスバー JSON 出力: tmux ステータスライン連携用
- ヘッドレスモード: バックグラウンド監視用

## 動作環境

- Go 1.25.5+（`go.mod` 準拠）
- tmux (デフォルトターミナルバックエンド)

## インストール

```bash
go install github.com/yoshihiko555/baton@latest
```

固定バージョンを導入する場合:

```bash
go install github.com/yoshihiko555/baton@v0.1.1
```

または GitHub Releases のビルド済みバイナリ（`baton_<tag>_<os>_<arch>.tar.gz` / `.zip`）を利用してください。

リリース関連ドキュメント:

- [CHANGELOG.md](../CHANGELOG.md)
- [release-process.md](release-process.md)

`go install` 後に `baton` が見つからない場合は、Go の bin ディレクトリが `PATH` に含まれているか確認してください。

ソースからビルド:

```bash
git clone https://github.com/yoshihiko555/baton.git
cd baton
go build -o baton .

# macOS: バイナリコピー後に codesign が必須
cp baton ~/.local/bin/baton && codesign -f -s - ~/.local/bin/baton
```

## 使い方

```bash
# TUI ダッシュボード（ペインジャンプ後も TUI に戻る）
baton

# TUI ダッシュボード（ペインジャンプ後に終了、tmux popup 向け）
baton --exit

# ヘッドレスモード（JSON 出力のみ、バックグラウンド監視用）
baton --no-tui

# ワンショット（1回スキャンして JSON 出力後に終了）
baton --once

# 設定ファイル指定
baton --config ~/.config/baton/config.yaml

# バージョン表示
baton --version
```

### tmux popup 連携

```bash
# tmux.conf に追加してキーバインドでアクセス
bind b display-popup -E -w 80% -h 80% "baton --exit"

# ジャンプ後も一覧を維持する場合
bind b display-popup -E -w 80% -h 80% "baton"
```

### TUI キー操作

| キー | 動作 |
|------|------|
| `j` / `Down` | カーソルを下に移動 |
| `k` / `Up` | カーソルを上に移動 |
| `Enter` | 選択したペインにジャンプ |
| `Tab` | セッションリストとプレビューのフォーカス切替 |
| `/` | セッションフィルタ入力を開始 |
| `a` / `d` | 承認 / 拒否（右ペインの Waiting Claude セッション） |
| `A` / `D` | コメント付き承認 / 拒否（右ペインの Claude セッション） |
| `Esc` | サブメニューを閉じる、または有効なフィルタをクリア |
| `q` / `Ctrl+C` | 終了 |

### セッションフィルタ

- `/` で入力モードに入り、入力中にインクリメンタルで絞り込み
- マッチ対象: セッション名、作業ディレクトリ（パス）、ツール名
- 状態トークン: `waiting`, `idle`, `thinking`, `tool_use`, `working`, `error`
- `!` プレフィックスで除外（例: `!idle`）

例:

- `waiting` → Waiting のみ表示
- `!idle` → Idle 以外を表示
- `codex !idle` → Codex かつ Idle 以外を表示

### 状態グループ

セッションは以下の優先度でグループ化されます:

| グループ | アイコン | 説明 |
|---------|---------|------|
| WAITING | `!` | 承認プロンプト検出。ユーザーの操作が必要 |
| ERROR | `x` | エラー状態 |
| WORKING | `*` | 思考中またはツール実行中 |
| IDLE | `~` | ユーザー入力待ち |

## 設定

設定ファイル（任意）: `~/.config/baton/config.yaml`

```yaml
# スキャン間隔（デフォルト: 2s）
scan_interval: "2s"

# Claude Code プロジェクトディレクトリ
claude_projects_dir: "~/.claude/projects"

# ステータス JSON 出力先
status_output_path: "/tmp/baton-status.json"

# ターミナルバックエンド: "tmux"（デフォルト）または "wezterm"（レガシー）
terminal: "tmux"

# ステータスバーフォーマット（Go テンプレート）
statusbar:
  format: "{{.Active}} active / {{.TotalSessions}} total{{if .Waiting}} | {{.Waiting}} waiting{{end}}"
  tool_icons:
    claude: ""
    codex: ""
    gemini: ""
    default: "●"
  state_icons:
    working: "🤔"
    waiting: "✋"
    idle: "~"
```

## 仕組み

```
Ticker (2s)
  └── Scanner.Scan()
        ├── tmux list-panes -a          # 全ペインを検出
        ├── ps + pgrep                  # ペインごとに AI プロセスを検索
        └── JSONL ログ解析              # セッション状態を判定
  └── StateManager.UpdateFromScan()
        ├── ResolveMultiple()           # プロセスと JSONL ログのマッチング
        └── RefineToolUseState()        # 承認プロンプトの画面スクレイピング
  └── ScanResultMsg → TUI Update()
  └── Exporter.Write()                  # /tmp/baton-status.json
```

### ツール別の状態検出方式

| ツール | Working | Idle | Waiting |
|--------|---------|------|---------|
| Claude Code | JSONL `assistant` エントリ | JSONL `end_turn` | 画面: 承認プロンプトパターン |
| Codex CLI | `pgrep -P`: 子プロセスあり | 子プロセスなし | 画面: 承認プロンプトパターン |
| Gemini CLI | プロセス実行中 | - | - |

## プロジェクト構成

```
.
├── main.go                          # エントリポイント（--no-tui / --once / --exit / --config）
├── internal/
│   ├── core/
│   │   ├── model.go                 # ドメイン型（SessionState, Session, Project）
│   │   ├── parser.go                # JSONL パーサー + IncrementalReader
│   │   ├── process.go               # プロセス検出（ps/pgrep）
│   │   ├── scanner.go               # DefaultScanner（ペイン走査 + CurrentCommand フィルタ）
│   │   ├── watcher.go               # fsnotify ファイルウォッチャー + デバウンス
│   │   ├── state.go                 # 状態集約マネージャー
│   │   └── exporter.go              # アトミック JSON エクスポーター
│   ├── terminal/
│   │   ├── terminal.go              # Terminal インターフェース
│   │   ├── tmux.go                  # tmux 実装（デフォルト）
│   │   └── wezterm.go               # WezTerm 実装（レガシー）
│   ├── config/
│   │   └── config.go                # YAML 設定読み込み
│   └── tui/
│       ├── model.go                 # bubbletea Model + Init
│       ├── update.go                # キー入力・イベントハンドリング・ペインジャンプ
│       └── view.go                  # セッションリスト + プレビューペイン描画
└── wezterm/
    └── baton-status.lua             # WezTerm ステータスバープラグイン（レガシー）
```

## 開発

```bash
# テスト実行
go test ./... -v

# 静的解析
go vet ./...

# ビルド＆ローカルインストール（macOS）
go build -o baton . && cp baton ~/.local/bin/baton && codesign -f -s - ~/.local/bin/baton
```

## ライセンス

MIT

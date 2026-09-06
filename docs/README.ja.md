# baton

tmux 上の AI コーディングセッション (Claude Code / Codex / Antigravity CLI (agy) / OpenCode) をリアルタイム監視する TUI ダッシュボード。

![baton TUI](assets/preview.png)

## 概要

baton は tmux ペイン上で動作している AI コーディングセッションを自動検出し、`Attention` セクションと安定順セッション一覧を持つダッシュボードで表示します。**ペインインデクサ + ステータストラッカー + スイッチャー** として設計されており、セッションの起動は行いません。

設計思想:

- **ペイン中心**: 主キーは `TMUX_PANE`。同一 tmux セッション内の複数 AI セッションを個別に追跡
- **非侵入的**: セッションは手動起動。baton は `ps`、`tmux capture-pane`、Claude の JSONL/session-meta を使って後追い検出
- **Hook はオプション**: 状態はデフォルトでペインテキスト・子プロセス検出・JSONL フォールバックから導出する。Claude Code hooks を任意で登録すると、承認待ち（Waiting）検知が確定的になり `session_id`/`transcript_path` も取得できる。未設定でも従来通り動作する

## 機能

- リアルタイム状態監視: `Thinking` / `ToolUse` / `Waiting` / `Idle` / `Error`
- `Attention` セクションと、`project / tool / PID` の安定順セッションリスト + ターミナルプレビューペイン
- ペインジャンプ: セッションを選択して対象 tmux ペインに移動
- マルチツール対応: Claude Code, Codex CLI, Antigravity CLI (agy), OpenCode
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
go install github.com/yoshihiko555/baton@v0.1.2
```

または GitHub Releases のビルド済みバイナリ（`baton_<tag>_<os>_<arch>.tar.gz` / `.zip`）を利用してください。

リリース関連ドキュメント:

- [CHANGELOG.md](../CHANGELOG.md)

`go install` 後に `baton` が見つからない場合は、Go の bin ディレクトリが `PATH` に含まれているか確認してください。

ソースからビルド:

```bash
git clone https://github.com/yoshihiko555/baton.git
cd baton
task build

# macOS: バイナリコピー後に codesign が必須
cp baton ~/.local/bin/baton && codesign -f -s - ~/.local/bin/baton
```

`task build` は `git describe --tags --dirty --always` 由来のバージョンを埋め込みます。通常の `go build` はプロジェクトのバージョン埋め込み処理を実行しないため、ローカルで `baton --version` を確認する場合は `task build` を使ってください。

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
| `w` | 次の Waiting セッションへ移動 |
| `a` / `d` | 承認 / 拒否（右ペインの Waiting Claude/Codex セッション） |
| `A` / `D` | コメント付き承認 / 拒否（右ペインの Claude セッション） |
| `t` | 選択中の Claude/Codex セッションの安全オートモードを切替 |
| `Esc` | サブメニューを閉じる、または有効なフィルタをクリア |
| `q` / `Ctrl+C` | 終了 |

### 安全オートモード

`t` でセッション単位のオートモードを有効にしても、baton は無条件に Enter を送信しません。まずルールベースで安全判定し、曖昧な承認は既定で `gpt-5.3-codex-spark` を使う Codex exec reviewer に渡します。

- `allow`: baton が `Enter` を送信
- `ask` / `deny` / `unknown` / `error`: 停止理由を表示し、手動の `a` / `d` / `A` / `D` を待つ
- `rm`、`git reset`、秘密情報アクセス、workspace 外書き込み、外部ネットワーク送信、production/deploy 系操作は自動承認しない

### セッションフィルタ

- `/` で入力モードに入り、入力中にインクリメンタルで絞り込み
- マッチ対象: セッション名、作業ディレクトリ（パス）、ツール名
- 状態トークン: `waiting`, `idle`, `thinking`, `tool_use`, `working`, `error`
- `!` プレフィックスで除外（例: `!idle`）

例:

- `waiting` → Waiting のみ表示
- `!idle` → Idle 以外を表示
- `codex !idle` → Codex かつ Idle 以外を表示

### TUI レイアウト

左ペインは 2 層構成です:

| セクション | 内容 |
|-----------|------|
| Attention | `Waiting / Working / Idle` の件数サマリと、最大 5 件の `Waiting` セッション |
| Sessions | `project -> tool -> PID` の安定順で並ぶ、プロジェクト単位のセッション一覧 |

各セッション行には状態アイコンを残します:

| 状態 | アイコン | 説明 |
|------|---------|------|
| WAITING | `!` | 承認プロンプト検出。ユーザーの操作が必要 |
| ERROR | `x` | セッション行上のエラー状態 |
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
    agy: ""
    opencode: ""
    default: "●"
  state_icons:
    working: "🤔"
    waiting: "✋"
    idle: "~"

# 安全オートモード reviewer
auto_mode:
  enabled: true
  reviewer: "codex" # "codex" または "none"
  model: "gpt-5.3-codex-spark"
  timeout: "20s"
  risk_threshold: "medium" # low, medium, high

# Claude Code hooks（オプション）: PermissionRequest で承認待ちを確定し、
# session_id/transcript_path を紐付ける。未設定・未接続時は従来のペイン判定にフォールバックする
hook:
  enabled: true
  socket_path: "~/.local/state/baton/hook.sock"
  idle_cancel_scans: 3
  status_max_age: 10s
```

### Claude Code hooks（オプション）

baton は Claude Code hooks イベントを Unix domain socket 経由で受信し、承認待ち
（`Waiting`）検知をペインテキストのヒューリスティックより確定的にできる（完全にオプション。
未設定でも従来通り動作する）。

tmux ステータスバー（`--once`）とポップアップ（`--exit`）も、常駐インスタンスが書いた
status JSON 経由で hook 由来の `Waiting` を引き継ぐ。常駐停止後は
`hook.status_max_age`（既定 `10s`）を超えると失効する。

1. 薄いラッパースクリプト（例: `~/.claude/hooks/baton-hook.sh`）を用意する:

   ```sh
   #!/bin/sh
   command -v baton >/dev/null 2>&1 || exit 0
   exec baton hook
   ```

   常駐 `baton` を独自の `--config` フラグ付きで起動する場合は、ラッパースクリプト内の
   `baton hook` にも同じ `--config` フラグを渡す。指定が異なるとパス（特に socket
   パス）が無言で食い違い、目に見えるエラーがないまま hook イベントが従来の
   ペインテキスト判定へフォールバックする。例:

   ```sh
   exec baton hook --config /path/to/config.yaml
   ```

2. Claude Code の `settings.json` で以下 7 イベントに登録する: `PermissionRequest`、
   `PreToolUse`、`PostToolUse`、`Stop`、`UserPromptSubmit`、`SessionStart`、
   `SessionEnd`。各イベントの hook コマンドを上記ラッパーに向ける。
3. 常駐 `baton` は `hook.socket_path`（既定 `~/.local/state/baton/hook.sock`）で待ち受け、
   `PermissionRequest` を Waiting 確定として扱う（最優先。ペインテキスト判定では
   上書きされない）。解除は後続イベント・ペイン消失・`idle_cancel_scans` 連続
   Idle 判定（安全網）のいずれかで行う。
4. `baton hook` は常に exit 0 を返し、baton 未起動時や `hook.enabled: false` でも
   Claude Code の実行を妨げない。

## 仕組み

```text
Ticker (2s)
  └── Scanner.Scan()
        ├── tmux list-panes -a          # ペインと現在コマンドを検出
        ├── ps -t <tty>                 # ペインごとに AI プロセスを検索
        └── pgrep -P <pid>              # Codex の子プロセスを検査
  └── StateManager.UpdateFromScan()
        └── ResolveMultiple()           # Claude の JSONL/session-meta から基礎データを構築
  └── StateManager.RefineToolUseState()
        └── tmux capture-pane           # ペインテキストで Waiting / Idle を精緻化
  └── ScanResultMsg → TUI Update()
  └── Exporter.Write()                  # /tmp/baton-status.json
```

### ツール別の状態検出方式

| ツール | Working | Idle | Waiting |
|--------|---------|------|---------|
| Claude Code | ペインテキストのインジケーター (`✢` / `·` / `✶`) と JSONL フォールバック | 待機プロンプト (`❯` + 区切り線) と JSONL フォールバック | 画面: 承認プロンプトパターン |
| Codex CLI | `pgrep -P`: 子プロセスあり | 子プロセスなし | 画面: 番号付き承認プロンプト |
| Antigravity CLI (agy) | 画面: braille スピナー行 または `esc to cancel` | 画面: 残余（他のパターン不一致。実画面ではフッター `? for shortcuts`） | 画面: `Requesting permission for:` + `Do you want to proceed?` |
| OpenCode | 画面: `esc interrupt` または進捗バー（`■`/`⬝`） | 画面: 残余（他のパターン不一致。実画面ではフッター `tab agents  ctrl+p commands`） | 画面: `△ Permission required` |

> **注記**: OpenCode は既定設定で bash/edit ツールが自動承認されます。`opencode.json` の `permission` を `ask` にしていない環境では、OpenCode セッションの `Waiting` は検出されません。

## プロジェクト構成

```
.
├── main.go                          # エントリポイント（--no-tui / --once / --exit / --config）
├── internal/
│   ├── core/
│   │   ├── model.go                 # ドメイン型（SessionState, Session, Project）
│   │   ├── parser.go                # JSONL パーサー + IncrementalReader
│   │   ├── process.go               # プロセス検出（ps/pgrep）
│   │   ├── resolver.go              # Claude JSONL / session-meta 解決
│   │   ├── scanner.go               # DefaultScanner（ペイン走査 + CurrentCommand フィルタ）
│   │   ├── state.go                 # 状態集約マネージャー
│   │   ├── exporter.go              # アトミック JSON エクスポーター
│   │   ├── tmux_status.go           # tmux ステータスライン文字列生成
│   │   └── watcher.go               # fsnotify ファイルウォッチャー（互換用途）
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
task build && cp baton ~/.local/bin/baton && codesign -f -s - ~/.local/bin/baton
```

## ライセンス

MIT

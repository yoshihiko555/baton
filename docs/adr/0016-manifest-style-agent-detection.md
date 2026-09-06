# ADR-0016: Antigravity CLI / OpenCode / takt の検知方式（manifest 型ルールテーブル）

## Status

Accepted（ADR-0010 を supersede）

## Date

2026-09-06

## Context

baton の対応ツールは Claude Code / Codex / Gemini CLI だったが、運用環境では以下の乖離が生じていた。

1. **Gemini CLI は使われていない**: `gemini` バイナリは未インストール。代わりに Antigravity CLI（`agy`、native バイナリ）を使っているが、`toolTypeMap` / `aiCommands` に `agy` がないため一切検出されない
2. **OpenCode が未対応**: `opencode` は native バイナリで COMM が `opencode`。tmux の `pane_current_command` は nix ラッパー経由だと `.opencode-wrapp` になる
3. **takt 配下のセッションが素の Claude セッションに見える**: takt（node スクリプト）は `claude -p --output-format stream-json` や `codex exec` を **stdio=pipe の非対話**で同一 TTY に spawn する。現状これらは通常の Claude/Codex セッションとして検出され、同一 CWD の JSONL 束ね（ResolveMultiple）を消費し、takt の標準出力に対して `classifyClaudePane` が走る

参考実装として herdr（Rust、Apache-2.0）が 20 種のエージェント検知 manifest（TOML）を持つ。各 manifest は画面テキストに対する `blocked` / `working` ルールの集合で、いずれにも一致しなければ `idle` とみなす残余判定になっている。

実機採取（2026-09-06、opencode 1.18.25 / agy 1.1.24）の結果:

| ツール | idle | working | waiting |
|--------|------|---------|---------|
| agy | フッター `? for shortcuts` | braille スピナー行 `⣻  Generating...`、フッター `esc to cancel` | `Requesting permission for:` + `Do you want to proceed?` + 番号付き選択肢 |
| opencode | フッター `tab agents  ctrl+p commands` | 進捗バー `⬝⬝■■■■■■  esc interrupt` | `△ Permission required` + `Allow once   Allow always   Reject` |

herdr manifest との差分: OpenCode は `esc to interrupt` ではなく `esc interrupt`、`↑↓ select` ではなく `⇆ select`。agy は `requesting permission for:` が先頭大文字。いずれも大文字小文字無視と実画面優先で吸収する。

両ツールとも作業中に子プロセスを生成しない（bash 実行もプロセス内で完結）ため、Codex 方式の子プロセス検査は使えない。

## Decision

### 1. Gemini CLI 対応を Antigravity CLI 対応に置き換える

- `ToolGemini` → `ToolAntigravity`（`String()` は `"agy"`）。config の色キー・status JSON の `tool`・TUI 表示もすべて `agy`
- `toolTypeMap` / `aiCommands` / `toolKeyMap` の `gemini` を `agy` に置き換える。`gemini` は残さない
- `geminiIdlePattern` は削除し、下記のルールテーブルに置き換える

### 2. 画面テキスト判定を manifest 型のルールテーブルに一般化する

Claude（`classifyClaudePane`、構造ベース）と Codex（子プロセス + 承認パターン）は現行方式を維持し、**子プロセスを持たない TUI ツール（agy / opencode）** に共通の判定器を導入する。

```go
type paneRules struct {
    waiting []*regexp.Regexp // 1 つでも一致 → Waiting（最優先）
    working []*regexp.Regexp // 1 つでも一致 → Thinking
    // いずれにも一致しない → Idle（herdr の残余 idle 方式）
}
var toolPaneRules = map[ToolType]paneRules{ ToolAntigravity: ..., ToolOpenCode: ... }
func classifyByRules(rules paneRules, text string) SessionState
```

- 判定対象は `capture-pane` の全文（herdr の `whole_recent` 相当）
- 正規表現は `(?i)` で大文字小文字無視。Go RE2 では `\p{L}`、braille は `[\x{2800}-\x{28FF}]`
- pane テキストが取得できない場合はプロセス由来の既定状態（Thinking）を維持する

#### agy ルール

| 状態 | パターン |
|------|----------|
| Waiting | `(?i)requesting permission for:` かつ `(?i)do you want to proceed\?` |
| Working | 行頭 braille スピナー `(?m)^\s*[\x{2800}-\x{28FF}]+\s+\p{L}` または `(?i)esc to cancel` |
| Idle | 残余（実画面ではフッター `? for shortcuts`） |

#### opencode ルール

| 状態 | パターン |
|------|----------|
| Waiting | `△ Permission required` または `(?i)allow once\s+allow always\s+reject` |
| Working | `(?i)esc (to )?interrupt` または `(■\|⬝){4,}` |
| Idle | 残余（実画面ではフッター `tab agents  ctrl+p commands`） |

`opencode serve`（takt が HTTP バックエンドとして起動する）は対話セッションではないため、ARGS に `serve` サブコマンドを含む場合は検出対象から除外する。

### 3. takt 配下のセッションにラベルを付ける

- `ps -t <tty> -o pid,ppid,comm,args` の結果は同一 TTY の全プロセスを含むため、追加の exec なしに PPID を辿れる。祖先の ARGS に `node_modules/takt/` を含む `node` があれば `DetectedProcess.Via = "takt"`
- tmux セッション名が `takt-claude-terminal-` で始まる pane の Claude セッションも `Via = "takt"`（takt の `claude-terminal` provider は別 tmux セッションを開く）
- `Via == "takt"` のセッションは
  - CWD 束ね（ResolveMultiple）の対象から除外する（hook 由来 transcript_path があれば ResolvePath は従来通り使う）
  - `RefineToolUseState` の pane 精緻化をスキップする（pane には takt の出力しか映らない）
  - JSONL 由来の Waiting は ToolUse に降格する（`--permission-mode` 固定の非対話。ただし hook 由来の Waiting は尊重する）
- TUI は `claude (takt)` のように表示し、status JSON に `via` を追加する。takt 自身の行は作らない
- 承認操作（Enter 送信）は Claude / Codex の allowlist のまま。agy / opencode / takt 配下は対象外

## Rationale

| 案 | 判断 |
|----|------|
| herdr へ移行 | 却下。並列運用の実態がなく移行コストに見合わない（digital-garden 2026-09-02） |
| gemini を残して agy を追加 | 却下。gemini バイナリが存在せず検証不能なコードを残すことになる |
| agy / opencode を既存の Gemini 分岐に相乗り | 却下。ツールごとの if 分岐が増え続ける。ルールテーブルにすれば PR B 以降はテーブル追記で済む |
| herdr manifest を TOML のまま読み込む | 却下。設定ファイルの互換性維持コストが baton の規模に対して過大。Go の定数テーブルで十分 |
| takt を独立した ToolType（1 行に集約） | 却下。Notion の完了条件は「配下として識別」であり、子のラベル付けで満たせる。集約はステップ間の空白期間の扱いなど設計が増える |

## Consequences

### Positive

- agy / opencode が Idle / Thinking / Waiting の 3 状態で監視できる
- takt 配下の headless セッションが通常セッションの JSONL 束ねを汚さなくなる（既存 Claude 検知の回帰修正でもある）
- ツール追加が `toolTypeMap` / `aiCommands` / `toolPaneRules` / theme の 4 箇所の追記で済む

### Negative

- config の `theme.tools.gemini` は無効になる（`agy` に書き換えが必要。CHANGELOG に BREAKING として記載）
- 残余 Idle 方式は、未知の作業中表示を Idle と誤判定する可能性がある。新バージョンの TUI 文言変更時はルール追記で追従する
- OpenCode は既定設定で bash / edit が自動承認されるため、`permission` を `ask` にしていない環境では Waiting が発生しない（baton の問題ではないが README に注記する）

## 参考

- herdr manifest: https://github.com/herdrdev/herdr/tree/master/src/detect/manifests（Apache-2.0）
- 実画面採取ログ: 2026-09-06 セッション（opencode 1.18.25 / agy 1.1.24、tmux 160x50）
- takt の spawn 方式: `dist/infra/claude-headless/headless-spawn.js`（stdio pipe、detached なし）、`dist/infra/claude-terminal/tmux-backend.js`（`tmux new-session -d -s takt-claude-terminal-<uuid>`）

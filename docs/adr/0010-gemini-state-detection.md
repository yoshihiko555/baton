# ADR-0010: Gemini CLI の状態検出方式

## Status

Accepted

## Date

2026-03-21

## Context

baton は AI セッション（Claude Code / Codex / Gemini CLI）の状態をリアルタイム監視するが、Gemini CLI は以下の理由で既存の検出方式では認識できなかった:

1. **プロセス名の不一致**: Gemini CLI は Node.js ランタイムで動作し、`ps -o comm` の結果が `node` となる。従来の `toolTypeMap["gemini"]` による COMM 完全一致では検出不可
2. **tmux CurrentCommand の不一致**: tmux の `pane_current_command` も `node` を返すため、`isAICommand` フィルタで除外されていた
3. **子プロセス構造の違い**: Gemini の子プロセスは MCP サーバー（Pencil, cocoindex）のみで、ツール実行時に作業用子プロセスを生成しない。Codex 方式（`pgrep -P` で子プロセス有無を検査）は適用不可
4. **親子プロセスの重複**: Gemini は `node → node` の親子構造で起動し、両方の ARGS に `gemini` が含まれるため、2重検出が発生する

## Decision

3層のアプローチで Gemini の検出と状態判定を実装する。

### 層1: プロセス検出（process.go）

- `ps` コマンドに `args` 列を追加（`pid,ppid,comm,args`）
- COMM で AI ツール名にマッチしない場合、ARGS の各トークンの basename を `toolTypeMap` と完全一致で照合
- 同一ツールの親子プロセスは PPID ベースで重複排除（親のみ採用）

### 層2: スキャナーフィルタ（scanner.go）

- `isAICommand` で `node` を通過させ、process.go の ARGS 解析に委ねる

### 層3: 状態精緻化（state.go）

- デフォルト状態: Thinking（プロセス存在 = 作業中と仮定）
- `RefineToolUseState` で `tmux capture-pane` のテキストを解析:
  - 承認パターン（`Allow? [y/N]` 等）→ Waiting
  - Gemini ステータスバー（`workspace (...) ... sandbox`）→ Idle
  - いずれにもマッチしない → Thinking 維持

### 副次修正

- `backgroundCommands` に `uv` を追加（Codex の子プロセス誤判定を修正）

## Rationale

### 検討した代替案

| 方式 | 却下理由 |
|------|---------|
| Gemini セッション JSON 解析 | `~/.gemini/tmp/{hash}/chats/` のハッシュ計算が必要で複雑 |
| 子プロセス検査（Codex 方式） | 実機検証で子プロセスが MCP サーバーのみと判明 |
| COMM の部分一致 | `node` は汎用的すぎて誤検知リスクが高い |

### ARGS basename 完全一致を選択した理由

- `/gemini-beta` や `/claude-wrapper` 等のツール名を含むがツール自体ではないバイナリを誤検知しない
- `strings.Contains(lower, "/"+name)` による部分一致よりも安全
- レビューで指摘された誤検知リスク（High）を解消

### ステータスバーパターンを選択した理由

- Gemini の `> ` プロンプトは入力テキストの有無で表示が変わるため不安定
- `workspace (/directory) ... sandbox` はアイドル時に常に表示される Gemini 固有の UI 要素
- 他のツール（Claude, Codex）のペインテキストには出現しないため誤検知リスクが低い

## Consequences

### Positive

- Gemini セッションが baton で検出・状態表示されるようになった
- Idle / Thinking / Waiting の3状態を判別可能
- 既存の Claude / Codex 検出に影響なし（COMM 優先、ARGS はフォールバック）
- Codex の `uv` 子プロセス誤判定も修正

### Negative

- `node` ペインで追加の `ps` 実行が発生する（パフォーマンスレビューで指摘済み、問題発生時に対処）
- Gemini の Thinking 状態で追加の `capture-pane` が実行される
- Gemini の UI パターンが変更された場合、正規表現の更新が必要

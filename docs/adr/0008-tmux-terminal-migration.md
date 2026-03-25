# ADR-0008: WezTerm から tmux への Terminal 実装移行

## Status

Accepted

## Date

2026-03-20

## Context

baton v2 は WezTerm 前提で Terminal インターフェースを実装していたが、ユーザーの運用が tmux に完全移行したため、baton も tmux ベースに作り変える必要が生じた。

主な課題:
- `Pane.ID` が `int` 型だったが、tmux の pane_id は `%5` のような string 形式
- WezTerm 固有のフィールド（`TabID`, `Workspace`）が Pane 構造体に混在
- tmux には `CurrentCommand`（pane_current_command）という WezTerm にない有用な情報がある
- Codex プロセスの状態が常に Thinking 固定で、Working/Idle の区別ができなかった

## Decision

### 1. Pane.ID を string 型に変更

tmux の `%N` 形式と WezTerm の数値 ID の両方を統一的に扱うため、`Pane.ID` を `int` → `string` に変更。WezTerm 側は `strconv.Itoa` でラップする。

### 2. Pane 構造体の再設計

- `TabID` 削除（WezTerm 固有）
- `Workspace` → `SessionName`（tmux セッション名と同等の役割）
- 新規フィールド: `CurrentCommand`, `SessionAttached`, `WindowIndex`, `PaneIndex`

### 3. TmuxTerminal 実装の追加

- `ListPanes`: `tmux list-panes -a` でタブ区切り出力をパース
- `FocusPane`: switch-client → select-window → select-pane（同期的、WezTerm の 2 秒 sleep 不要）
- `GetPaneText`: `tmux capture-pane -p -J` で末尾 80 行
- `IsAvailable`: `exec.LookPath("tmux")`
- Hook セッション除外: `^claude-.*-\d{4,}$` パターンの unattached セッション

### 4. Scanner の CurrentCommand フィルタ

tmux は `pane_current_command` を提供するため、AI ツール以外のペインで `ps` を実行する必要がない。`isAICommand()` で "claude", "codex", "gemini" を判定し、非 AI ペインをスキップ。WezTerm は `CurrentCommand` が空のため従来通り全ペインをスキャン。

### 5. Codex 子プロセス検査

`pgrep -P {pid}` で子プロセスの有無を検査し、Codex セッションの Working/Idle を判定。

### 6. デフォルト terminal を tmux に変更

`config.go` のデフォルト値を `"wezterm"` → `"tmux"` に変更。`main.go` の `initTerminal` で `""` と `"tmux"` を TmuxTerminal にルーティング。

## Rationale

- tmux は pane_id が string 形式のため、int 型では自然に表現できない
- `CurrentCommand` フィルタにより、多数のペインがある環境での `ps` 実行回数を大幅に削減
- tmux の FocusPane は同期的に完了するため、WezTerm のような 2 秒 sleep が不要
- Codex の子プロセス検査は `pgrep` ベースでシンプルに実装でき、状態精度が向上

### 却下案

- **Pane.ID を interface{} にする**: 型安全性が失われる。string に統一する方が比較・マップキーでの利用が自然
- **WezTerm 実装を削除する**: レガシー対応として残し、config で切り替え可能にする

## Consequences

### Positive

- tmux 環境で正確なセッション監視が可能になった
- CurrentCommand フィルタで不要な ps 呼び出しを削減
- Codex の Working/Idle 状態が判別可能になった
- hook セッションが自動除外され、TUI のノイズが減少

### Negative

- Pane 構造体に tmux 固有フィールドが混在（将来的に TerminalMetadata 等への分離を検討）
- WezTerm 実装は維持コストとして残る（使用頻度が下がるため陳腐化リスク）
- `RefineToolUseState` が mutex 保持中に外部 IO を行う問題は未解決（別タスクで対応予定）

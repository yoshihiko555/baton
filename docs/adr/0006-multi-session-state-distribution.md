# ADR-0006: 同一CWD複数セッションの状態分布方式

## Status

Accepted

## Date

2026-03-15

## Context

baton は WezTerm ペインのプロセスを外部から検出し、Claude Code の JSONL ログファイルから状態を判定する。同一 CWD で複数の Claude Code セッションが並列実行されている場合、PID と JSONL ファイルの1対1対応が取れないという根本的な課題がある。

Claude Code は JSONL ファイル名に UUID を使用し、PID 情報をファイル名やメタデータに含めない。`lsof` でも書き込み時のみファイルをオープンするため捕捉できない。`ps -o args` でもコマンドライン引数にセッション ID は含まれない。

Claude Code の statusline 機能が `session_id` と `transcript_path` を JSON で出力することは確認したが、これは Claude Code 固有の機能であり、Codex/Gemini には存在しない。また、ユーザーの statusline 設定に依存する方式は採用できない。

類似ツール（claude-squad, ccmanager, claude-session-manager）はすべて「自分でセッションを起動する」方式を採用しており、外部監視で個別セッションを識別するツールは存在しない。

## Decision

PID と JSONL の1対1対応は行わず、同一 CWD のセッション群に対して JSONL から「状態分布」を取得して割り当てる。

1. `StateResolver.ResolveMultiple(cwd, count)` で同一 CWD の JSONL を ModTime 降順でソートし、上位 `count` 個から状態を判定する
2. 判定結果を重要度順（Waiting > Error > Thinking > ToolUse > Idle）にソートする
3. `StateManager` が CWD ごとに Claude セッションをグループ化し、重要度順に状態を割り当てる

## Rationale

- **Waiting の見逃し防止が最優先**: Waiting 状態の JSONL は直近に更新されるため、ModTime 上位 N 個に必ず含まれる。重要度順ソートにより Waiting は最初に割り当てられる
- **PID との紐付けは不可能**: 外部監視方式では PID と JSONL の確実な対応付け手段がない（調査済み）
- **プロジェクト単位の状態分布は正確**: どの PID がどの状態かは不明だが、「このプロジェクトに waiting: 1, idle: 3 がある」という情報は正確に出せる

比較した代替案:

- **プロセス起動時刻と JSONL 作成時刻の照合**: `--resume` / `--continue` 使用時に大きくずれるため不採用
- **JSONL 最終エントリでアクティブ/終了判定**: 異常終了したセッションの JSONL が大量に誤判定されるため不採用（検証で50件がアクティブと誤判定）
- **statusline JSON の利用**: Claude Code 固有機能であり、Codex/Gemini 非対応。ユーザーの設定への侵入が必要なため不採用
- **起動ラッパー方式への転換**: baton の「外部監視」という設計思想と矛盾するため現時点では不採用

## Consequences

### Positive

- 同一 CWD の複数セッションでも Waiting の見逃しを防止できる
- プロジェクト行の Description に正確な状態分布が表示される（例: `waiting: 1 · thinking: 2 · idle: 1`）
- 単一セッション（CWD ごとに1プロセス）の場合は完全に正確な状態表示が得られる
- Codex/Gemini のプロセスにも影響しない（プロセス存在＝active として扱う既存動作を維持）

### Negative

- セッション行（右ペイン）の個別 PID と状態の対応が不正確になる可能性がある
- TUI で Enter → ペインジャンプした先が、表示された状態と異なるセッションである可能性がある
- PID と JSONL の対応付けが将来的に Claude Code の公開 API で可能になった場合、設計の見直しが必要

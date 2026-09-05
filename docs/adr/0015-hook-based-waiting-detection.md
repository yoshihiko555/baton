# ADR-0015: Claude Code hooks による Waiting 検知の確定と session 相関

## Status

Accepted

## Date

2026-09-05

## Context

現行の Claude Code 状態検知は `classifyClaudePane`（`internal/core/state.go`）が権威的ソースであり、ペインテキストを逆順スキャンして Idle / Working / Waiting を判定する。判定不能な場合は JSONL 由来の状態を維持するが、JSONL が Waiting のときだけは `ToolUse` に降格する分岐（`state.go:380` 付近）がある。

この降格分岐は「承認待ちの検知が甘い」というユーザー不満の最有力候補である。ただし、具体的な取りこぼし例（どのプロンプト UI パターンで `classifyClaudePane` が判定に失敗するか）はまだ記録できていない。

一方で、pane 単位の状態管理を先行実装している herdr（Rust 製、Apache-2.0）を調査したところ、herdr は「hooks で working/blocked/idle を全て報告する」設計を一度採用したのち、意図的に放棄していた。現行の herdr は `SessionStart` イベントのみを使い pane↔session の相関を取るだけで、状態そのものは画面照合（screen manifest）が権威である。放棄の背景には、`SubagentStop` など非決定的なタイミングで発火するイベントが durable な working 状態を誤って復活させる、といった誤検知の修正履歴が複数回見られる（herdr リポジトリのコミット `295b09ca` 前後の履歴。ローカル調査ノートは git 管理外）。

Claude Code hooks の公式仕様を確認したところ、`PermissionRequest` は承認ダイアログの表示と同時（決定的なタイミング）に発火し、`session_id` / `transcript_path` / `cwd` / `tool_name` / `tool_input` を持つ。一方で `Notification` の `permission_prompt` はダイアログ表示から 6 秒遅延して発火する。承認・拒否が解消されたことを直接通知するイベントは存在しない（`PermissionDenied` は auto mode 専用）。hook プロセスは親 `claude` プロセスの環境変数を継承するため、`$TMUX_PANE` は tmux の pane ID（baton の `Pane.ID`、`%5` 形式）とそのまま一致する。サブエージェントの承認プロンプトは親 TUI に表示され、対応する `PermissionRequest` には `agent_id` が付与される。

これらを踏まえ、「hooks で全状態を報告する」設計は herdr がすでに放棄した領域であり、baton が同じ轍を踏む必要はない。一方で `PermissionRequest` は決定的なタイミングで発火するイベントであり、`classifyClaudePane` が苦手とする「承認待ちの確定検知」にはピンポイントで有効である。

## Decision

baton における hooks の役割を以下の 2 つに限定する。

1. **`PermissionRequest` による Waiting の確定** — herdr が放棄した「hooks で全状態報告」ではなく、Waiting 1 状態のみを hooks の担当領域とする
2. **`session_id` / `transcript_path` の pane への紐付け** — herdr の現行設計と同じく、hooks は相関情報の提供に留める

working / idle の判定は引き続き `classifyClaudePane`（画面照合）が権威とし、hooks からは導出しない。

優先順位と解除条件は以下の通り。

- hook 由来の Waiting は `classifyClaudePane` の判定より**優先**する（上書きされない）
- 解除は次の 3 つのイベントのみに限定し、**TTL は設けない**（離席中に数時間の承認待ちが発生し得るため、時間経過による自動失効は誤動作の原因になる）
  - 後続 hook イベント（`PreToolUse` / `PostToolUse` / `Stop` / `UserPromptSubmit` / `SessionEnd`）の受信
  - 対象ペインが tmux スキャンから消えた
  - 画面判定が Idle を `hook.idle_cancel_scans`（既定 3）スキャン連続で返した（hook 取りこぼし時の安全網）

登録する hook イベントは `PermissionRequest` `PreToolUse` `PostToolUse` `Stop` `UserPromptSubmit` `SessionStart` `SessionEnd` の 7 種類。baton 側の解釈は「`PermissionRequest` → Waiting、それ以外 → 解除」の 2 値のみとし、herdr のような多段状態機械は持たない。`agent_id` 付き（サブエージェント由来）の `PermissionRequest` も同様に採用する。

輸送路には Unix domain socket（既定 `/tmp/baton-hook.sock`、`hook.socket_path` で変更可、パーミッション 0600）を用いる。`baton hook` サブコマンドは stdin から受け取った hook JSON に `$TMUX_PANE` から取得した `pane_id` を添えて常駐 baton にそのまま転送するだけの薄いクライアントとし、解釈は常駐側（listen している 1 プロセス）に集約する。常駐していない `--exit` / `--once` 起動は listen しない。

詳細設計は `docs/design/detailed/hook-state-detection.md` を参照。

## Rationale

- **herdr の失敗を繰り返さない**: hooks による全状態報告は herdr がすでに誤検知で放棄した設計。baton は herdr が「権威」として残した画面照合を維持したまま、hooks は herdr も採用している「決定的イベントの検知」だけに使う
- **`PermissionRequest` は決定的**: 承認ダイアログ表示と同時に発火するため、`classifyClaudePane` のテキストパターンマッチより取りこぼしが少ない
- **TTL を設けない**: 承認待ちは人間の操作待ちであり、経過時間とは無関係に有効であり続けるべき状態。TTL は離席中の長時間待ちを誤って消してしまう
- **常駐前提でも安全**: baton 再起動時は hook 由来の Waiting が失われるが、次の `PermissionRequest` で復帰し、それまでは従来の画面判定にフォールバックする。既存の Waiting 検知が完全に失われるわけではない
- **相関情報は副産物として安価に取れる**: `session_id` / `transcript_path` は `PermissionRequest` 等のペイロードに元々含まれるため、Waiting 検知と同じ輸送路で追加コストなく取得できる

比較した代替案:

- **herdr へ移行する**: 移行コストが重く、hook 検知自体は herdr 固有の技術ではない。しかも herdr 自身がすでに「hooks で全状態報告」を放棄しているため、移行しても得られるものが少ない
- **hooks で working/idle も含めた全状態を報告する（旧 herdr 方式）**: `SubagentStop` 等の非決定的なタイミングで誤検知が発生することが herdr のコミット履歴から確認できており、不採用
- **ペインごとの状態ファイル + fsnotify ポーリング**: baton は常駐プロセスであることが前提のため、socket による即時プッシュの方が反応性で優る。また既存の `watcher.go`（fsnotify）はデッドコード化しており JSONL 前提の設計であるため、この用途には再利用できない
- **TTL による失効**: 長時間の承認待ち（離席中など）を誤って解除してしまうリスクが高く、不採用

## Consequences

### Positive

- 承認待ち（Waiting）の検知精度が、決定的イベントである `PermissionRequest` により向上する
- `classifyClaudePane` は working / idle の判定に専念でき、責務が単純化する
- `session_id` / `transcript_path` の相関情報が hooks 経由で確実に取得できるようになり、将来の resolver 1:1 化（Phase 4）の土台になる
- herdr の失敗を踏まえた設計のため、非決定的イベントによる誤検知リスクを構造的に排除できる
- hooks 未設定の環境でも、従来の画面判定にそのままフォールバックするため後方互換性を維持できる
- 自動承認モード（`checkAutoApprove`）は PaneID ベースの Waiting 立ち上がり検知のみを見るため、hook 由来の Waiting が加わってもロジック変更なしでそのまま機能する

### Negative

- hooks の設定（dotfiles 側の `settings.json` 追加）が別途必要になり、未設定の環境では恩恵を受けられない
- baton 常駐プロセスの再起動時に hook 由来の Waiting 状態が失われる（次の `PermissionRequest` まで画面判定に戻る）
- socket サーバーの追加により、常駐プロセスの障害点が 1 つ増える（stale socket ファイルの削除、リスナー異常時のフェイルセーフ実装が必要）
- `hook.idle_cancel_scans` のスキャン回数はヒューリスティックであり、環境によっては解除が早すぎる／遅すぎる可能性がある

# ADR-0007: ペインジャンプの別ワークスペース切り替え方式

## Status

Accepted

## Date

2026-03-16

## Context

baton の TUI では Enter キーで選択セッションの WezTerm ペインに直接ジャンプする機能がある。同一ワークスペース内のペインは `wezterm cli activate-pane --pane-id N` で即座にフォーカスできる。

しかし WezTerm はワークスペースを複数持てるため、baton が動作しているワークスペースとは別のワークスペースにペインが存在する場合がある。`activate-pane` は別ワークスペースのペインには直接適用できず、先に `wezterm cli switch-workspace` または GUI 操作でワークスペースを切り替える必要がある。

baton は外部監視ツールであり、WezTerm の GUI イベントを直接発火させる手段を持たない。また `wezterm cli switch-workspace` コマンドは baton v2 実装時点では Go 側から呼び出し可能だが、WezTerm の現在のウィンドウフォーカスを切り替える際のタイミング制御が難しく、activate-pane と組み合わせたときに競合が生じるケースがあった。

既存の Alfred ワークフロー連携（`setup_alfred_watcher`）では、`/tmp/wezterm-alfred-workspace.json` にワークスペース名と CWD を書き込むと WezTerm Lua の `update-status` イベントがそれを検知して `SwitchToWorkspace` を実行する仕組みが稼働している。

## Decision

別ワークスペースへのペインジャンプは、Alfred watcher が使用するトリガーファイル (`/tmp/wezterm-alfred-workspace.json`) 経由でワークスペース切り替えを依頼し、その後 `activate-pane` でペインにフォーカスする。

実装フロー:

1. `ListPanes()` で全ペイン情報を取得し、現在の WS（`$WEZTERM_PANE` 環境変数）と対象ペインの WS を比較する
2. WS が異なる場合、トリガーファイルにターゲット WS 名・CWD・タイムスタンプを JSON で書き込む
3. WezTerm Lua（`update-status` イベント）がファイルを検知し `SwitchToWorkspace` を実行するまで 2 秒待機する
4. `wezterm cli activate-pane --pane-id N` でペインにフォーカスする

TUI 側では `jumping` フラグを立ててキー入力をブロックし、「Switching workspace...」メッセージを表示する。

## Rationale

- **既存機構の再利用**: Alfred watcher トリガーファイルは既に稼働しており、同じプロトコルを使うことで Lua 側の変更が不要
- **WezTerm CLI の制約回避**: `wezterm cli switch-workspace` は存在するが、GUI ウィンドウのフォーカス切り替えと activate-pane のタイミングを Go 側で確実に制御するのが困難
- **実装の単純さ**: ファイル書き込み + sleep + activate-pane という直列処理で競合なく実装できる

比較した代替案:

- **`wezterm cli switch-workspace` の直接呼び出し**: コマンドは存在するが、切り替え完了のシグナルを受け取る手段がなく、activate-pane とのタイミング制御が難しいため不採用
- **`wezterm cli spawn --workspace` でペインを再作成**: ジャンプではなく新規ペイン作成になるため要件を満たさない
- **WezTerm の MCP/IPC**: v2 実装時点で baton が利用できる安定した IPC 手段がないため不採用

## Consequences

### Positive

- Alfred watcher が稼働している環境では、別 WS のペインへもシームレスにジャンプできる
- Lua 側の変更が不要で、既存の Alfred ワークフロー連携と共存できる
- ジャンプ中の `jumping` フラグにより、切り替え待機中の誤操作を防止できる

### Negative

- Alfred watcher（WezTerm Lua の `setup_alfred_watcher`）が設定されていない環境ではワークスペース切り替えが機能しない（activate-pane のみ実行され、別 WS のペインへのジャンプが失敗する場合がある）
- 2 秒の固定 sleep がある。Lua の `update-status` 発火間隔によっては不足する可能性があり、ハードコードされた待機時間は実環境依存
- `$WEZTERM_PANE` 環境変数が未設定の場合、現在の WS が特定できず常に同一 WS と判定される

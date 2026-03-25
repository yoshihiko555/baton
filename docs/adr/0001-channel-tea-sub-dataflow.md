# ADR-0001: Channel + tea.Sub によるデータフロー

## Status

Accepted

## Date

2026-03-02

## Context

ファイルウォッチャーからの変更イベントを TUI（bubbletea）に伝達する仕組みが必要だった。主な選択肢として以下があった:

1. **Polling**: 一定間隔でファイルシステムを再スキャン
2. **Channel + tea.Sub**: Go channel 経由でイベントを bubbletea のメッセージループに統合

## Decision

Channel + `tea.Sub` パターンを採用する。

- `Watcher` が fsnotify イベントを Go channel (`Events()`) に送出
- bubbletea の `tea.Sub` で channel を購読し、`WatchEventMsg` として `Update()` に配信

## Rationale

- bubbletea の純粋関数モデル（Elm Architecture）と自然に合致する
- Polling より効率的（変更があった時のみ処理が走る）
- Go の channel は複数ゴルーチン間の通信に適しており、デバウンスとの組み合わせも容易

## Consequences

### Positive

- イベント駆動で無駄な処理が発生しない
- bubbletea の `Cmd` / `Msg` パターンに統合されるため、テストが書きやすい
- `--no-tui` モードでも同じ channel を直接 `select` で受信でき、コード共有が容易

### Negative

- channel のバッファサイズやブロッキングに注意が必要
- 初期スキャンと channel 受信の順序を正しく制御しないとデッドロックの可能性がある（実際に修正済み: `watcher.Start()` から初期スキャンを分離）

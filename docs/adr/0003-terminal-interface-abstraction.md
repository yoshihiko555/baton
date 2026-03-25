# ADR-0003: Terminal インターフェース抽象化

## Status

Accepted

## Date

2026-03-02

## Context

baton は WezTerm のペインフォーカス機能を使用してセッションへのジャンプを実現する。ただし、ターミナル実装への直接依存は以下の問題を引き起こす:

- テスト時に実際の WezTerm CLI が必要になる
- 将来的に他のターミナル（Kitty, iTerm2 等）に対応する際に大幅な変更が必要

## Decision

`Terminal` インターフェースを定義し、WezTerm 実装をその具象型とする。

```go
type Terminal interface {
    Name() string
    IsAvailable() bool
    FocusPane(paneID string) error
}
```

## Rationale

- v1 時点では WezTerm のみだが、テスト容易性のためにインターフェースを導入する価値がある
- mock 差し替えにより、TUI のテストでターミナル操作を模擬できる
- 将来の拡張性を最小コストで確保

## Consequences

### Positive

- テストで mock を注入でき、WezTerm CLI への依存なしにテスト可能
- 新ターミナル対応時はインターフェースを実装するだけで済む

### Negative

- v1 では WezTerm のみなので、過剰設計に見える可能性がある（ただしテスト容易性で十分に正当化される）

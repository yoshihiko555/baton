# Architecture Decision Records (ADR)

baton プロジェクトのアーキテクチャ上の意思決定を記録する。

## ADR 一覧

| ID | タイトル | 日付 | ステータス |
|----|---------|------|-----------|
| [ADR-0001](0001-channel-tea-sub-dataflow.md) | Channel + tea.Sub によるデータフロー | 2026-03-02 | Accepted |
| [ADR-0002](0002-separate-state-exporter.md) | state.go と exporter.go の責務分離 | 2026-03-02 | Accepted |
| [ADR-0003](0003-terminal-interface-abstraction.md) | Terminal インターフェース抽象化 | 2026-03-02 | Accepted |
| [ADR-0004](0004-incremental-reader-offset.md) | IncrementalReader による offset 管理 | 2026-03-02 | Accepted |
| [ADR-0005](0005-workspace-based-project-grouping.md) | ワークスペースベースのプロジェクトグルーピング | 2026-03-14 | Accepted |
| [ADR-0006](0006-multi-session-state-distribution.md) | 同一CWD複数セッションの状態分布方式 | 2026-03-15 | Accepted |
| [ADR-0007](0007-pane-jump-workspace-switch.md) | ペインジャンプの別ワークスペース切り替え方式 | 2026-03-16 | Accepted |
| [ADR-0008](0008-tmux-terminal-migration.md) | WezTerm から tmux への Terminal 実装移行 | 2026-03-20 | Accepted |
| [ADR-0009](0009-exit-on-jump-option.md) | ペインジャンプ後の自動終了をオプション化 | 2026-03-20 | Accepted |
| [ADR-0010](0010-gemini-state-detection.md) | Gemini CLI の状態検出方式 | 2026-03-21 | Accepted |

## ADR の書き方

新しい ADR を追加する場合:

1. `NNNN-short-title.md` の形式でファイルを作成
2. 以下のテンプレートに従って記述
3. この README の一覧に追加

### テンプレート

```markdown
# ADR-NNNN: タイトル

## Status

Proposed | Accepted | Deprecated | Superseded by [ADR-XXXX](XXXX-xxx.md)

## Date

YYYY-MM-DD

## Context

背景・課題・制約条件

## Decision

採用した解決策

## Rationale

選択の理由、比較した代替案

## Consequences

### Positive

- 良い影響

### Negative

- トレードオフ、リスク
```

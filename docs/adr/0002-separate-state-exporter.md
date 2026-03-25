# ADR-0002: state.go と exporter.go の責務分離

## Status

Accepted

## Date

2026-03-02

## Context

セッション状態の集約とステータス JSON の書き出しを、1 つのモジュールにまとめるか分離するかの判断が必要だった。

## Decision

`state.go`（状態集約）と `exporter.go`（JSON 書き出し）を別ファイルに分離する。

- `state.go`: `StateManager` がプロジェクト・セッション単位で状態を集約
- `exporter.go`: `WriteStatusJSON()` がアトミック書き込み（temp + rename）で JSON を出力

## Rationale

- 単一責任の原則に従い、集約ロジックと I/O を分離
- `--no-tui` モードでのテストが容易になる（exporter を単独でテスト可能）
- 将来的に出力先を変更する場合（ファイル以外への出力）、exporter のみ変更すれば済む

## Consequences

### Positive

- テスト時に集約ロジックと I/O を独立して検証できる
- exporter の書き出し方式（アトミック書き込み）を集約ロジックから隠蔽

### Negative

- ファイル数が増える（ただし各ファイルは小さく保たれる）

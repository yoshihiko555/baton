# baton v2 実装タスク

v1 → v2 移行の実装タスクをフェーズ別に管理する。

## フェーズ構成

| ファイル | フェーズ | 概要 |
|---------|---------|------|
| [phase1.md](phase1.md) | Phase 1: 基盤型 | ドメイン型・パーサー・設定の再定義 |
| [phase2.md](phase2.md) | Phase 2: インフラ層 | Terminal拡張・ProcessScanner・Scanner |
| [phase3.md](phase3.md) | Phase 3: ドメイン層 | StateResolver・StateManager・Exporter |
| [phase4.md](phase4.md) | Phase 4: プレゼンテーション層 | TUI・main.go・Luaプラグイン |
| [phase5.md](phase5.md) | Phase 5: テスト・品質 | テスト作成・更新・静的解析 |

## 依存関係

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5
```

同一フェーズ内のタスクは並列実施可能。

## 参照

- 移行計画: `docs/design/detailed/migration.md`
- 詳細設計: `docs/design/detailed/`
- 要件定義: `docs/requirements/requirements-v2.md`

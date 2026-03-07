# baton v2 詳細設計書

**前提文書**:
- 要件定義: `docs/requirements/requirements-v2.md` (v0.3)
- 基本設計: `docs/design/basic-design.md` (v0.4)
- アーキテクチャ概要: `docs/architecture/overview.md`

---

## ドキュメント一覧

| ファイル | 対象コンポーネント | 概要 |
|---------|------------------|------|
| [model.md](model.md) | `internal/core/model.go` | ドメイン型・列挙型・インターフェース定義の変更 |
| [scanner.md](scanner.md) | `internal/core/scanner.go`, `process.go` | Scanner オーケストレータ + ProcessScanner の新規設計 |
| [state-resolver.md](state-resolver.md) | `internal/core/resolver.go` | CWD→JSONL 解決、状態判定、session-meta 取得 |
| [parser.md](parser.md) | `internal/core/parser.go` | JSONL パーサーの拡張（Entry 型、状態判定ロジック変更） |
| [state-manager.md](state-manager.md) | `internal/core/state.go` | スナップショット照合方式への全面改修 |
| [exporter.md](exporter.md) | `internal/core/exporter.go` | version:2 スキーマ + formatted_status 対応 |
| [terminal.md](terminal.md) | `internal/terminal/` | Terminal IF の拡張（TTYName, IsActive, int 型 ID） |
| [tui.md](tui.md) | `internal/tui/` | ticker 駆動・Waiting 表示・サブメニュー対応 |
| [main-entrypoint.md](main-entrypoint.md) | `main.go` | ワイヤリング・ライフサイクルの再設計 |
| [lua-plugin.md](lua-plugin.md) | `wezterm/baton-status.lua` | version:2 + formatted_status 対応 |
| [migration.md](migration.md) | 全体 | v1→v2 ファイル別移行計画 |

---

## 設計レベル

本詳細設計書は**設計判断レベル**で記述する:

- コンポーネント間の契約（入出力、呼び出し順序）
- データ変換ルール（型変換、正規化、フォールバック）
- 状態遷移・判定ロジックの詳細
- エラー処理の分岐と対応方針
- v1 からの変更差分と移行手順

関数シグネチャの擬似コードは含むが、実装レベルの詳細は省略する。

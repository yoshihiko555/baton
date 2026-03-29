# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `main` 基準のリリース運用を定義した `docs/release-process.md` を追加
- タグ push から GitHub Release を自動作成する workflow を追加
- GitHub Release のカテゴリ設定と PR テンプレートを追加
- TUI セッションフィルタ機能を追加（`/` で開始、名前/パス/ツール + 状態トークンで絞り込み）
- `--once --format tmux` で tmux ステータスバー向けの軽量サマリ出力（`🤔/✋/💤`）を追加
- `statusbar.state_icons` で tmux ステータスサマリの `working/waiting/idle` アイコンを設定可能にした
- セッション単位の自動承認モード（`t` キーでトグル、`[AUTO]` バッジ表示）
- Claude Code 状態判定の行単位逆順スキャン方式（`classifyClaudePane`）

### Changed

- README / `docs/README.ja.md` のキー操作・機能一覧を現行実装（承認操作・セッションフィルタ）に更新
- `a/d/A/D` で承認/拒否した直後にプレビューを再取得するようにし、遅延反映時の取りこぼしを減らした
- tmux 向け軽量サマリの `idle` 既定アイコンを `💤` から `~` に変更し、背景色に埋もれにくくした
- Claude Code の承認プロンプト検出を正規表現ベースから構造ベース（選択肢 UI パターン）に変更
- 承認パターンの厳密化（`"allow"` 部分一致を廃止、`Allow <tool>? (y)` 等の具体的パターンに限定）
- `ReadLastEntry` の読み取りバッファを 4KB → 64KB〜512KB に拡大（大きな JSONL エントリへの対応）
- `--no-tui` モードのスキャンループに `RefineToolUseState` を追加（ヘッドレスモードでの状態精緻化）

### Removed

- `realignClaudeWaitingStates` を削除（ペインテキスト優先の判定により不要に）

## [0.1.1] - 2026-03-23

### Added

- TUI から Claude Code の承認/拒否を実行できるようにした
- 設定ファイルから TUI のカラーパレットを変更できるようにした
- 承認操作まわりの E2E テスト基盤を追加した

### Fixed

- Claude Code のリスト選択 UI に対する承認/拒否キーシーケンスを修正した
- フラッシュメッセージの表示タイミングとエラークリアの挙動を安定化した
- WezTerm の `SendKeys` 変換と E2E 用 terminal mock の不整合を修正した

## [0.1.0] - 2026-03-22

### Added

- tmux 上の Claude Code / Codex / Gemini セッションを監視する `baton` の初回リリース
- 状態別グルーピング、プレビュー、ペインジャンプを備えた TUI ダッシュボード
- 承認待ち検出、workspace ベースのグルーピング、Codex / Gemini の状態検出
- GitHub Actions による CI と GitHub Releases 向けアセット生成

### Changed

- 既定のターミナルバックエンドを WezTerm から tmux 中心の構成へ整理した

[Unreleased]: https://github.com/yoshihiko555/baton/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/yoshihiko555/baton/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/yoshihiko555/baton/releases/tag/v0.1.0

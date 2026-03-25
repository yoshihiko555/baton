# Release Standardization Migration Plan

baton で先行導入したリリース運用を、他プロダクトへ安全に横展開するための移行計画。

## 背景

- baton では `main` 基準の release 運用、`CHANGELOG.md`、tag push 起点の GitHub Release 作成を導入した
- 今後は自分が管理する複数プロダクトでも、同じ release 体系を使いたい
- 共通資材は `yoshihiko555/.github` repo に集約する

## 目的

- release 運用の責務を「`.github` repo」「dotfiles」「各 repo」に分離する
- baton を reference implementation として扱い、他 repo へ順次展開できるようにする

## 非目的

- 全 repo の build / test / asset 作成 workflow を完全に共通化すること
- branch protection や label 設定をこの段階で自動同期すること
- 既存 repo の個別 release 履歴を一度に統一し直すこと

## baton で確定した標準

- `main` を唯一の release ブランチにする
- 日常開発は短命 branch を `main` から切り、PR 経由で squash merge する
- 並列作業は `gtr` による `git worktree` を前提にする
- `CHANGELOG.md` を人間向け変更履歴の一次ソースにする
- GitHub Release は tag push を契機に自動作成する
- assets は必要なプロダクトのみ release publish 後の workflow で添付する

## 共通化の責務分担

### 1. `.github` repo に置くもの

[yoshihiko555/.github](https://github.com/yoshihiko555/.github) で管理:

- reusable release workflow (`.github/workflows/release.yml`)
- release 実行のための共通 Taskfile (`taskfiles/release.yml`)
- GitHub Rulesets 用の JSON ファイル (`rulesets/`)
- Ruleset の生成・更新・インポート手順 (`docs/github-rulesets.md`)
- Git/release 運用方針 (`docs/git-release-policy.md`)
- 将来的な PR template / issue template

### 2. dotfiles に置くもの

- AI が PR を作る前提のグローバル `CLAUDE.md` / `AGENTS.md` 運用ルール

dotfiles は「AI エージェントのグローバル設定」を持つ。

### 3. 各 repo に残すもの

- `CHANGELOG.md`
- `.github/release.yml` (release note カテゴリ設定)
- release caller workflow (`.github/workflows/release.yml`)
- repo 固有の asset build workflow
- repo 固有の README / install / release ドキュメント導線

各 repo は「プロダクト固有の差分」だけを持つ。

### 各 repo の Taskfile 設定

各 repo の `Taskfile.yml` から `.github` repo の release タスクをローカル参照する:

```yaml
includes:
  rel:
    taskfile: ~/ghq/github.com/yoshihiko555/.github/taskfiles/release.yml
    flatten: true
```

前提: `ghq get yoshihiko555/.github`

## 配置方針

| 対象 | 配置先 | 理由 |
|------|--------|------|
| reusable release workflow | `.github` repo | 各 repo は caller だけ持てばよい |
| release タスク | `.github` repo | 各 repo から Taskfile ローカル参照 |
| branch / tag ruleset JSON | `.github` repo | GitHub 画面から import しやすい |
| ruleset 適用手順書 | `.github` repo | rulesets と同居 |
| Git/release 運用方針 | `.github` repo | workflow・タスク・rulesets と同居 |
| AI 向け共通運用ルール | `dotfiles` のグローバル `CLAUDE.md` / `AGENTS.md` | AI のグローバル設定として |
| release categories | 各 repo の `.github/release.yml` | repo ごとの label 方針を吸収 |
| `CHANGELOG.md` | 各 repo | 内容が repo 固有 |
| asset build workflow | 各 repo | build 対象と配布物が repo 固有 |

## 移行フェーズ

### Phase 1: baton を reference implementation として固定

状態: **完了**

成果物:

- `docs/release-process.md`
- `CHANGELOG.md`
- `.github/workflows/release.yml`
- `.github/release.yml`
- `.github/PULL_REQUEST_TEMPLATE.md`
- `Taskfile.yml` の `release*`

### Phase 2: dotfiles へローカル共通化を反映

状態: **完了**

成果物:

- `dotfiles/claude/.claude/CLAUDE.md` に Git/PR/changelog ルール追加
- `dotfiles/codex/.codex/AGENTS.md` に同上

### Phase 3: `.github` repo への共通資材集約

状態: **完了**

成果物:

- `yoshihiko555/.github` repo 作成
- `.github/workflows/release.yml` (reusable release workflow)
- `taskfiles/release.yml` (共通 release タスク)
- `rulesets/main-protection.json`, `rulesets/tag-protection.json`
- `docs/git-release-policy.md`, `docs/github-rulesets.md`
- baton の release workflow を caller に置き換え
- baton の Taskfile を `.github` repo のローカル参照に変更

### Phase 4: 各 repo の onboarding

状態: **未着手**

各 repo で行うこと:

1. `CHANGELOG.md` を追加
2. `.github/release.yml` を追加
3. release caller workflow を追加
4. `Taskfile.yml` に `.github` repo の release タスク参照を追加
5. `.github` repo の ruleset JSON を GitHub Settings から import する
6. asset workflow を repo 固有で調整
7. README に release 導線を追加
8. `main + 短命 branch + gtr` 運用へ切り替える

完了条件:

- 各 repo の release が `main` 基準で同じ手順になる
- 各 repo の GitHub ruleset が同じ基準でそろう

## repo 追加時のチェックリスト

- `main` が唯一の release ブランチになっている
- `main` 向け ruleset が import 済み
- `task release:status` が動く
- `task release VERSION=vX.Y.Z` が tag push まで担当する
- GitHub Release が tag push で自動作成される
- assets が必要な repo は publish 後に添付される
- `CHANGELOG.md` を `Unreleased` 運用できる
- PR template が有効になっており、release note / changelog 項目を確認できる

## リスク

### GitHub 共通資材の premature abstraction

- repo ごとの差分を吸収しきれない
- 共有 workflow の入力設計が過剰になる

対策:

- まず baton と 1 つ以上の追加 repo で実運用する
- その後に設計を見直す

### repo 固有 asset workflow の差分

- build 対象や配布形式が repo によって異なる

対策:

- asset build は各 repo に残す
- 共通化するのは release 作成の核だけに絞る

## 受け入れ基準

- baton の release 運用を他 repo に説明なしで移植できる
- `.github` repo の release task と workflow が baton と矛盾しない
- `.github` repo で ruleset JSON と import 手順が管理されている
- AI 向けグローバル指示に PR / changelog / squash merge ルールが含まれている
- 新しい repo へ導入するとき、repo 固有で個別設計が必要なのは主に `CHANGELOG` の内容、asset workflow、README などの利用者向け導線に絞られている

# Release Process

baton で採用するリリース運用の標準。個別の Go 実装に依存しないため、他プロダクトにもそのまま横展開できる。

移行計画と共通化方針は [release-standardization-plan.md](release-standardization-plan.md) を参照。

## 目的

- `CHANGELOG.md` を人間向けの一次ソースにする
- `main` を常にリリース可能な状態に保つ
- タグ作成から GitHub Release / assets 公開までを定型化する

## 標準ルール

| 項目 | 方針 |
|------|------|
| ベースブランチ | `main` を唯一のリリースブランチにする |
| 開発ブランチ | `feat/*` / `fix/*` / `chore/*` を `main` から切る |
| マージ方式 | 原則 PR 経由で `main` に squash merge |
| バージョン | Semantic Versioning (`vX.Y.Z`) を使う |
| 変更履歴 | `CHANGELOG.md` の `Unreleased` を更新する |
| GitHub Release | タグ push を契機に自動作成する |
| アセット配布 | プロジェクトごとに決める。必要な repo のみ Release 公開イベントで配布物を添付する |

## `develop` ブランチを標準にしない理由

- 個人開発や少人数開発では `develop -> main` の二段階マージが運用コストになりやすい
- リリース対象の差分が `main` ではなく `develop` に溜まり、何を出したのか追いにくい
- hotfix を `main` に直接入れたとき、`develop` への取り込み忘れが起きやすい
- GitHub Release とタグは通常 `main` 上のコミットを指すため、`main` を常時 deployable に保つ方が素直

## 日常の開発フロー

1. `main` から作業ブランチを切る
2. 実装後に PR を作る
3. PR でラベルを付ける (`feature` / `fix` / `docs` / `chore` など)
4. ユーザー向け変更がある場合は `CHANGELOG.md` の `Unreleased` を更新する
5. CI 通過後に `main` へ squash merge する

## worktree 運用

- 並列作業時は `git worktree` を前提にする
- worktree の作成・削除は `gtr` で管理する
- 原則として `1 worktree = 1 branch = 1 PR` に揃える
- ルート worktree は `main` の更新確認とリリース作業を中心に使う
- 日常の実装作業は `gtr` で作成した作業用 worktree 側で行う
- ブランチ名は厳密でなくてよい。識別できる短い名前を優先する
  - 例: `fix/typo`, `docs/readme`, `chore/release`, `fix/123`

## 競合解決

- `main` への統合は GitHub 上の PR マージで行う
- ローカルで `main` に merge して push する運用は採らない
- 競合が見つかった場合は、PR の head branch 側で `origin/main` を取り込んで解決する
- 競合解決は GitHub の conflict editor ではなく、原則ローカル worktree で行う
- 解決後に branch を push し直して、GitHub 上の PR を更新する

例:

```bash
git fetch origin
git switch fix/123
git merge origin/main
# 競合解決
git push
```

## リリースフロー

1. `CHANGELOG.md` の `Unreleased` を次バージョンへ確定する
2. `main` の CI が通っていることを確認する
3. `task release VERSION=vX.Y.Z` または `task release BUMP=patch|minor|major` を実行する
4. Task が `main` を fast-forward で同期し、タグを作成して push する
5. `.github/workflows/release.yml` が GitHub Release を作成する
6. `.github/workflows/release-assets.yml` がアセットをビルドして添付する

## GitHub Release の考え方

- GitHub Release は配布チャネル
- `CHANGELOG.md` は説明責任のある一次ソース
- `.github/release.yml` は自動生成ノートのカテゴリ分け設定

この 3 層を分けることで、GitHub 上で見やすく、かつリポジトリ外でも追える変更履歴になる。

配布物の有無はプロジェクトごとに異なる。

- baton のような CLI は GitHub Release にバイナリ assets を付ける
- npm package / Docker image / 社内ツールなどは別の配布チャネルを使ってよい
- assets が不要な repo は、GitHub Release 本文だけを運用してもよい

## GitHub 設定

各 repo で最低限そろえる推奨設定:

### Pull Requests

GitHub の `Settings` -> `General` -> `Pull Requests`

- `Allow squash merging`: ON
- `Allow merge commits`: OFF
- `Allow rebase merging`: OFF
- `Automatically delete head branches`: ON

この設定にすると、PR マージ時の選択肢が実質 `Squash and merge` に揃う。

### Branch Protection

GitHub の `Settings` -> `Branches` で `main` に対して設定する。

- Require a pull request before merging
- Require status checks to pass before merging
- Require linear history
- Do not allow force pushes

これにより、`main` への直接 merge commit push を避けやすくなる。

可能なら legacy の branch protection ではなく、GitHub Rulesets を基準に管理する。

- Ruleset の JSON は repo ごとに手で作り直さず、[`yoshihiko555/.github`](https://github.com/yoshihiko555/.github) で共通管理する
- baton は reference implementation とし、ruleset 実ファイルは `.github` repo に置く
- 新しい repo には `Settings` -> `Rules` -> `Rulesets` から JSON を import して適用する
- import 後に required checks と bypass actor だけ repo 固有事情に合わせて確認する

## 推奨ラベル

| ラベル | 用途 |
|--------|------|
| `breaking-change` | 破壊的変更 |
| `feature` | 新機能 |
| `enhancement` | 改善 |
| `fix` / `bug` | 不具合修正 |
| `docs` | ドキュメント変更 |
| `chore` | 雑務・運用変更 |
| `refactor` | 振る舞いを変えない内部整理 |
| `test` | テスト追加・修正 |
| `ci` | CI / workflow 更新 |
| `dependencies` | 依存更新 |
| `skip-changelog` | release note から除外したい変更 |

## 例外運用

### hotfix

- `main` から `fix/*` を切って最短で PR を作る
- マージ後に patch リリースを切る

### 長期検証が必要な変更

- 長く生きるブランチを使う前に、本当に `develop` が必要かを確認する
- 必要な場合でも、通常機能開発の標準フローは `main` ベースのまま維持する

## CHANGELOG 運用責務

- `CHANGELOG.md` の `Unreleased` は PR 作成者が更新する
- AI が PR を作る場合も、PR 作成時に AI が更新する
- reviewer / merge 担当者は、記載内容が妥当かだけ確認する
- release 時は release 担当者が `Unreleased` を version セクションへ確定する

このルールは repo ローカルのテンプレートだけでなく、`dotfiles` 管理のグローバル `CLAUDE.md` / `AGENTS.md` にも入れておく。

- `main` 直 push をしない
- PR 作成時に changelog 更新要否を判断する
- ユーザー向け変更なら `Unreleased` を更新する
- `main` への統合は GitHub 上の squash merge を使う

更新対象:

- ユーザー向け変更がある PR
- 利用者が認識すべき挙動変更を含む PR

更新しない対象:

- 純粋な内部整理
- テストのみ
- ユーザー影響のない CI / 補助スクリプト変更

更新不要の PR には `skip-changelog` ラベルを付けてよい

## 他プロダクトへ横展開する最小セット

この項目は「各 repo に最終的に残るもの」を指す。  
共通化方針の詳細は [release-standardization-plan.md](release-standardization-plan.md) を優先する。

### 各 repo に残す最小セット

- `CHANGELOG.md`
- `.github/release.yml`
- release caller workflow
- リポジトリ固有のアセット配布 workflow
- README / install / release ドキュメント導線

### `.github` repo へ寄せた対象

- `Taskfile` の `release*` タスク
- PR template
- release workflow 本体

これらは [`yoshihiko555/.github`](https://github.com/yoshihiko555/.github) に集約済み。

## baton での実装メモ

- `task release` は GitHub CLI に依存せず、タグ push までを担当する
- Release 本文は `CHANGELOG.md` から抽出する
- GitHub Release が既に存在する場合、workflow は二重作成しない

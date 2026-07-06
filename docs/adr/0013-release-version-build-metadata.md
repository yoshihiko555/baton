# ADR-0013: リリースバージョンをビルドメタデータから解決する

## Status

Accepted

## Date

2026-07-06

## Context

`baton --version` はソース内の固定値 `version` を表示していたため、リリースタグが `v0.1.2` に進んでも固定値の更新漏れにより `0.1.1` を表示する問題があった。

バージョン情報は以下の複数経路で必要になる。

- `go install github.com/yoshihiko555/baton@vX.Y.Z` で導入したバイナリ
- GitHub Releases で配布するビルド済みバイナリ
- ローカルで `task build` した確認用バイナリ
- 開発中に `go run . --version` した場合の表示

これらをすべて手動更新の固定値に依存させると、リリース時の作業漏れがそのままユーザー表示に出る。

## Decision

リリースバージョンの SSOT は Git tag / Go build metadata とし、ソース内の `version` 変数は開発用 fallback の `dev` にする。

具体的には以下の順で `--version` を解決する。

1. `-ldflags "-X main.version=..."` で渡された embedded version
   - GitHub Release assets workflow は release tag から version を渡す
   - `task build` は `git describe --tags --dirty --always --match "v*"` の結果を渡す
2. Go build info の module version
   - `go install ...@vX.Y.Z` では Go が module version を埋め込む
3. どちらも取得できない場合は `dev`

## Rationale

- **タグを SSOT にできる**: `go install @v...` と GitHub Release asset のどちらも tag に追従できる
- **固定値の更新漏れを避けられる**: ソース内 fallback は `dev` なので、古いリリース番号を誤表示しない
- **ローカルビルドでも検証しやすい**: `task build` で `git describe` 由来の `0.1.2-6-g...-dirty` のような識別子を埋め込める
- **通常の `go build` は単純なままにできる**: `ldflags` なしの開発ビルドはプロジェクト独自の version embedding を実行しないため、リリース確認用途では `task build` を使う前提にできる

比較した代替案:

- **リリースごとに `main.go` の固定値を更新する**: 実装は単純だが、今回のような更新漏れが再発しやすい
- **リリース workflow でソースを書き換えてコミットする**: 自動化はできるが、タグ作成後の追加コミットや main との整合性管理が複雑になる
- **CHANGELOG からバージョンを読む**: runtime 表示とドキュメント管理が結合し、配布バイナリの再現性が弱くなる

## Consequences

### Positive

- `baton --version` がリリースタグに追従しやすくなる
- `go install @v...` で導入したバイナリは、ソース内 fallback に依存せず正しいタグバージョンを表示できる
- ローカル確認用バイナリも `task build` を使えばコミット位置や dirty 状態を含めて識別できる

### Negative

- `go build -o baton .` で直接ビルドした場合は `task build` と同じ `git describe` 由来の表示にはならない
- バージョン付きローカルビルドには `task build` または明示的な `-ldflags` が必要になる

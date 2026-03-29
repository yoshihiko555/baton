# ADR-0011: ペインテキスト優先の Claude Code 状態判定

## Status

Accepted (supersedes ADR-0006 の Claude Waiting 判定部分)

## Date

2026-03-29

## Context

ADR-0006 で導入した ResolveMultiple 方式は、同一 CWD の複数セッションに対して JSONL から「状態分布」を取得し重要度順に割り当てる。しかし運用で以下の問題が顕在化した。

### 問題 1: JSONL → PID 対応不可による状態ミスマッチ

JSONL ファイルを特定の PID/ペインに対応付ける手段がない（lsof、プロセス引数、ロックファイルいずれも不可）。ResolveMultiple が重要度順にソートして割り当てるため、実際に Waiting のセッションに Thinking が、Idle のセッションに Waiting が割り当てられるケースが頻発した。

### 問題 2: RefineToolUseState のカバー不足

ペインテキストによる精緻化が `ToolUse` 状態の Claude セッションのみに適用されていた。JSONL が誤って `Thinking` や `Idle` を割り当てた場合、ペインテキストがチェックされず Waiting を見逃した。

### 問題 3: 正規表現パターンの脆弱性

全文マッチの正規表現でペインテキストを判定していたが、以下の問題が繰り返し発生した:

- スクロール履歴の `❯` プロンプトが現在の状態と競合
- 非ブレークスペース（`\u00a0`）が Go の `\s` にマッチしない
- `"allow"` の部分一致による誤検知
- Working 中でも `❯` + 区切り線が表示されるため Idle と誤判定

### 問題 4: ReadLastEntry のバッファ不足

`ReadLastEntry` が末尾 4KB のみ読み取っていたが、Claude Code の assistant エントリはツール呼び出し内容を含むため数万バイトになることがあり、パースに失敗してデフォルトの Thinking が返されていた。

### 問題 5: --no-tui モードでの RefineToolUseState 欠落

`runNoTUI` のスキャンループで `RefineToolUseState` が呼ばれておらず、ヘッドレスモードでは JSONL 状態がそのまま出力されていた。

## Decision

Claude Code の状態判定をペインテキスト優先の行単位逆順スキャンに変更する。

### `classifyClaudePane` 関数の導入

正規表現による全文マッチを廃止し、ペインテキストを末尾から逆順にスキャンして構造に基づいて判定する。

**判定アルゴリズム（優先順位順）**:

1. **Waiting**: テキスト全体に選択肢 UI（`❯ 1. Yes` + `2.`）または承認プロンプト（`Allow <tool>?` 等）
2. **入力プロンプト行の特定**: 末尾から逆順に `❯` を含む行 + 直前行が区切り線（`─` 4文字以上）を探す
3. **Working**: 入力プロンプト行より上 20 行以内に `✢`/`·`/`✶` プレフィックスまたは `Running…`
4. **Idle**: Working シグナルなし + 入力プロンプト行あり
5. **判定不能**: `(0, false)` を返し JSONL 状態を維持（Waiting の場合は ToolUse に降格）

### 全 Claude セッションでのペインテキストチェック

`RefineToolUseState` の Claude 分岐を `ToolUse` 状態のみから全状態（PaneID あり）に拡大。

### `realignClaudeWaitingStates` の削除

全セッションでペインテキストをチェックするため、複雑なスワップ処理は不要になった。

### `ReadLastEntry` のバッファ拡大

4KB → 64KB/256KB/512KB の段階的読み取りに変更。

## Rationale

- **ペインテキストは「今この瞬間」の状態を反映する唯一の確実なソース**: JSONL は別セッションの状態を含む可能性があるが、ペインテキストは tmux ペイン ID 経由で特定セッションのものが確実に取得できる
- **行単位スキャンは正規表現より堅牢**: スクロール履歴の影響を受けず、Claude Code の UI 構造（❯ + 区切り線 + ステータスバー）に基づいて判定する
- **Working 中でも `❯` + 区切り線が表示される**: Claude Code は Working 中もステータスバー領域を維持するため、`❯` の有無だけでは Idle と Working を区別できない。入力プロンプト行より上の出力エリアに Working シグナルがあるかで区別する

比較した代替案:

- **hook ベース監視（issue #4）**: 最も正確だが実装コストが高い。将来的に移行する方針は維持
- **JSONL パースのみ**: PID 対応不可のため複数セッションで破綻する
- **正規表現パターンの改善**: 何度も修正を重ねたが、エッジケースが多く安定しなかった

## Consequences

### Positive

- 複数セッション時の Waiting/Idle/Working 判定精度が大幅に向上
- 正規表現パターンの管理コストが削減（`claudeIdlePattern`、`claudeWorkingPattern`、`claudeChoicePattern` を削除）
- `realignClaudeWaitingStates` の複雑なスワップ処理が不要に
- ReadLastEntry の改善により大きな JSONL エントリでも正しく状態判定
- Codex/Gemini の既存ロジックに影響なし

### Negative

- `capture-pane` の呼び出し回数が増加（全 Claude セッション分）。2 秒ポーリング間隔内では許容範囲
- Claude Code の UI 構造（`❯` + 区切り線）に依存。UI 変更時にパターン更新が必要
- Working インジケーター（`✢`/`·`/`✶`/`Running…`）が網羅的でない可能性。新しいインジケーターが追加された場合は検出対象の追加が必要

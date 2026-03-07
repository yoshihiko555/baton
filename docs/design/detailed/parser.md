# 詳細設計: JSONL パーサー (`internal/core/parser.go`)

> 対象バージョン: baton v2
> 作成日: 2026-03-07

---

## 1. 概要

`parser.go` は Claude Code が書き出す JSONL ログファイルを解析し、セッション状態を判定するモジュールである。
v2 では JSONL の実際のスキーマに合わせた型拡張と、判定ロジックの簡略化を行う。

### 責務

- JSONL の1行を `Entry` 型にデシリアライズする（`ParseRecord`）
- エントリ列からセッション状態を判定する（`DetermineSessionState`）
- ファイルの追記分を増分読み取りする（`IncrementalReader`）

---

## 2. 型定義の変更

### 2.1 v1 → v2 の差分サマリ

| フィールド | v1 | v2 | 変更理由 |
|-----------|----|----|---------|
| `Entry.SubType` | なし | `string` | `system` エントリの `subtype` を参照するため |
| `Entry.SessionID` | なし | `string` | セッション識別に利用 |
| `Entry.GitBranch` | なし | `string` | ブランチ表示に利用 |
| `Entry.Data` | なし | `ProgressData` | `progress` エントリの詳細判定に利用 |
| `Entry.Role` | あり | **削除** | `Message.Role` に移動（JSONL 実構造に合わせる） |
| `Entry.CreatedAt` | あり | **削除** | v2 では参照しない |
| `Entry.Raw` | あり | **削除** | v2 では生バイト保持が不要 |
| `Entry.Message` | `*Message`（ポインタ） | `Message`（値型） | nil チェック排除（後述） |
| `Message.Role` | なし | `string` | トップレベルの `role` から移動 |
| `Message.StopReason` | なし | `string` | 状態判定のキー情報 |
| `ContentBlock.Name` | なし | `string` | `tool_use` のツール名取得 |

### 2.2 v2 型定義

```go
// Entry は JSONL ストリームの1レコードを表す。
type Entry struct {
    Type      string       `json:"type"`
    SubType   string       `json:"subtype,omitempty"`
    Message   Message      `json:"message,omitempty"`
    SessionID string       `json:"sessionId,omitempty"`
    GitBranch string       `json:"gitBranch,omitempty"`
    Timestamp string       `json:"timestamp,omitempty"`
    Data      ProgressData `json:"data,omitempty"`
}

// Message は JSONL レコード内の message フィールドを表す。
type Message struct {
    Role       string         `json:"role"`
    Content    []ContentBlock `json:"content"`
    StopReason string         `json:"stop_reason,omitempty"`
}

// ContentBlock は message.content[] の1要素を表す。
type ContentBlock struct {
    Type string `json:"type"`
    Name string `json:"name,omitempty"` // tool_use のツール名
}

// ProgressData は progress エントリの data フィールドを表す。
type ProgressData struct {
    Type string `json:"type"` // "hook_progress", "bash_progress", "agent_progress"
}
```

### 2.3 設計判断: Message をポインタから値型に変更

**v1 の問題点**: `entry.Message == nil` チェックが至る所に必要になり、コードが煩雑だった。

```go
// v1（煩雑）
if entry.Message == nil || len(entry.Message.Content) == 0 {
    return Idle
}
lastContent := entry.Message.Content[len(entry.Message.Content)-1]
```

**v2 の方針**: `Message` を値型にすることで、`entry.Message.Content` が常に安全に参照できる。
`message` キーが JSONL に存在しない場合は `Message{}` のゼロ値（`Content` が空スライス）になる。

```go
// v2（シンプル）
if len(entry.Message.Content) == 0 {
    return Idle
}
lastContent := entry.Message.Content[len(entry.Message.Content)-1]
```

**トレードオフ**:
- `message` フィールドが存在しないエントリ（`system` 等）でも `Message` のゼロ値が入るが、実害はない
- JSON の `omitempty` は値型の場合はゼロ値を省略するため、シリアライズ方向も問題なし

---

## 3. `DetermineSessionState` の変更

### 3.1 v1 の問題点

v1 は末尾から `assistant` エントリをループ検索していた。

```go
for i := len(entries) - 1; i >= 0; i-- {
    if entry.Type == "user" { return Thinking }
    if entry.Type != "assistant" { continue }
    // ... content で判定
}
```

**問題**:
- `progress` エントリは `continue` でスキップされるため、ツール実行中の状態を正確に反映できない
- `Waiting` 状態（ツール結果待ち）が存在せず、`ToolUse` との区別ができない
- ループコストが小さいとはいえ、最終エントリだけで判定できる情報を全件走査していた

### 3.2 v2 の判定方式

**最終エントリ1件のみで判定する**。

Claude Code の JSONL は時系列に追記されるため、最終エントリがセッションの現在状態を最も正確に表す。
ループ検索は不要であり、最終エントリ1件で十分な情報が得られる。

```go
func DetermineSessionState(entries []*Entry) SessionState {
    if len(entries) == 0 {
        return Idle
    }
    last := entries[len(entries)-1]
    if last == nil {
        return Idle
    }
    return classifyEntry(last)
}
```

### 3.3 判定ロジック（優先順位順）

| 優先度 | 条件 | 判定結果 | 備考 |
|-------|------|---------|------|
| 1 | `type == "system" && subtype == "turn_duration"` | `Idle` | ターン終了の明示シグナル |
| 2 | `type == "assistant" && stop_reason == "end_turn"` | `Idle` | 正常完了 |
| 3 | `type == "assistant" && stop_reason == "tool_use"` | `Waiting` | ツール実行をリクエストした直後（NEW） |
| 4 | `type == "progress"` | `ToolUse` | ツール実行中（NEW） |
| 5 | `type == "user" && content[0].type == "tool_result"` | `Thinking` | ツール結果を受け取り再推論中 |
| 6 | `type == "user"` | `Thinking` | ユーザー入力後、AI 推論待ち |
| 7 | `type == "assistant" && content[-1].type == "thinking"` | `Thinking` | 拡張思考中 |
| 8 | `type == "assistant" && content[-1].type == "error"` | `Error` | エラー発生 |
| 9 | fallback | `Idle` | 判定不能な場合 |

### 3.4 `Waiting` 状態の追加

v2 で `Waiting` を新たに追加する。

**定義**: `assistant` が `stop_reason: "tool_use"` でターンを終えた直後（ツール実行リクエスト済み、ツール実行完了前）。

```
assistant (stop_reason="tool_use")  ← Waiting
    ↓
progress エントリ群                  ← ToolUse
    ↓
user (content[0].type="tool_result") ← Thinking
```

`Waiting` と `ToolUse` は TUI 上では異なるアイコン・色で表示できる。

```go
const (
    Idle     SessionState = iota
    Thinking
    ToolUse
    Waiting  // NEW: ツール実行リクエスト済み、実行待ち
    Error
)
```

### 3.5 `progress` エントリを判定対象に含める理由

v1 では `progress` エントリをスキップしていたため、長時間のツール実行中に状態が `Thinking` のまま更新されなかった。

v2 では `progress` エントリを `ToolUse` の直接シグナルとして扱う。
`ProgressData.Type` の値（`hook_progress` / `bash_progress` / `agent_progress`）は将来の詳細表示に利用できる。

### 3.6 `stop_reason` を Message に持つ理由

実際の Claude Code JSONL の構造:

```json
{
  "type": "assistant",
  "message": {
    "role": "assistant",
    "content": [...],
    "stop_reason": "tool_use"
  }
}
```

v1 では `stop_reason` をトップレベルに置いていたが、実際の JSONL では `message` の中にある。
v2 では構造体を JSONL の実構造に合わせることで、デシリアライズの正確性を向上させる。

---

## 4. `IncrementalReader` の変更

### 4.1 基本構造の維持

以下の v1 機能は v2 でも維持する。

- `offsets map[string]int64` による複数ファイルの offset 管理
- ファイルサイズが offset より小さい場合のローテーション検出
- 不完全行（末尾改行なし）の次回再読み処理

### 4.2 `ReadLastEntry` メソッドの追加

**目的**: v2 では状態判定に必要なのは最終エントリ1件のみである。
全追記分を読み込む `ReadNew` は最終エントリ取得には非効率なケースがある。

```go
// ReadLastEntry は filepath の末尾から最終エントリ1件を返す。
// ファイル末尾から逆順に読み取るため、大量追記がある場合に効率的。
func (r *IncrementalReader) ReadLastEntry(filepath string) (*Entry, error)
```

**実装方針**:
1. ファイルをシークして末尾付近を読む（バッファサイズ: 4096 バイト程度）
2. 最後の完全な JSONL 行を探してパースする
3. 取得できなければ `nil, nil` を返す（エントリなし）

**注意**: `ReadLastEntry` は offset を更新しない（状態判定専用）。

### 4.3 `ReadNew` の維持理由

`gitBranch` や現在のツール名（`ContentBlock.Name`）は最終エントリ以外に存在する可能性がある。
複数エントリを走査して情報を収集する用途では `ReadNew` が引き続き必要。

### 4.4 メソッド一覧（v2）

| メソッド | 引数 | 戻り値 | 用途 |
|---------|------|-------|------|
| `ReadNew(filepath)` | `string` | `([]*Entry, error)` | 追記分全件取得（gitBranch 等の付加情報収集） |
| `ReadLastEntry(filepath)` | `string` | `(*Entry, error)` | 状態判定用の最終エントリ取得 |
| `Reset(filepath)` | `string` | `void` | offset リセット（ローテーション対応） |

---

## 5. `ParseRecord` の変更

v1 から以下を変更する。

- `entry.Raw` への生バイトコピーを削除（v2 では不要）
- 戻り値は引き続き `(*Entry, error)`

```go
// v2 の ParseRecord（シンプル化）
func ParseRecord(line []byte) (*Entry, error) {
    record := bytes.TrimSpace(line)
    if len(record) == 0 {
        return nil, errors.New("empty record")
    }
    entry := &Entry{}
    if err := json.Unmarshal(record, entry); err != nil {
        return nil, err
    }
    return entry, nil
}
```

---

## 6. 設計判断まとめ

| 判断 | 採用した方針 | 却下した代替案 | 理由 |
|-----|------------|--------------|------|
| 状態判定の方式 | 最終エントリ1件で判定 | 末尾からループ検索（v1） | 最終エントリが現在状態を最も正確に表す。ループは不要な複雑さ |
| Message の型 | 値型 | ポインタ型（v1） | nil チェック排除。JSONL に `message` がなければゼロ値で安全 |
| progress エントリ | 判定対象に含める | スキップする（v1） | `ToolUse` 状態の正確な反映に必須 |
| stop_reason の位置 | `Message` 内 | トップレベル（v1） | JSONL の実際の構造に合わせる |
| `ReadLastEntry` | 追加（任意利用） | `ReadNew` のみ | 状態判定専用の軽量パスを提供。`ReadNew` との共存で柔軟性を維持 |
| `Waiting` 状態 | 追加 | `ToolUse` に統合 | ツール実行リクエスト済みと実行中を区別することで TUI 表現が豊かになる |

---

## 7. テスト方針

| テストケース | 検証内容 |
|------------|---------|
| `ParseRecord` 正常系 | 各フィールドが正しくデシリアライズされること |
| `ParseRecord` 異常系 | 空行・不正 JSON でエラーが返ること |
| `DetermineSessionState` 各ルール | 優先順位 1-9 の全ケースで期待する状態が返ること |
| `DetermineSessionState` 空 | `entries` が空のとき `Idle` を返すこと |
| `ReadNew` 追記検出 | offset 以降の行のみが返ること |
| `ReadNew` ローテーション | ファイルが縮小したとき先頭から再読されること |
| `ReadLastEntry` 正常系 | ファイル末尾の最終エントリが返ること |
| `ReadLastEntry` 空ファイル | `nil, nil` が返ること |

テストフィクスチャは `t.TempDir()` で作成し、実ファイルへの依存を排除する。

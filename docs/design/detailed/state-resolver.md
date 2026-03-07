# StateResolver 詳細設計

## 概要

StateResolver は baton v2 の新規コンポーネントであり、以下の責務を担う。

- Claude Code セッションの CWD から対応 JSONL ファイルを解決する
- JSONL の末尾エントリを解析し、詳細状態（Idle / Thinking / ToolUse / Waiting / Error）を判定する
- gitBranch・currentTool・session-meta などの補助情報を取得する

**スコープ外**: Codex / Gemini プロセスはプロセス存在のみで `Thinking` に固定されるため、StateResolver は関与しない。

---

## 責務

1. `DetectedProcess` の CWD をスラッグ化し、対応する JSONL ディレクトリを特定する
2. ディレクトリ内の .jsonl ファイルを mtime でフィルタリングし、アクティブ候補を絞り込む
3. JSONL の末尾エントリのパターンから状態を判定する
4. gitBranch・currentTool を JSONL エントリから抽出する
5. JSONL ファイル名（UUID）をキーに session-meta を取得する
6. 同一 CWD に複数の Claude プロセスが存在する場合に Ambiguous フラグを設定する

---

## インターフェース

### 入力

| フィールド | 型 | 説明 |
|-----------|-----|------|
| cwd | string | 対象プロセスの作業ディレクトリ |
| claudeProcesses | []DetectedProcess | 同一 CWD に紐づく Claude プロセス一覧 |

### 出力（プロセスごと）

| フィールド | 型 | 説明 |
|-----------|-----|------|
| status | Status | Idle / Thinking / ToolUse / Waiting / Error |
| gitBranch | string | 現在の git ブランチ名（取得失敗時は空文字） |
| currentTool | string | 実行中ツール名（取得失敗時は空文字） |
| sessionMeta | SessionMeta | firstPrompt, inputTokens, outputTokens |
| ambiguous | bool | 同一 CWD に複数プロセスが存在するか |
| candidatePaneIDs | []int | Ambiguous=true のときの候補ペイン ID 一覧 |

---

## CWD → JSONL パス解決

### スラッグ化ルール

CWD のすべての `/` を `-` に置換する。先頭の `/` も `-` に変換される。

```
"/Users/foo/bar"     → "-Users-foo-bar"
"/home/user"         → "-home-user"
"/Users/foo/bar/"    → "-Users-foo-bar-"   （末尾スラッシュはそのまま "-" になる）
```

**実装**: `strings.ReplaceAll(cwd, "/", "-")`（Go 標準ライブラリのみ、依存なし）

**Edge case**:

| 入力 | スラッグ | 備考 |
|------|---------|------|
| `"/"` | `"-"` | ルートディレクトリ |
| `"/Users/foo/my project"` | `"-Users-foo-my project"` | スペースはそのまま保持 |
| `""` | `""` | 空文字 → Thinking にフォールバック |

JSONL ディレクトリパス: `{claudeProjectsDir}/{slug}/`

---

### アクティブ JSONL の絞り込み

1. `{claudeProjectsDir}/{slug}/` 内の `.jsonl` ファイルを列挙する
2. `mtime >= now - scanInterval×2` のファイルを「アクティブ候補」とする（例: scanInterval=3s → 閾値 6s）
3. 候補数とプロセス数を照合し、対応を決定する

| 候補数 N_f | プロセス数 N_p | 処理 |
|-----------|--------------|------|
| 0 | 任意 | 全プロセスを Thinking にフォールバック |
| N_f == N_p | — | mtime 降順でソートし 1:1 対応と推定 |
| N_f > N_p | — | mtime 降順の上位 N_p 本を使用 |
| N_f < N_p | — | 対応が取れないプロセスを Thinking にフォールバック |

---

### 設計判断 1: mtime ベース判定を選択した理由（lsof 不可のため）

`lsof` はプロセスが開いているファイルを特定できるが、以下の理由で採用しない。

- macOS のサンドボックス環境・制限環境では `lsof` に特別な権限が必要であり、信頼性が低い
- `lsof` は外部プロセス実行を伴うため、スキャン周期内での安定実行が困難
- JSONL への書き込みは Claude Code のアクティブな処理に伴って行われるため、mtime は実際のセッション活動と十分に相関する
- `os.Stat` のみで完結し、外部依存ゼロで実装できる

---

### 設計判断 2: scanInterval×2 を閾値とする根拠

- `scanInterval` はポーリング周期（例: 3s）
- ×2 により 1 サイクル分のマージンを確保する

| 倍率 | リスク |
|------|-------|
| ×1 | スキャンのタイミングによって書き込み直後のファイルを見逃す可能性がある |
| ×2 | 1 サイクル分の猶予。鮮度と誤検知率のバランスが最適 |
| ×3 以上 | 終了直後のセッションのファイルを誤ってアクティブと判定するリスクが高まる |

---

### 設計判断 3: Waiting 中の JSONL mtime 停滞問題の許容判断

**問題**: Waiting 状態（tool_use 承認待ち）では Claude Code が JSONL への書き込みを停止するため、mtime が停滞してアクティブ候補から外れる場合がある。

**判断**: この制約を許容する。

- Waiting は短命な状態（ユーザーが承認すれば直ちに解消される）
- 候補落ちした場合のフォールバック先は Thinking であり、安全（プロセスは生存している）
- 「最後に確認した状態のキャッシュ」を持つ方式も検討したが、ステートレス設計の複雑化に見合わないと判断
- ×2 の猶予により、短時間の Waiting では候補落ちしないケースが多い

---

## 状態判定ルール

### 最終エントリ パターンマッチング

JSONL の末尾エントリを読み取り、以下のパターンで状態を決定する。

| パターン | 判定 |
|---------|------|
| `type:"system"` かつ `subtype:"turn_duration"` | **Idle** |
| `type:"assistant"` かつ `stop_reason:"end_turn"` | **Idle** |
| `type:"assistant"` かつ `stop_reason:"tool_use"` | **Waiting** |
| `type:"progress"` | **ToolUse** |
| `type:"user"` かつ `content[0].type:"tool_result"` | **Thinking** |
| `type:"user"`（その他） | **Thinking** |
| `type:"assistant"` かつ `content[-1].type:"thinking"` | **Thinking** |
| `type:"assistant"` かつ `content[-1].type:"error"` | **Error** |
| 上記いずれにも該当しない | **Idle**（フォールバック） |

---

### Waiting vs ToolUse の区別ロジック

```
assistant (stop_reason: "tool_use")
    → Waiting（ユーザー承認待ち）
      ↓ [ユーザーが承認]
progress (hook_progress)
    → ToolUse（承認済み・フック実行中）
progress (bash_progress / agent_progress)
    → ToolUse（コマンド / サブエージェント実行中）
      ↓ [実行完了]
user (content[0].type: "tool_result")
    → Thinking（結果を処理中）
```

`hook_progress` エントリの存在が「承認済み」シグナルとなる。`hook_progress` が現れる前は承認待ちの Waiting、現れた後は実行中の ToolUse と解釈する。

---

### 設計判断 4: フォールバック先を Idle ではなく Thinking にする理由

JSONL が解決できない・パースできない場合のフォールバック先として Thinking を選択する。

- `DetectedProcess` が存在する時点でプロセスは生存している
- Idle は「何もしていない」を意味するため、生存中のプロセスに対して誤解を招く
- Thinking は「アクティブだが状態不明」を伝えており、実態に近い
- ユーザーへの情報として「動いているが詳細不明」の方が「止まっている」より正確

---

## 補助情報の取得

### gitBranch

- **ソース**: JSONL エントリのトップレベル `gitBranch` フィールド
- **取得方法**: 末尾から遡り、`gitBranch` が設定されている最初のエントリの値を使用する
- **フォールバック**: 空文字（TUI / WezTerm では `-` として表示）

### currentTool

- **ソース**: JSONL 内の最後の `tool_use` content block の `name` フィールド
- **取得方法**: 末尾エントリの `content` 配列を後方から走査し、`type:"tool_use"` の `name` を取得する
- **フォールバック**: 空文字（TUI / WezTerm では `-` として表示）

### session-meta

- **パス**: `{sessionMetaDir}/{UUID}.json`
- **UUID**: JSONL ファイル名から `.jsonl` 拡張子を除いた部分
- **取得フィールド**:

| フィールド | 型 | 説明 |
|-----------|-----|------|
| first_prompt | string | セッション最初のプロンプト |
| input_tokens | int | 累積入力トークン数 |
| output_tokens | int | 累積出力トークン数 |

- **フォールバック**: 読み取り失敗時は全フィールドをゼロ値・空文字のまま。Exporter の `omitempty` により JSON 出力から省略される

---

## Ambiguous フラグ

| 条件 | Ambiguous | CandidatePaneIDs |
|------|-----------|-----------------|
| 同一 CWD に Claude プロセスが 2 以上 | `true` | 全プロセスのペイン ID |
| 同一 CWD に Claude プロセスが 1 | `false` | `[]`（空） |

TUI および WezTerm Lua プラグインは `Ambiguous=true` の場合に警告表示を行い、ユーザーにマッピングが不確実であることを伝える。

---

## エラー処理方針

| エラー状況 | 対応 |
|-----------|------|
| JSONL ディレクトリが存在しない | 全プロセスを Thinking にフォールバック |
| JSONL ファイルの読み取り失敗 | 当該セッションを Thinking にフォールバック |
| JSONL パースエラー（途中エントリ） | 直前の有効エントリで判定を継続 |
| session-meta 読み取り失敗 | 補助情報をゼロ値・空文字のまま（Exporter で省略） |
| スラッグ化後のパスが空文字 | Thinking にフォールバック |

エラーは呼び出し元に `error` として返す。呼び出し元（StateManager）がフォールバック（Thinking）を適用する責務を持つ。ログ出力は StateManager 側で行う（`log.Fatal` は使用しない）。

---

## 依存コンポーネント

| コンポーネント | 役割 |
|-------------|------|
| `Parser (IncrementalReader)` | JSONL の末尾エントリを効率的に読み取る |
| ファイルシステム（`os.Stat`, `os.ReadDir`） | JSONL / session-meta の mtime 取得・列挙・読み取り |
| `Config` | `claudeProjectsDir`, `sessionMetaDir`, `scanInterval` を参照 |

---

## 実装上の注意

- **ステートレス設計**: StateResolver は状態キャッシュを持たない。呼び出しのたびにファイルシステムから読み取る
- **IncrementalReader の管理**: Reader のライフサイクルは呼び出し元（Watcher / Orchestrator）が管理し、StateResolver は Reader を受け取る形にする
- **mtime 取得**: `os.Stat(path).ModTime()` を使用。syscall 直接呼び出し不要
- **スラッグ化**: `strings.ReplaceAll(cwd, "/", "-")` で実装。外部ライブラリ不要
- **エラーは握り潰さない**: フォールバック後もエラー内容をデバッグログに出力する

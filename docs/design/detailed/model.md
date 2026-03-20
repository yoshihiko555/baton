# model.go 詳細設計 — v1 → v2 変更仕様

**対象ファイル**: `internal/core/model.go`
**作成日**: 2026-03-07
**ステータス**: Draft

---

## 概要

v2 では、ファイル監視（fsnotify）ベースのアーキテクチャから、プロセス検出（ps/procfs）ベースのアーキテクチャへ移行する。
これに伴い、`model.go` のドメイン型を以下の方針で再設計する。

- **セッション識別子**: ファイルパス/文字列 ID → PID（整数）に変更
- **ペイン識別子**: 文字列 → 整数（WezTerm CLI の出力型に準拠）
- **ツール情報**: 型なし文字列 → `ToolType` 列挙型で型安全に管理
- **ファイル監視型**: `WatchEvent` 系の型を削除し、`ScanResult` に置き換え
- **出力 DTO**: 内部型と外部表現を明確に分離

---

## 1. 列挙型

### 1.1 ToolType（新規）

```
ToolClaude  = 0
ToolCodex   = 1
ToolGemini  = 2
ToolUnknown = 3
```

**用途**: プロセス検出時にどの AI ツールが起動しているかを型安全に表現する。

**String() メソッド**:

| 値 | 戻り値 |
|----|--------|
| ToolClaude | `"claude"` |
| ToolCodex | `"codex"` |
| ToolGemini | `"gemini"` |
| ToolUnknown | `"unknown"` |

**設計判断**: `int` 列挙型を採用する理由は以下の通り。

- プロセスリストのフィルタリング・ソートで比較演算が頻繁に発生するため、`int` 比較の方が `string` 比較より効率的
- `MarshalJSON` は持たない。外部出力は後述の DTO 層で `String()` を呼び出して変換する
- 新しいツールの追加が容易（定数を追加するだけ）

---

### 1.2 SessionState（拡張）

v1 の 4 値から、v2 では **5 値** に拡張する。

```
Idle     = 0  // 作業していない状態（v1 から継続）
Thinking = 1  // 推論中（v1 から継続）
ToolUse  = 2  // ツール実行中（v1 から継続）
Waiting  = 3  // 新規追加
Error    = 4  // エラー状態（v1 から継続、値が変わることに注意）
```

**Waiting 状態の意味**:
ツール承認待ち状態を表す。JSONL の最終エントリが `type: "assistant"` かつ `stop_reason: "tool_use"` で、後続に `progress` エントリがない場合に判定される。

**判定ロジック**（実機検証済み）:
- `assistant(stop_reason: "tool_use")` → **Waiting**（ユーザーの承認がまだ行われていない）
- 後続に `progress(hook_progress)` が出現 → **ToolUse**（承認済み、ツール実行中）
- 特定のツール名（`AskUserQuestion` 等）には依存しない。`hook_progress` の有無が承認シグナル

**設計判断**:

- v1 では Waiting 状態が存在せず、ツール承認待ちを検出できなかった
  - `Idle`: ターン完了、ユーザーの次の入力待ち
  - `Waiting`: ツール実行の承認待ち（ユーザーの即時対応が必要）
- `Summary.Waiting` での集計に使用するため独立した状態値が必要

**String() / MarshalJSON() の方針**:
v1 では `SessionState` が `MarshalJSON()` を持っていたが、v2 では削除する。外部出力は DTO 経由で行うため、内部型に JSON 変換ロジックを持たせる必要がない。`String()` のみ保持する（ログ・デバッグ用途）。

---

## 2. コアドメイン型

### 2.1 Session（変更）

#### v1 → v2 フィールド対応表

| v1 フィールド | v2 フィールド | 変更内容 |
|--------------|--------------|---------|
| `ID string` | （削除） | PID がセッション識別子になるため不要 |
| `ProjectPath string` | （削除） | Project に属する情報のため Session が持つ必要がない |
| `FilePath string` | （削除） | ファイル監視アーキテクチャの廃止に伴い削除 |
| `PaneID string` | `PaneID string` | 型維持（v3 で int → string に戻した。ADR-0008 参照） |
| `State SessionState` | `State SessionState` | 型は同じ、値域が拡張 |
| `LastActivity time.Time` | `LastActivity time.Time` | 内部フィールド（JSON 出力対象外）に変更 |
| （なし） | `PID int` | 必須フィールドとして新規追加 |
| （なし） | `Tool ToolType` | 必須フィールドとして新規追加 |
| （なし） | `WorkingDir string` | 必須フィールドとして新規追加 |
| （なし） | `Branch string` | 任意フィールド（`omitempty`） |
| （なし） | `CurrentTool string` | 任意フィールド（`omitempty`） |
| （なし） | `FirstPrompt string` | 任意フィールド（`omitempty`） |
| （なし） | `InputTokens int` | 任意フィールド（`omitempty`） |
| （なし） | `OutputTokens int` | 任意フィールド（`omitempty`） |
| （なし） | `Ambiguous bool` | 内部フィールド（JSON 出力対象外） |
| （なし） | `CandidatePaneIDs []string` | 内部フィールド（JSON 出力対象外） |

#### Session.ID 削除の理由

v1 では Claude Code が生成するセッション ID（JSONL ファイル名に含まれる UUID 様の文字列）を `ID` として使用していた。
v2 ではプロセス検出ベースに移行するため、OS のプロセス ID（PID）がセッションの自然な識別子となる。
PID は OS が保証するユニーク性を持ち、セッション追跡・更新・削除の全操作で一貫して使用できる。

#### PaneID の型変遷: string (v1) → int (v2) → string (v3)

v2 では WezTerm CLI が数値で pane_id を返すため `int` に変更したが、v3 で tmux 対応を行った際に tmux の pane_id が `%5` 形式の string であるため `string` に戻した（ADR-0008 参照）。

- tmux の `%N` 形式を自然に表現できる
- WezTerm 側は `strconv.Itoa` で string 化する（変換コストは無視できるレベル）
- マップキーとしての利用が容易

#### Sessions を `[]*Session` → `[]Session` に変更する理由

v1 ではポインタのスライスを使用していたが、v2 では値のスライスに変更する。

- `Session` の更新操作は `StateUpdater.UpdateFromScan()` の1箇所に集約される。複数箇所からポインタ経由で変更するパターンがなくなるため、ポインタの利点が薄い
- 値セマンティクスにより、TUI レンダリング時のスナップショットコピーが安全に行える
- コピーコストは `Session` のサイズ（概算 ~100 バイト）で許容範囲

#### Ambiguous フラグの設計意図

プロセス検出でペインと作業ディレクトリの対応が一意に決まらない場合（例: 同一 CWD で複数の WezTerm ペインが開いている）に `true` を設定する。

- TUI: 「?」マーカーを表示してユーザーに通知
- DTO 出力: `Ambiguous: true` の場合は `pane_id` を省略（`omitempty`）
- `CandidatePaneIDs`: デバッグ・ログ用に候補 ID を保持するが、外部には公開しない

---

### 2.2 Project（変更）

| v1 フィールド | v2 フィールド | 変更内容 |
|--------------|--------------|---------|
| `Path string` | `Path string` | 変更なし |
| `DisplayName string` | `Name string` | フィールド名を短縮 |
| （なし） | `Workspace string` | ワークスペース名を新規追加（グルーピングキー） |
| `Sessions []*Session` | `Sessions []Session` | ポインタ除去（2.1 参照） |
| `ActiveCount int` | （削除） | `Summary` 型で集計するため不要 |

`ActiveCount` の削除理由: v2 で `Summary` 型が導入され、`Active`・`Waiting` などの集計値を一元管理する。`Project` が個別に `ActiveCount` を持つと集計ロジックの重複が発生する。

#### Workspace フィールドの設計意図

WezTerm のワークスペース駆動運用（プロジェクト別ワークスペース）に対応するため、プロジェクトのグルーピングキーおよび表示名として使用する。

**グルーピングルール:**

| 条件 | グルーピングキー | `Name` の値 |
|------|---------------|-------------|
| `Workspace` が空でなく `"default"` でもない | `Workspace` | ワークスペース名 |
| `Workspace` が空 or `"default"` | `CWD`（既存の動作） | CWD のベース名 |

**`"default"` をフォールバックにする理由:** WezTerm は初期状態で全 pane を `"default"` ワークスペースに属させる。ワークスペースを使わないユーザーへの影響を排除するため、`"default"` は CWD ベースのグルーピングに戻す。

---

### 2.3 DetectedProcess（新規）

プロセス検出結果の1プロセスを表すデータ転送オブジェクト。

| フィールド | 型 | 説明 |
|-----------|-----|------|
| `PID` | `int` | OS プロセス ID |
| `Name` | `string` | プロセス名（例: `claude`, `codex`） |
| `ToolType` | `ToolType` | 列挙型で分類されたツール種別 |
| `PaneID` | `string` | 関連ペイン ID（tmux: "%5", wezterm: "42"。空文字 = 未関連） |
| `TTY` | `string` | 端末デバイスパス（例: `/dev/ttys001`） |
| `CWD` | `string` | プロセスの作業ディレクトリ（正規化済み） |

**CWD 正規化**: `file://` プレフィックスは `ListPanes()` 内で除去する（WezTerm が `file:///Users/...` 形式で返す場合がある）。`DetectedProcess` に格納される時点では純粋なファイルシステムパスである。

---

### 2.4 ScanResult（新規）

`Scanner.Scan()` の戻り値。ファイル監視の `WatchEvent` に代わる、プロセス検出の単一スナップショット。

| フィールド | 型 | 説明 |
|-----------|-----|------|
| `Processes` | `[]DetectedProcess` | 検出されたプロセス一覧 |
| `Panes` | `[]Pane` | Terminal.ListPanes() から取得したペイン一覧（Ambiguous 解決用） |
| `Timestamp` | `time.Time` | スキャン実行時刻 |
| `Err` | `error` | スキャンエラー（`nil` = 正常） |

`Err` をフィールドとして持つ理由: `Scanner.Scan()` は `(ScanResult, error)` ではなく `ScanResult` のみを返すシグネチャとする。これによりエラー時も `Timestamp` を保持でき、呼び出し元でのパターンマッチが単純化される。

---

### 2.5 Summary（新規）

集約済み状態のサマリーを表す型。

| フィールド | 型 | 説明 |
|-----------|-----|------|
| `TotalSessions` | `int` | 検出された全セッション数 |
| `Active` | `int` | Thinking + ToolUse + Waiting 状態のセッション数 |
| `Waiting` | `int` | Waiting 状態のセッション数 |
| `ByTool` | `map[string]int` | ツール種別ごとのセッション数 |

`Summary` 導入理由:

- v1 の `Project.ActiveCount` は「アクティブ」のみ集計していたが、v2 では `Waiting` も重要な状態のため独立した集計が必要
- WezTerm ステータスバーには全体のサマリーを表示する。プロジェクト単位ではなくグローバルな集計値が必要
- `StateReader.Summary()` として公開することで、TUI と WezTerm プラグインが同一インターフェースから取得できる

---

## 3. 削除される型

以下の型はファイル監視アーキテクチャの廃止に伴い削除する。

| 型 | 削除理由 |
|----|---------|
| `WatchEventType` | fsnotify 連携が不要になるため |
| `WatchEvent` | 同上 |
| `EventSource` | `Events() <-chan WatchEvent` チャネルパターンを廃止 |
| `StatusOutput`（内部型） | Exporter 用 DTO に置き換え（後述） |

---

## 4. インターフェース変更

### 4.1 StateReader（変更）

| v1 メソッド | v2 メソッド | 変更内容 |
|------------|------------|---------|
| `GetProjects() []Project` | `Projects() []Project` | 名前を Go 慣習に合わせ短縮 |
| `GetStatus() StatusOutput` | `Summary() Summary` | 戻り値型を `Summary` に変更 |
| （なし） | `Panes() []Pane` | 新規追加（TUI でのペイン情報表示用） |

メソッド名の変更理由: Go では getter に `Get` プレフィックスを付けない慣習（Effective Go 参照）。v1 では慣習に反していたため修正する。

### 4.2 StateWriter → StateUpdater（置き換え）

v1 の `StateWriter` インターフェースは削除し、`StateUpdater` に置き換える。

| v1 | v2 |
|----|-----|
| `HandleEvent(event WatchEvent) error` | `UpdateFromScan(result ScanResult) error` |

変更理由: イベント駆動（差分更新）からスキャン駆動（全量更新）へアーキテクチャが変わるため、インターフェースのシマンティクスも変わる。`UpdateFromScan` は `ScanResult` を受け取り、内部状態を完全に置き換える。

### 4.3 Scanner（新規）

```
type Scanner interface {
    Scan(ctx context.Context) ScanResult
}
```

`context.Context` を受け取ることでタイムアウト・キャンセルに対応する。

---

## 5. Exporter 用 DTO（新規）

内部ドメイン型と外部 JSON 表現を分離するため、Exporter 専用の DTO 群を定義する（`internal/core/model.go` または `internal/exporter/dto.go` に配置）。

### 5.1 StatusOutput（DTO として再定義）

| フィールド | 型 | JSON キー | 説明 |
|-----------|-----|-----------|------|
| `Version` | `int` | `version` | スキーマバージョン（v2 = 2） |
| `Timestamp` | `string` | `timestamp` | RFC3339 形式 |
| `Projects` | `[]ProjectOutput` | `projects` | プロジェクト一覧 |
| `Summary` | `SummaryOutput` | `summary` | 集計情報 |
| `FormattedStatus` | `string` | `formatted_status` | WezTerm 表示用フォーマット済み文字列 |

### 5.2 ProjectOutput

| フィールド | 型 | JSON キー |
|-----------|-----|-----------|
| `Path` | `string` | `path` |
| `Name` | `string` | `name` |
| `Sessions` | `[]SessionOutput` | `sessions` |

### 5.3 SessionOutput

| フィールド | 型 | JSON キー | 備考 |
|-----------|-----|-----------|------|
| `PID` | `int` | `pid` | 必須 |
| `Tool` | `string` | `tool` | `ToolType.String()` で変換 |
| `State` | `string` | `state` | `SessionState.String()` で変換 |
| `PaneID` | `string` | `pane_id,omitempty` | `strconv.Itoa` で変換。Ambiguous 時は省略 |
| `WorkingDir` | `string` | `working_dir` | 必須 |
| `Branch` | `string` | `branch,omitempty` | 任意 |
| `CurrentTool` | `string` | `current_tool,omitempty` | 任意 |
| `FirstPrompt` | `string` | `first_prompt,omitempty` | 任意 |
| `InputTokens` | `int` | `input_tokens,omitempty` | 任意（0 は省略） |
| `OutputTokens` | `int` | `output_tokens,omitempty` | 任意（0 は省略） |

**PaneID を DTO で string に変換する理由**:
内部では整数として扱う（比較効率）が、外部 JSON では文字列として公開する。
これにより JSON 消費者（Lua プラグイン等）が型変換を意識せずに扱える。変換は `strconv.Itoa` で一箇所に集約する。

### 5.4 MarshalJSON の配置方針

| 型 | v1 | v2 |
|----|----|----|
| `SessionState` | `MarshalJSON()` を持つ | 削除（String() のみ） |
| DTO 型 | 存在しない | struct タグと `omitempty` で制御（カスタム実装不要） |

内部型に `MarshalJSON` を持たせると、意図せず JSON に直接シリアライズされた際の挙動が変わるリスクがある。v2 では「内部型は比較・集計に最適化、JSON 変換は DTO 層が責任を持つ」という原則を徹底する。

---

## 6. フォールバック方針

### ToolType 判定が不明な場合

プロセス名からツール種別を判定できない場合は `ToolUnknown` を設定する。`ToolUnknown` のセッションは:

- TUI: グレーアウト表示
- DTO 出力: `tool: "unknown"` として含める（省略しない）
- `Summary.ByTool`: `"unknown"` キーにカウント

### PaneID 解決失敗時

WezTerm が起動していない、またはペインと CWD の対応が取れない場合:

- `Session.PaneID = 0`（ゼロ値）
- `Session.Ambiguous = true`（ゼロ値と区別するため）
- DTO 出力: `pane_id` フィールドを `omitempty` で省略

### ScanResult のエラー時

`ScanResult.Err != nil` の場合、`StateUpdater.UpdateFromScan()` は既存の内部状態を**変更しない**。最後の正常スキャン結果を維持することで、一時的なスキャン失敗が TUI 表示に影響しないようにする。

---

## 7. v2 型依存関係図

```
Scanner ──Scan()──> ScanResult
                        │
                        ▼
              StateUpdater.UpdateFromScan()
                        │
                        ▼
              内部状態（Projects, Sessions）
                    /        \
      StateReader.Projects()  StateReader.Summary()
               │                      │
               ▼                      ▼
          []Project               Summary
               │
               ▼
           Exporter
               │
               ▼
    StatusOutput(DTO) → /tmp/baton-status.json
```

---

## 8. 変更サマリー

| カテゴリ | v1 | v2 |
|---------|----|----|
| セッション識別子 | UUID 文字列 (`ID`) | PID (int) |
| ペイン識別子 | 文字列 | 文字列（v3 で int から string に変更。ADR-0008） |
| ツール情報 | なし | `ToolType` 列挙型 |
| セッション参照 | ポインタスライス | 値スライス |
| 状態数 | 4 (Idle/Thinking/ToolUse/Error) | 5 (+Waiting) |
| イベント駆動 | `WatchEvent` チャネル | `ScanResult` 全量更新 |
| JSON 変換 | `MarshalJSON` on `SessionState` | DTO 層のみ |
| 集計 | `Project.ActiveCount` | `Summary` 型 |

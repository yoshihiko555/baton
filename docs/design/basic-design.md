# baton v2 基本設計書

**文書バージョン**: 0.4 (ドラフト)
**最終更新**: 2026-03-07
**前提文書**: `docs/requirements/requirements-v2.md` (v0.3)

---

## 1. 設計方針

### 1.1 アーキテクチャスタイル

**ポーリング + スナップショット照合**方式を採用する。

| 観点 | v1（イベント駆動） | v2（ポーリング） |
|------|-------------------|-----------------|
| セッション発見 | JSONL ファイル存在 | OS プロセス存在 |
| 状態更新 | fsnotify → channel → handler | ticker → Scan → UpdateFromScan |
| 状態管理 | イベント蓄積（追加のみ） | スナップショット照合（毎回全量比較） |
| 終了検出 | なし（ゾンビ化） | プロセス消失で自動削除 |

**選定理由**:
- プロセスの生死は「現在のスナップショット」でしか取得できない（イベントが発生しない）
- ポーリング間隔 2-3 秒は AI セッション監視として十分な粒度
- スナップショット照合により、状態の整合性が毎サイクルで保証される

### 1.2 レイヤー構成

```
┌─────────────────────────────────────────────────┐
│  Presentation Layer                              │
│  TUI (bubbletea) / Exporter (JSON)              │
├─────────────────────────────────────────────────┤
│  Application Layer                               │
│  main.go (scan loop, モード切替)                 │
├─────────────────────────────────────────────────┤
│  Domain Layer                                    │
│  StateManager / Model / StateResolver            │
├─────────────────────────────────────────────────┤
│  Infrastructure Layer                            │
│  Scanner / ProcessScanner / Terminal / Parser    │
└─────────────────────────────────────────────────┘
```

- **Presentation**: 表示とユーザー操作。Domain に依存するが、Infrastructure には直接依存しない
- **Application**: ワイヤリングとライフサイクル管理。各レイヤーを接続する
- **Domain**: ビジネスロジック。外部依存なし（インターフェース経由でのみ Infrastructure にアクセス）
- **Infrastructure**: 外部システム（OS, WezTerm, ファイルシステム）との通信

---

## 2. コンポーネント設計

### 2.1 コンポーネント一覧

```
internal/
├── core/
│   ├── model.go           # ドメイン型・インターフェース定義
│   ├── scanner.go         # Scanner オーケストレータ
│   ├── process.go         # ProcessScanner (ps パース)
│   ├── resolver.go        # StateResolver (CWD→JSONL 解決 + 状態判定)
│   ├── parser.go          # JSONL パーサー + IncrementalReader
│   ├── state.go           # StateManager (スナップショット照合)
│   └── exporter.go        # JSON ステータス出力
├── terminal/
│   ├── terminal.go        # Terminal インターフェース
│   └── wezterm.go         # WezTerm CLI 実装
├── config/
│   └── config.go          # YAML 設定読み込み
└── tui/
    ├── model.go           # bubbletea Model + Init
    ├── update.go          # イベントハンドリング
    └── view.go            # 画面描画
```

### 2.2 依存関係

```
main.go
  ├── config.Config
  ├── terminal.Terminal  ←── terminal.WezTerminal
  ├── core.Scanner       ←── core.DefaultScanner
  │     ├── terminal.Terminal
  │     └── core.ProcessScanner
  ├── core.StateManager
  │     └── core.StateResolver
  │           └── core.Parser
  ├── core.Exporter
  └── tui.Model
        └── core.StateReader (読み取り専用インターフェース)
```

**依存の方向**: 上位 → 下位のみ。循環依存なし。

---

## 3. インターフェース定義

### 3.1 Scanner インターフェース

```go
// Scanner は生きている AI セッションを発見する。
// エラーは ScanResult.Err に格納する（戻り値の error は使わない）。
// これにより、呼び出し側は常に ScanResult を受け取り、
// StateManager が Err の有無で前回スナップショット保持を判断できる。
type Scanner interface {
    Scan(ctx context.Context) ScanResult
}
```

**責務**: 1 回のスキャンで全 WezTerm ペインを調査し、AI プロセスが動作しているペインの情報を返す。

**実装**: `DefaultScanner`
- `Terminal.ListPanes()` で全ペイン取得
- ペインごとに `ProcessScanner.FindAIProcesses(tty)` を呼び出し
- 結果を `ScanResult` に集約

### 3.2 Terminal インターフェース

```go
type Terminal interface {
    Name() string
    IsAvailable() bool
    ListPanes() ([]Pane, error)
    FocusPane(paneID int) error
}
```

**v1 からの変更点**:
- `ListPanes()` の戻り値 `Pane` に `TTYName`, `IsActive` フィールドを追加

### 3.3 StateReader インターフェース

```go
// StateReader は TUI / Exporter が状態を読み取るための読み取り専用インターフェース。
type StateReader interface {
    Projects() []Project
    Summary() Summary
    Panes() []Pane // 最新スキャン時のペイン一覧。サブメニューでの TTY 表示に使用
}
```

**目的**: StateManager の内部状態を直接公開せず、読み取り専用ビューを提供する。
TUI と Exporter は StateReader のみに依存する。

### 3.4 StateUpdater インターフェース

```go
// StateUpdater はスキャン結果から状態を更新する。
type StateUpdater interface {
    UpdateFromScan(result ScanResult) error
}
```

**目的**: main.go の scan loop が StateManager を更新するためのインターフェース。

---

## 4. データモデル

### 4.1 型定義

#### ToolType

```go
type ToolType int

const (
    ToolClaude ToolType = iota
    ToolCodex
    ToolGemini
    ToolUnknown
)

// String は FR-06 の JSON スキーマで要求される文字列表現を返す。
func (t ToolType) String() string {
    switch t {
    case ToolClaude:  return "claude"
    case ToolCodex:   return "codex"
    case ToolGemini:  return "gemini"
    default:          return "unknown"
    }
}
```

#### SessionState

```go
type SessionState int

const (
    Idle SessionState = iota
    Thinking
    ToolUse
    Waiting
    Error
)

// String は FR-06 の JSON スキーマで要求される文字列表現を返す。
func (s SessionState) String() string {
    switch s {
    case Idle:     return "idle"
    case Thinking: return "thinking"
    case ToolUse:  return "tool_use"
    case Waiting:  return "waiting"
    case Error:    return "error"
    default:       return "idle"
    }
}
```

**JSON シリアライズ方針**:

内部のドメイン型（`Session`, `Project`）は `int` enum をそのまま使用する（比較・ソートの効率）。
外部出力（FR-06 の `/tmp/baton-status.json`）には **Exporter 用 DTO** を経由して文字列に変換する。

```go
// exporter 用の出力型。FR-06 スキーマに対応。
type StatusOutput struct {
    Version         int             `json:"version"`
    Timestamp       string          `json:"timestamp"`
    Projects        []ProjectOutput `json:"projects"`
    Summary         SummaryOutput   `json:"summary"`
    FormattedStatus string          `json:"formatted_status"` // 整形済みステータス文字列
}

type SessionOutput struct {
    PaneID      string `json:"pane_id"`
    Tool        string `json:"tool"`          // ToolType.String()
    State       string `json:"state"`         // SessionState.String()
    PID         int    `json:"pid"`
    WorkingDir  string `json:"working_dir"`
    Branch      string `json:"branch,omitempty"`
    CurrentTool string `json:"current_tool,omitempty"`
    FirstPrompt string `json:"first_prompt,omitempty"`
    InputTokens  int   `json:"input_tokens,omitempty"`
    OutputTokens int   `json:"output_tokens,omitempty"`
}
```

Exporter は `Session` → `SessionOutput` の変換時に `Tool.String()` / `State.String()` を呼び出す。

#### Session

```go
type Session struct {
    // --- 必須フィールド（Scanner から確定） ---
    PaneID     int          `json:"-"`            // Pane.ID と同じ int 型。JSON 出力は DTO で string 化
    Tool       ToolType     `json:"tool"`
    State      SessionState `json:"state"`
    PID        int          `json:"pid"`
    WorkingDir string       `json:"working_dir"`

    // --- 内部フィールド（JSON 出力には含めない） ---
    LastActivity     time.Time `json:"-"` // JSONL 最終エントリの timestamp。Codex/Gemini はスキャン時刻
    Ambiguous        bool      `json:"-"` // true: 同一CWDに複数Claudeがあり、JSONL対応が不確定
    CandidatePaneIDs []int     `json:"-"` // Ambiguous==true の場合、候補となる全ペインID（Pane.ID と同じ int 型）

    // --- 任意フィールド（StateResolver / session-meta から補完） ---
    Branch      string `json:"branch,omitempty"`
    CurrentTool string `json:"current_tool,omitempty"`
    FirstPrompt string `json:"first_prompt,omitempty"`
    InputTokens int    `json:"input_tokens,omitempty"`
    OutputTokens int   `json:"output_tokens,omitempty"`
}
```

**Ambiguous フラグの設定条件**:
- 同一 CWD に Claude プロセスが **2 つ以上**存在する場合、該当する全セッションの `Ambiguous` を `true` に設定
- `CandidatePaneIDs` にはその CWD の全 Claude ペイン ID を格納
- 単一 Claude、Codex、Gemini のセッションでは常に `Ambiguous == false`

**TUI での利用**:
- `Ambiguous == true` のセッション行には `~` マークを表示し、状態/補助情報が推定値であることを示す
- Enter 押下時に `Ambiguous == true` なら `CandidatePaneIDs` からサブメニューを生成
- Enter 押下時に `Ambiguous == false` なら `PaneID` に直接ジャンプ

**サブメニューでの Pane 情報参照**:
- TUI Model は `StateManager` から取得した `[]Pane`（最新スキャン時のペイン一覧）を保持する
- サブメニュー表示時に `CandidatePaneIDs` の各 ID で `[]Pane` をルックアップし、`TTYName` を取得する
- これにより `PaneID` と `TTY` の両方をサブメニューに表示できる

```go
// 右ペイン表示例（Ambiguous の場合）
// ~🟡 claude  thinking  feat-auth     ← "~" は推定値マーク
//     Bash  |  12.5k / 8.3k tokens
```

#### Project

```go
type Project struct {
    Path     string    `json:"path"`
    Name     string    `json:"name"`
    Sessions []Session `json:"sessions"`
}
```

#### DetectedProcess

```go
type DetectedProcess struct {
    PID      int
    Name     string   // COMM 名
    ToolType ToolType
    PaneID   int      // Pane.ID と同じ int 型
    TTY      string
    CWD      string   // 正規化済み（file:// 除去済み）
}
```

#### ScanResult

```go
type ScanResult struct {
    Processes []DetectedProcess
    Timestamp time.Time
    Err       error // WezTerm CLI 失敗等。非 nil の場合、StateManager は前回スナップショットを保持
}
```

#### Summary

```go
type Summary struct {
    TotalSessions int            `json:"total_sessions"`
    Active        int            `json:"active"`
    Waiting       int            `json:"waiting"`
    ByTool        map[string]int `json:"by_tool"`
}
```

### 4.2 Pane（terminal パッケージ）

```go
type Pane struct {
    ID         int    `json:"pane_id"`    // WezTerm CLI は数値で返す
    Title      string `json:"tab_title"`
    TabID      int    `json:"tab_id"`     // WezTerm CLI は数値で返す
    WorkingDir string `json:"cwd"`        // 正規化済み
    TTYName    string `json:"tty_name"`
    IsActive   bool   `json:"is_active"`
}
```

> **型の選択理由**: `wezterm cli list --format json` は `pane_id`, `tab_id` を JSON 数値で返す。
> `json.Unmarshal` で型不一致エラーを避けるため、内部型も `int` とする。
> 外部出力（FR-06）の `pane_id` は `SessionOutput` で `string` に変換する（`strconv.Itoa`）。

**CWD 正規化**: `WezTerminal.ListPanes()` 内で以下を実施:
1. `file://` プレフィックスを除去
2. 末尾スラッシュを除去

### 4.3 JSONL 関連型（parser パッケージ）

```go
type Entry struct {
    Type    string  `json:"type"`
    SubType string  `json:"subtype,omitempty"`
    Message Message `json:"message,omitempty"`

    // エントリ共通メタデータ（実機検証済み: 全エントリのトップレベルに存在）
    SessionID string `json:"sessionId,omitempty"`
    GitBranch string `json:"gitBranch,omitempty"`
    Timestamp string `json:"timestamp,omitempty"`

    // progress エントリ用
    Data ProgressData `json:"data,omitempty"`
}

type Message struct {
    Role       string         `json:"role"`
    Content    []ContentBlock `json:"content"`
    StopReason string         `json:"stop_reason,omitempty"`
}

type ContentBlock struct {
    Type string `json:"type"`
    Name string `json:"name,omitempty"`
}

type ProgressData struct {
    Type string `json:"type"` // "hook_progress", "bash_progress", "agent_progress"
}
```

---

## 5. 処理シーケンス

### 5.1 スキャンサイクル（モード別の駆動方式）

**重要**: スキャン処理（`Scanner.Scan` → `StateManager.UpdateFromScan`）は単一の関数 `doScan()` に集約する。
モードによって「誰が `doScan()` を呼ぶか」だけが異なる。

| モード | 駆動方式 | 呼び出し元 |
|--------|---------|-----------|
| TUI | bubbletea の `TickMsg` | `Update()` 内で `doScan()` → `ScanResultMsg` を返す |
| ヘッドレス | goroutine 内の `time.Ticker` | ticker → `doScan()` → `Exporter.Write()` |
| ワンショット | `main()` 直接呼び出し | `doScan()` → `Exporter.Write()` → 終了 |

```go
// doScan はモード非依存のスキャン+状態更新処理。
// Scanner は常に ScanResult を返す。エラーは ScanResult.Err に格納される。
// StateManager.UpdateFromScan が Err の有無を判定し、
// エラー時は前回スナップショットを保持する。
func doScan(ctx context.Context, scanner Scanner, sm StateUpdater) error {
    result := scanner.Scan(ctx)
    return sm.UpdateFromScan(result)
}
```

**シーケンス図**（TUI モードの例）:

```
┌──────┐     ┌─────────┐     ┌──────────────┐     ┌──────────┐
│TUI   │     │Scanner  │     │StateManager  │     │View()    │
│Update│     │         │     │              │     │          │
└──┬───┘     └────┬────┘     └──────┬───────┘     └────┬─────┘
   │              │                 │                   │
   │ TickMsg      │                 │                   │
   │─ doScan() ──>│                 │                   │
   │              │                 │                   │
   │              │ ListPanes()     │                   │
   │              │───> Terminal    │                   │
   │              │<─── []Pane      │                   │
   │              │                 │                   │
   │              │ FindAIProcesses(tty) × N            │
   │              │───> ProcessScanner                  │
   │              │<─── []DetectedProcess               │
   │              │                 │                   │
   │  ScanResult  │                 │                   │
   │<─────────────│                 │                   │
   │              │                 │                   │
   │  UpdateFromScan(result)        │                   │
   │───────────────────────────────>│                   │
   │              │                 │                   │
   │              │                 │ (Claude セッション │
   │              │                 │  のみ JSONL 解析) │
   │              │                 │                   │
   │  ScanResultMsg(Projects, Summary)                  │
   │────────────────────────────────────────────────────>│
   │              │                 │                   │
```

### 5.2 StateManager.UpdateFromScan の処理フロー

```
UpdateFromScan(result ScanResult):
  1. result がエラー（ScanResult.Err != nil）の場合:
     → 前回のスナップショットをそのまま保持して return（※指摘2対応）
  2. result.Processes を CWD でグループ化 → プロジェクト単位に分類
  3. 各 DetectedProcess について:
     a. ToolType == ToolClaude の場合:
        - StateResolver.ResolveState(cwd, claudeProcesses) を呼び出し
          → CWD からアクティブ JSONL 群を解決（複数対応）
          → 各 JSONL の最終エントリから状態判定
          → session-meta から補助情報を取得
        - 判定結果で Session を構築
     b. ToolType == ToolCodex / ToolGemini の場合:
        - State = Thinking（プロセス存在 = 動作中）
        - 必須フィールドのみで Session を構築
  4. 前回のスナップショットと比較:
     - 前回あって今回ない PID → セッション削除
     - 今回新たに出現した PID → セッション追加
     - 両方にある PID → 状態更新
  5. Summary を再計算
```

### 5.3 StateResolver の JSONL 解決フロー

**課題**: 同一 CWD で複数の Claude セッションが並行動作する場合、各プロセスに正しい JSONL を対応付ける必要がある。

**実機検証結果（2026-03-07）**:
- JSONL ファイル名 = `{sessionId}.jsonl`（UUID）
- 各 JSONL エントリに `sessionId` フィールドが存在する
- Claude プロセスは JSONL を常時オープンしない（`lsof` でのファイルディスクリプタ紐付け不可）
- アクティブなセッションの JSONL は頻繁に mtime が更新される

**解決方針: アクティブ JSONL インデックス**

```
ResolveState(cwd, claudeProcesses []DetectedProcess):
  1. CWD をスラッグ化: "/Users/foo/bar" → "-Users-foo-bar"
  2. ~/.claude/projects/{slug}/ 内の .jsonl ファイルを列挙
  3. アクティブ JSONL の絞り込み:
     - mtime が直近 scanInterval×2（例: 6秒）以内のファイルを「アクティブ候補」とする
     - アクティブ候補数 == Claude プロセス数 → 1:1 対応と推定
     - アクティブ候補数 > Claude プロセス数 → mtime 降順で上位 N 本を使用
       **注意**: この場合、Waiting 状態の JSONL が mtime が古いために候補から外れる可能性がある
       （Waiting 中は JSONL 書き込みが停止するため mtime が更新されない）
     - アクティブ候補数 < Claude プロセス数 → 対応が取れないプロセスは Thinking にフォールバック
     - アクティブ候補数 == 0 → 全プロセスを Thinking にフォールバック
  4. 各アクティブ JSONL に対して:
     a. IncrementalReader で末尾を読み取り
     b. 最終エントリの type/属性から状態判定:
        - system + turn_duration        → Idle
        - assistant + end_turn          → Idle
        - assistant + tool_use          → Waiting
        - progress                      → ToolUse
        - user + tool_result            → Thinking
        - user (other)                  → Thinking
        - assistant + thinking block    → Thinking
        - assistant + error block       → Error
        - fallback                      → Idle
     c. 最終エントリの gitBranch フィールドからブランチ名を取得
     d. 最終の tool_use エントリの content[].name からツール名を取得
  5. session-meta からの補助情報取得（ベストエフォート）:
     - JSONL ファイル名（UUID）→ session-meta/{UUID}.json
     - first_prompt, input_tokens, output_tokens を取得
  6. 取得失敗時は該当フィールドを空文字列 / 0 のまま返す
```

**JSONL-プロセス対応の設計判断**:

同一 CWD で複数の Claude セッションが動作する場合、JSONL とプロセスの厳密な 1:1 紐付けは現時点では不可能である（Claude Code が PID を JSONL に記録しないため）。

**問題の本質**: TTY→PID（どのペインにどのプロセスがいるか）は確実に特定できるが、PID→JSONL（どのプロセスがどの JSONL に書いているか）は特定できない。このため、JSONL から判定した状態（Waiting 等）を特定のペインに紐付けることができない。

**対処方針: 2 段階のモデル**

**(A) 単一 Claude セッション（同一 CWD に Claude が 1 つ）**:
- PID→JSONL の対応は一意に確定する（アクティブ JSONL が 1 本のみ）
- 状態判定、補助情報、ペインジャンプすべて正確
- **これが典型的なユースケースであり、完全に US-02/US-05 を満たす**

**(B) 複数 Claude セッション（同一 CWD に Claude が 2 つ以上）**:
- セッション数の検出は正確（プロセス数 = セッション数）
- 各 JSONL の状態判定はそれぞれ独立に正確
- **ただし、どの状態がどのペインに対応するかは不確定**
- このため、以下の特別な表示・操作ルールを適用する:

| 項目 | 単一 Claude | 複数 Claude（同一 CWD） |
|------|-----------|----------------------|
| 状態表示 | ペインごとに正確 | プロジェクト全体で正確（ペイン対応は不確定） |
| 補助情報 | ペインごとに正確 | ペイン対応が入れ替わる可能性あり |
| ペインジャンプ | 正確 | **プロジェクト内の全 Claude ペインを候補として提示**（後述） |
| 集計（Active/Waiting） | 正確 | ベストエフォート（Waiting 中の JSONL が mtime 停滞で候補落ちする可能性あり） |

**複数 Claude 時のジャンプ動作**:

TUI で Waiting セッションを選択して Enter を押した場合:
1. 対象プロジェクトの同一 CWD に Claude が 1 つ → そのペインに直接ジャンプ
2. 対象プロジェクトの同一 CWD に Claude が 2 つ以上 → **ペイン選択サブメニューを表示**
   - 該当する全 Claude ペインを一覧表示（PaneID, TTY）
   - ユーザーが選択してジャンプ
   - これにより「間違ったペインにジャンプ」を防止する

**将来の改善パス**:
- Claude Code が PID を JSONL に記録するようになれば、PID→JSONL の厳密な紐付けが可能
- それまでは上記 2 段階モデルで運用する

### 5.4 CWD → JSONL パス解決

```
入力: CWD = "/Users/yoshihiko/ghq/github.com/yoshihiko555/baton"
     Claude プロセス数 = 2

1. スラッグ化:
   "/" を "-" に、先頭 "/" も "-" に
   → "-Users-yoshihiko-ghq-github-com-yoshihiko555-baton"

2. ディレクトリ解決:
   ~/.claude/projects/-Users-yoshihiko-ghq-github-com-yoshihiko555-baton/

3. .jsonl ファイル列挙（mtime 降順）:
   74eca77c-..-.jsonl (mtime: 2026-03-07 16:49)  ← アクティブ候補
   f7f2ed94-..-.jsonl (mtime: 2026-03-07 16:48)  ← アクティブ候補
   a2b47c96-..-.jsonl (mtime: 2026-03-06 12:00)  ← 非アクティブ

4. アクティブ候補 2 本 == Claude プロセス 2 → 両方を使用

5. UUID 抽出:
   → 74eca77c-..., f7f2ed94-...

6. session-meta パス:
   ~/.claude/usage-data/session-meta/74eca77c-....json
   ~/.claude/usage-data/session-meta/f7f2ed94-....json
```

---

## 6. 外部インターフェース

### 6.1 WezTerm CLI

**使用コマンド**:

| コマンド | 用途 | 出力形式 |
|---------|------|---------|
| `wezterm cli list --format json` | ペイン一覧取得 | JSON 配列 |
| `wezterm cli activate-pane --pane-id <id>` | ペインフォーカス | なし |

**wezterm cli list の出力フィールド**:

```json
[
  {
    "pane_id": 0,
    "tab_id": 0,
    "tab_title": "...",
    "workspace": "default",
    "size": {"rows": 24, "cols": 80},
    "cursor_x": 0,
    "cursor_y": 0,
    "cursor_shape": "Default",
    "cursor_visibility": "Visible",
    "tty_name": "/dev/ttys003",
    "cwd": "file:///Users/yoshihiko/project",
    "is_active": true
  }
]
```

**使用するフィールド**: `pane_id`, `tab_id`, `tab_title`, `tty_name`, `cwd`, `is_active`

### 6.2 ps コマンド

**使用コマンド**: `ps -t <tty> -o pid,ppid,comm`

**出力例**:
```
  PID  PPID COMM
12345  1000 zsh
12346 12345 claude
12400 12346 node
```

**パース方法**:
1. ヘッダ行をスキップ
2. 各行を空白分割で PID, PPID, COMM を抽出
3. COMM が `claude` / `codex` / `gemini` に完全一致するものを返却

### 6.3 JSON ステータス出力

出力スキーマは要件定義書 FR-06 に定義済み。ここでは書き込み方式と Lua との受け渡し契約を補足する。

**アトミック書き込み手順**:
1. 同一ディレクトリに一時ファイルを作成（`/tmp/baton-status.json.tmp.{pid}`）
2. JSON を一時ファイルに書き込み
3. `os.Rename()` で本来のパスにアトミックに置換
4. エラー時は一時ファイルを削除（残骸を残さない）

**Lua プラグインとの受け渡し契約**:

baton 本体（Go）が設定ファイル（`statusbar.*`）のテンプレートを評価し、**整形済みステータス文字列**を JSON に含める。Lua プラグインはこの文字列をそのまま表示するだけで、テンプレート解釈やアイコン解決を行わない。

```jsonc
{
  "version": 2,
  "summary": { ... },
  "projects": [ ... ],
  // baton が生成した整形済み文字列（Lua はこれをそのまま表示）
  "formatted_status": "baton: 3 active | ⚠ 1 waiting | 2⚡ 1📦"
}
```

| 責務 | baton (Go) | Lua プラグイン |
|------|-----------|---------------|
| テンプレート評価 | `statusbar.format` を Go template で展開 | しない |
| アイコン解決 | `statusbar.tool_icons` を適用 | しない |
| Waiting 強調色 | - | `summary.waiting > 0` で判定して色付け（文字列解析に依存しない） |
| version チェック | - | `version != 2` ならフォールバック表示 |
| JSON パース | - | `/tmp/baton-status.json` を読み込み |

**Waiting 強調の判定方法**: Lua は `formatted_status` の文字列内容を解析しない。
`summary.waiting > 0` の構造化データで判定し、`formatted_status` 全体にオレンジ色を適用する。
これにより、テンプレートをカスタマイズしても強調ロジックが壊れない。

この設計により、Lua 側は baton の設定ファイルを読む必要がなく、JSON のみに依存する。

### 6.4 JSONL ファイル

**パス**: `~/.claude/projects/{slug}/{uuid}.jsonl`

**読み取り方式**: `IncrementalReader`
- ファイルの末尾から最終エントリを読み取る
- 前回読み取り位置を記録し、変更があった部分のみを再パースする
- ファイルが読めない場合は状態を **Thinking** にフォールバックする（プロセスは生存しているため。7.2 節参照）

### 6.5 session-meta ファイル

**パス**: `~/.claude/usage-data/session-meta/{uuid}.json`

**読み取り方式**: 毎回全量読み込み（ファイルサイズが小さいため）

**使用フィールド**:

| フィールド | 用途 | 型 |
|-----------|------|---|
| `first_prompt` | タスク概要表示 | string |
| `input_tokens` | トークン使用量表示 | int |
| `output_tokens` | トークン使用量表示 | int |

---

## 7. エラーハンドリング

### 7.1 方針

**フォールバック優先**: エラーが発生してもプロセスを停止せず、利用可能な情報で継続表示する。

### 7.2 エラー分類と対応

| エラー | 影響範囲 | 対応 |
|--------|---------|------|
| WezTerm CLI 実行失敗 | 全体 | **前回のスナップショットを保持**する。ScanResult にエラーを格納し、UpdateFromScan は更新をスキップ。次回 ticker で再試行 |
| `ps` コマンド失敗（特定 TTY） | 当該ペイン | 当該ペインをスキップ。他ペインは正常処理 |
| JSONL ファイル読み取り失敗 | 当該セッション | 状態を **Thinking** にフォールバック（プロセスは生存しているため、Idle より安全）。必須フィールドは維持 |
| JSONL パースエラー（不正 JSON 行） | 当該エントリ | 当該行をスキップ。直前の有効エントリで判定 |
| session-meta 読み取り失敗 | 補助情報 | 該当フィールドを空のまま（`-` 表示） |
| CWD が空 | プロジェクト名 | プロジェクト名を `(unknown)` とする |
| CWD → スラッグ変換で JSONL ディレクトリが見つからない | 状態判定 | 状態を Thinking（プロセスは生存）とする |

### 7.3 ログ出力

- エラーは `log` パッケージで記録する（TUI モード時は `/tmp/baton.log` にリダイレクト）
- 毎スキャンで発生する既知エラー（JSONL 未発見等）は `debug` レベルにして出力を抑制する
- 予期しないエラー（WezTerm CLI の異常終了等）は `warn` レベルで記録する

---

## 8. 設定

### 8.1 設定ファイル

**パス**: `~/.config/baton/config.yaml`（任意）

```yaml
# スキャン間隔（秒）
scan_interval: 3

# ステータス JSON 出力先
status_file: /tmp/baton-status.json

# Claude プロジェクトディレクトリ
claude_projects_dir: ~/.claude/projects

# session-meta ディレクトリ
session_meta_dir: ~/.claude/usage-data/session-meta

# ログレベル: debug, info, warn, error
log_level: info

# ステータスバー表示カスタマイズ（NFR-03 対応）
statusbar:
  # 表示フォーマット（Go template）
  # 使用可能な変数: .Active, .Waiting, .Total, .ByTool (map[string]int)
  format: "baton: {{.Active}} active{{if .Waiting}} | ⚠ {{.Waiting}} waiting{{end}}"
  # ツール別カウントの表示
  show_tool_counts: true
  # ツール別アイコン（変更可能）
  tool_icons:
    claude: "⚡"
    codex: "📦"
    gemini: "🔮"
```

### 8.2 コマンドラインフラグ

| フラグ | デフォルト | 説明 |
|--------|----------|------|
| `--no-tui` | false | ヘッドレスモード |
| `--once` | false | ワンショットモード |
| `--config` | `~/.config/baton/config.yaml` | 設定ファイルパス |
| `--version` | - | バージョン表示 |

### 8.3 デフォルト値の解決

設定値の優先順位: コマンドラインフラグ > 設定ファイル > ハードコードデフォルト

## 9. TUI 設計

### 9.1 レイアウト

```
┌─────────────────────────────┬──────────────────────────────────────┐
│  Projects                   │  Sessions (myproject)                │
│                             │                                      │
│  ▸ myproject     2 active   │  🟡 claude  thinking  feat-auth     │
│    other-proj    1 waiting  │     Bash  |  12.5k / 8.3k tokens    │
│                             │                                      │
│                             │  🟠 claude  waiting   feat-login    │
│                             │     Edit  |  5.2k / 3.1k tokens     │
│                             │                                      │
│                             │  🟡 codex   thinking  -             │
│                             │     -     |  -                       │
│                             │                                      │
├─────────────────────────────┴──────────────────────────────────────┤
│  3 sessions | 2 active | 1 waiting          q:quit  enter:jump    │
└────────────────────────────────────────────────────────────────────┘
```

### 9.2 左ペイン（プロジェクト一覧）

| 表示項目 | ソース |
|----------|--------|
| プロジェクト名 | CWD の最後のパスコンポーネント |
| アクティブ数 | Thinking + ToolUse + Waiting のカウント |
| 承認待ち数 | Waiting のカウント（0 の場合は非表示） |

### 9.3 右ペイン（セッション詳細）

| 行 | 表示内容 |
|----|---------|
| 1 行目 | `{状態色アイコン} {ツール種別}  {状態名}  {ブランチ名}` |
| 2 行目 | `    {実行中ツール名}  |  {入力トークン} / {出力トークン} tokens` |

**表示ソート順**（FR-02 の優先順に準拠）:

セッション一覧は以下の優先順でソートする:

| 優先度 | 状態 | 理由 |
|--------|------|------|
| 1 | Waiting | ユーザーの即時対応が必要 |
| 2 | Error | 異常状態の確認が必要 |
| 3 | Thinking | AI が動作中 |
| 4 | ToolUse | ツール実行中（承認済み） |
| 5 | Idle | 入力待ち（緊急度低） |

同一状態内では `LastActivity`（JSONL の最終タイムスタンプ）降順でソートする。

### 9.4 ステータスバー

`{セッション総数} sessions | {active数} active | {waiting数} waiting          q:quit  enter:jump`

### 9.5 色定義

| 状態 | 色 | lipgloss 近似値 |
|------|---|----------------|
| Idle | グレー | `lipgloss.Color("240")` |
| Thinking | 黄色 | `lipgloss.Color("226")` |
| ToolUse | シアン | `lipgloss.Color("43")` |
| Waiting | オレンジ | `lipgloss.Color("208")` |
| Error | 赤 | `lipgloss.Color("196")` |

### 9.6 キーバインド

**メイン画面**:

| キー | アクション |
|------|----------|
| `↑` / `k` | 前の項目 |
| `↓` / `j` | 次の項目 |
| `Tab` | ペイン切替（プロジェクト ↔ セッション） |
| `Enter` | 選択中セッションのペインにジャンプ（Ambiguous の場合はサブメニュー表示） |
| `q` / `Ctrl+C` | 終了 |

**ペイン選択サブメニュー**（Ambiguous セッションで Enter 時に表示）:

| キー | アクション |
|------|----------|
| `↑` / `k` | 前の候補ペイン |
| `↓` / `j` | 次の候補ペイン |
| `Enter` | 選択したペインにジャンプ |
| `Esc` | サブメニューを閉じてメイン画面に戻る |

サブメニューには候補ペインの PaneID と TTY を表示する:
```
ペインを選択してください:
  ▸ pane 5 (ttys010)
    pane 6 (ttys013)
    pane 8 (ttys016)
```

### 9.7 TUI の更新方式

v1 の `tea.Sub`（channel 購読）から **ticker ベース**に変更する。

```go
func (m Model) Init() tea.Cmd {
    return tea.Batch(
        tickCmd(), // 2-3秒間隔の ticker
    )
}

type ScanResultMsg struct {
    Projects []core.Project
    Summary  core.Summary
}

func tickCmd() tea.Cmd {
    return tea.Tick(scanInterval, func(t time.Time) tea.Msg {
        return TickMsg(t)
    })
}
```

**スキャン駆動の流れ**（5.1 節 `doScan()` を参照）:

1. `TickMsg` 受信 → `Update()` 内で `doScan(ctx, scanner, stateManager)` を呼び出し
2. `doScan()` が `Scanner.Scan()` → `StateManager.UpdateFromScan()` を実行
3. `Update()` が `StateReader.Projects()` / `StateReader.Summary()` を読み取り `ScanResultMsg` を返す
4. `ScanResultMsg` 受信時に画面を再描画

**注意**: TUI が `doScan()` を呼ぶため、ヘッドレスモードの goroutine と二重にスキャンされることはない。
モード選択は `main.go` で排他的に行われる（TUI モード時はヘッドレスの goroutine は起動しない）。

---

## 10. WezTerm ステータスバー設計

### 10.1 Lua プラグインの動作

```lua
-- baton-status.lua
-- /tmp/baton-status.json を定期読み込みし、ステータスバーに反映

local CACHE_TTL = 5  -- 秒
local STATUS_FILE = "/tmp/baton-status.json"
```

### 10.2 表示フォーマット

```
baton: 3 active | 1 waiting | 2⚡ 1📦
```

| 部分 | 意味 |
|------|------|
| `3 active` | Thinking + ToolUse + Waiting の合計 |
| `1 waiting` | Waiting のみ（0 の場合は非表示） |
| `2⚡` | Claude セッション数 |
| `1📦` | Codex セッション数 |

### 10.3 承認待ちの強調表示

承認待ち（Waiting）セッションが 1 件以上存在する場合、ステータスバーの表示を視覚的に変更する。

| 条件 | 通常表示 | 強調表示 |
|------|---------|---------|
| waiting == 0 | `baton: 3 active \| 2⚡ 1📦` | （強調なし、`waiting` 部分自体を非表示） |
| waiting >= 1 | - | `baton: 3 active \| ⚠ 1 waiting \| 2⚡ 1📦` |

**強調方法**（Lua 実装）:
- Waiting の有無は **`summary.waiting > 0`** で判定する（`formatted_status` の文字列内容を解析しない）
- Waiting ありの場合、`formatted_status` 全体をオレンジ色（`#FF8800`）で表示
- Waiting なしの場合、`formatted_status` をデフォルト色で表示
- テンプレートのカスタマイズ（日本語化等）に影響されない設計

```lua
local formatted = data.formatted_status or "baton: no data"

if data.summary and data.summary.waiting > 0 then
    -- summary.waiting で判定（文字列解析ではない）
    table.insert(elements, wezterm.format({
        { Foreground = { Color = "#FF8800" } },
        { Text = formatted },
    }))
else
    table.insert(elements, wezterm.format({
        { Text = formatted },
    }))
end
```

### 10.4 version チェック

```lua
local data = json.decode(content)
if data.version ~= 2 then
    return "baton: unknown format"
end
```

---

## 11. v1 コンポーネントの処遇

| v1 コンポーネント | v2 での扱い | 理由 |
|------------------|------------|------|
| `Watcher` (watcher.go) | **保持・不使用** | 将来的に JSONL 変更の即時通知に活用可能 |
| `Parser` (parser.go) | **改修** | IncrementalReader は継続使用。Entry/Message 型を拡張 |
| `StateManager` (state.go) | **大幅改修** | HandleEvent → UpdateFromScan に置換。Watcher 依存を削除 |
| `Exporter` (exporter.go) | **改修** | 出力スキーマを version: 2 に変更 |
| `Model` (model.go) | **拡張** | ToolType, Waiting, DetectedProcess, ScanResult 等を追加 |
| TUI 各ファイル | **改修** | WatchEventMsg → TickMsg/ScanResultMsg に変更。View に Waiting 表示追加 |

---

## 12. テスト戦略

### 12.1 ユニットテスト

| コンポーネント | テスト方針 |
|--------------|-----------|
| ProcessScanner | `execFn` を注入し、`ps` 出力のモックでテスト |
| StateResolver | テスト用 JSONL ファイルを `t.TempDir()` に作成してテスト |
| StateManager | モック ScanResult を与えてスナップショット照合をテスト |
| Parser | 既存テストを拡張（progress エントリ、新フィールド対応） |
| WezTerminal | `execFn` を注入し、`wezterm cli` 出力のモックでテスト |

### 12.2 DI パターン

外部コマンド実行は全て関数注入でテスタブルにする:

```go
type ProcessScanner struct {
    execFn func(tty string) ([]byte, error) // テスト時に差し替え
}

type WezTerminal struct {
    execFn func(args ...string) ([]byte, error) // テスト時に差し替え
}
```

### 12.3 テストカバレッジ目標

NFR-04 に基づき **80% 以上**を目標とする。

---

## 13. 将来の拡張ポイント

| 拡張 | 影響範囲 | 準備 |
|------|---------|------|
| Linux 対応 | ProcessScanner | `/proc/{pid}/comm` ベースの実装を追加 |
| 他ターミナル対応 | Terminal インターフェース | 新しい実装を追加するだけ |
| 新 AI ツール追加 | ToolType + ProcessScanner | COMM 名マッチング追加のみ |
| JSONL リアルタイム監視 | Watcher（保持済み） | Scanner が発見したセッションの JSONL のみを watch |
| 通知機能 | Presentation Layer | StateReader を監視する Notifier を追加 |

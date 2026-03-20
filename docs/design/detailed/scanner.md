# Scanner 詳細設計

> baton v2 / 作成日: 2026-03-07

---

## 目次

1. [コンポーネント概要](#コンポーネント概要)
2. [インターフェース定義](#インターフェース定義)
3. [コンポーネント間契約](#コンポーネント間契約)
4. [データ変換ルール](#データ変換ルール)
5. [エラー処理方針](#エラー処理方針)
6. [ps コマンドのパース詳細](#ps-コマンドのパース詳細)
7. [テスト戦略](#テスト戦略)
8. [設計判断の記録](#設計判断の記録)

---

## コンポーネント概要

Scanner コンポーネントは「どのターミナルペインで、どの AI ツールが動いているか」を検出する責務を持つ。

```
┌─────────────────────────────────────────────┐
│                 main.go (doScan)            │
└───────────────────┬─────────────────────────┘
                    │ Scan(ctx)
                    ▼
┌─────────────────────────────────────────────┐
│             DefaultScanner                  │
│                                             │
│  1. Terminal.ListPanes()                    │
│  2. for each Pane → ProcessScanner.Find()   │
│  3. 結果を ScanResult に集約                 │
└───────┬────────────────────┬────────────────┘
        │                    │
        ▼                    ▼
┌───────────────┐   ┌─────────────────────────┐
│   Terminal    │   │      ProcessScanner      │
│  (interface)  │   │                          │
│               │   │  ps -t <tty> を実行       │
│ .ListPanes()  │   │  出力をパースして         │
│               │   │  DetectedProcess を返す   │
└───────────────┘   └─────────────────────────┘
```

### 各コンポーネントの責務

| コンポーネント | 責務 | 依存 |
|--------------|------|------|
| `DefaultScanner` | ペイン列挙・並列スキャン・結果集約 | `Terminal`, `ProcessScanner` |
| `ProcessScanner` | ps コマンド実行・出力パース・ToolType マッピング | `execFn`（注入可能） |
| `Terminal` | ペイン情報の取得（WezTerm 等の実装に委ねる） | 外部 CLI |

---

## インターフェース定義

### Scanner インターフェース

```go
type Scanner interface {
    Scan(ctx context.Context) ScanResult
}
```

### ScanResult

```go
type ScanResult struct {
    Processes []DetectedProcess
    Panes     []Pane    // Terminal.ListPanes() から取得したペイン一覧（Ambiguous 解決用）
    Timestamp time.Time // スキャン実行時刻
    Err       error     // スキャン全体が失敗した場合のみ設定。非 nil の場合、StateManager は前回スナップショットを保持
}
```

### DetectedProcess

```go
type DetectedProcess struct {
    PID      int
    Name     string   // COMM 名
    ToolType ToolType
    PaneID   string   // Pane.ID と同じ string 型（tmux: "%5", wezterm: "42"）
    TTY      string
    CWD      string   // Pane.WorkingDir からコピー（正規化済み）
}
```

### ToolType

```go
type ToolType int

const (
    ToolClaude ToolType = iota
    ToolCodex
    ToolGemini
    ToolUnknown
)
```

### ProcessScanner

```go
type ProcessScanner struct {
    execFn func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func NewProcessScanner() *ProcessScanner
func NewProcessScannerWithExec(execFn ...) *ProcessScanner  // テスト用

func (s *ProcessScanner) FindAIProcesses(ctx context.Context, tty string) ([]DetectedProcess, error)
func (s *ProcessScanner) HasChildProcesses(ctx context.Context, pid int) (bool, error)  // Codex 状態判定用
```

---

## コンポーネント間契約

### DefaultScanner ↔ Terminal

| 項目 | 仕様 |
|------|------|
| 呼び出し | `Terminal.ListPanes()` |
| 戻り値 | `([]Pane, error)` |
| エラー時 | `ScanResult.Err` に格納して即 return（後続ペインのスキャンは行わない） |
| Pane に必要なフィールド | `ID string`、`TTYName string`、`WorkingDir string`、`CurrentCommand string`（tmux 最適化用） |

### DefaultScanner ↔ ProcessScanner

| 項目 | 仕様 |
|------|------|
| 呼び出し | `ProcessScanner.FindAIProcesses(ctx, pane.TTYName)` |
| 戻り値 | `([]DetectedProcess, error)` |
| エラー時 | 当該ペインをスキップ（警告ログを出力）、他ペインは続行 |
| 前提 | ProcessScanner は TTYName のみ受け取るため、`DetectedProcess.PaneID` と `CWD` は未設定で返される。DefaultScanner が FindAIProcesses の結果を受け取った後に Pane 情報からコピーする（下記コード例参照） |

**補足: PaneID / CWD のセット責任**

ProcessScanner は TTYName のみを受け取り、ps 出力から得られる情報（PID, COMM）のみを返す。
PaneID と CWD の付与は DefaultScanner の責任とする。これにより ProcessScanner の責務を純粋な「プロセス検出」に絞ることができる。

```go
// DefaultScanner 内の処理イメージ
for _, pane := range panes {
    // CurrentCommand フィルタ（tmux 最適化）
    // CurrentCommand が空（WezTerm）の場合はフィルタしない
    if pane.CurrentCommand != "" && !isAICommand(pane.CurrentCommand) {
        continue
    }
    procs, err := s.processScanner.FindAIProcesses(ctx, pane.TTYName)
    if err != nil {
        log.Printf("warn: skip pane %s: %v", pane.TTYName, err)
        continue
    }
    for i := range procs {
        procs[i].PaneID = pane.ID
        procs[i].CWD = pane.WorkingDir
    }
    results = append(results, procs...)
}
```

---

## データ変換ルール

### 1. Pane.TTYName → ps コマンド引数

```
ps -t <pane.TTYName> -o pid,ppid,comm
```

- TTYName は `/dev/ttys001` などのパスではなく `ttys001` の形式で渡すことを想定
- WezTerm の `ListPanes` が返す形式に依存するため、Terminal 実装側で正規化する

### 2. COMM 名 → ToolType マッピング

| COMM 名（完全一致） | ToolType |
|-------------------|----------|
| `claude` | `ToolClaude` |
| `codex` | `ToolCodex` |
| `gemini` | `ToolGemini` |
| それ以外 | スキップ（DetectedProcess に含めない） |

マッピングテーブルは ProcessScanner 内に定数として定義する。

```go
var toolTypeMap = map[string]ToolType{
    "claude": ToolClaude,
    "codex":  ToolCodex,
    "gemini": ToolGemini,
}
```

### 3. DetectedProcess.CWD の正規化

- DefaultScanner が `pane.WorkingDir` をそのまま `CWD` にコピーする
- 正規化（`~` 展開、シンボリックリンク解決等）は Terminal 実装側の責任
- ProcessScanner は CWD を独自に取得しない

---

## エラー処理方針

### スキャン全体を中断するエラー

`ScanResult.Err` に格納し、即 return する。`Processes` は空スライスになる。

| ケース | 対応 |
|--------|------|
| `Terminal.ListPanes()` 失敗 | `ScanResult{Err: err}` を返す |

### ペイン単位でスキップするエラー

当該ペインを警告ログ付きでスキップし、他ペインは正常処理を続ける。

| ケース | 対応 |
|--------|------|
| ps コマンド実行失敗（特定 TTY） | `log.Printf("warn: ...")` でスキップ |
| ps コマンドが非ゼロ終了 | 同上 |

### 行単位でスキップするエラー

ログ出力なし（ノイズになるため）でスキップする。

| ケース | 対応 |
|--------|------|
| ps 出力行のフィールド不足 | 当該行をスキップ |
| PID の int 変換失敗 | 当該行をスキップ |
| COMM が対象外 | 当該行をスキップ（正常ケース） |

### 全ペインでプロセスなし

エラーではない。`ScanResult{Processes: []DetectedProcess{}}` を返す（nil ではなく空スライス）。

### エラー処理の判断基準

```
Terminal 障害（ListPanes 失敗）
  → スキャン継続が不可能 → ScanResult.Err

特定ペインの ps 失敗
  → 当該ペインの情報が欠落するが全体は継続可能 → スキップ + warn ログ

行パースエラー
  → ヘッダ行・非対象プロセスは日常的に発生 → サイレントスキップ
```

---

## ps コマンドのパース詳細

### コマンド

```
ps -t <tty> -o pid,ppid,comm
```

### 出力例

```
  PID  PPID COMM
12345  1000 zsh
12346 12345 claude
12400 12346 node
```

### パースルール

1. **ヘッダ行スキップ**: 1 行目は固定でスキップする
2. **フィールド分割**: `strings.Fields(line)` で空白分割（連続空白・先頭空白を正規化）
3. **フィールド数チェック**: 3 フィールド未満の行はスキップ
4. **PID の変換**: `strconv.Atoi(fields[0])` — 失敗した行はスキップ
5. **COMM の照合**: `fields[2]` を `toolTypeMap` で検索 — ヒットしない場合はスキップ
6. **採用**: ヒットした場合のみ `DetectedProcess{PID: pid, ToolType: toolType}` を生成

### パース実装イメージ

```go
func (s *ProcessScanner) parse(output []byte) []DetectedProcess {
    lines := strings.Split(string(output), "\n")
    var results []DetectedProcess

    for i, line := range lines {
        if i == 0 { // ヘッダスキップ
            continue
        }
        fields := strings.Fields(line)
        if len(fields) < 3 {
            continue
        }
        pid, err := strconv.Atoi(fields[0])
        if err != nil {
            continue
        }
        toolType, ok := toolTypeMap[fields[2]]
        if !ok {
            continue
        }
        results = append(results, DetectedProcess{
            PID:      pid,
            ToolType: toolType,
        })
    }
    return results
}
```

---

## テスト戦略

### DefaultScanner のテスト

Terminal と ProcessScanner の両方をモックする。

```
DefaultScanner テストの構成:

- Terminal モック: 任意の []Pane を返す
- ProcessScanner.execFn モック: 任意の ps 出力文字列を返す

テストケース:
  - 正常: 複数ペインで複数プロセス検出
  - Terminal 失敗: ScanResult.Err が設定される
  - 特定ペインの ps 失敗: 当該ペインがスキップされる
  - 全ペインでプロセスなし: Processes が空スライス
  - PaneID と CWD が正しくコピーされる
```

### ProcessScanner のテスト

`execFn` を差し替えることで外部プロセス実行をモックする。

```go
func TestProcessScanner(t *testing.T) {
    mockExec := func(ctx context.Context, name string, args ...string) ([]byte, error) {
        return []byte(`  PID  PPID COMM
12345  1000 zsh
12346 12345 claude
`), nil
    }
    s := NewProcessScannerWithExec(mockExec)
    procs, err := s.FindAIProcesses(context.Background(), "ttys001")
    // 期待値: [{PID: 12346, ToolType: ToolClaude}]
}
```

テストケース:
- 正常: claude / codex / gemini の各 COMM 名を検出
- ps 実行エラー: error を返す
- フィールド不足行: スキップされる
- PID 変換失敗: スキップされる
- ヘッダ行: スキップされる
- 対象外 COMM（zsh, node 等）: スキップされる

---

## 設計判断の記録

### 1. Scanner が error を返さず ScanResult.Err に格納する理由

**採用**: `Scan(ctx context.Context) ScanResult`（error を戻り値に含めない）

**理由**:
- スキャンは「部分的成功」を自然な状態として扱う必要がある。Terminal は成功したが特定ペインの ps が失敗した場合、成功したペインの結果を捨てるのは非合理的
- `(ScanResult, error)` の2値返しにすると、呼び出し元が `err != nil` のみをチェックして `ScanResult` を捨てるコードを書きやすくなる（取りこぼしリスク）
- `ScanResult.Err` にすることで「エラーを確認しつつ得られた結果も使える」設計になり、呼び出し元の制御が明示的になる

**トレードオフ**:
- Go 慣習（`func f() (T, error)`）から外れるため、初見の開発者には若干なじみにくい
- `ScanResult.Err` を見落とすリスクは残るが、ゼロ値（空 Processes）との組み合わせで気づきやすい

---

### 2. COMM 名の完全一致を選択した理由

**採用**: `toolTypeMap[fields[2]]`（完全一致）

**却下案 A: 前方一致**（例: `strings.HasPrefix(comm, "claude")`）

却下理由:
- `claude-sandbox`、`claude-wrapper` 等のプロセスを誤検出するリスクがある
- Claude Code 内部が起動するヘルパープロセス名が将来変わる可能性がある

**却下案 B: 部分一致**（例: `strings.Contains(comm, "claude")`）

却下理由:
- `include-claude`、`node-claude-server` 等の無関係なプロセスを誤検出する
- 誤検出はステータスバーへの誤表示に直結するため、偽陽性より偽陰性を選ぶ

**完全一致の利点**:
- claude / codex / gemini は CLI として直接起動される場合、COMM 名は実行ファイル名と一致する
- 管理対象のツール名が増えた場合は `toolTypeMap` に追加するだけでよく、ロジック変更が不要

---

### 3. PPID を使わない理由

**判断**: ps 出力に PPID カラムを含めるが、DetectedProcess への変換には使用しない。

**理由**（基本設計での確認事項）:
- claude / codex / gemini は通常、ユーザーがシェルから直接起動する。シェルの子プロセスとして起動されるため、PPID によるフィルタリングは不要
- PPID フィルタリングを追加した場合、ネスト起動（tmux 内の zsh から起動など）で偽陰性が発生するリスクがある
- ツール検出のシンプルさを優先し、PPID は将来の拡張余地として ps 出力には含めておく（フィールドインデックスを安定させるため）

**将来の拡張ポイント**:
- バックグラウンドデーモンとして起動される AI ツールを区別する必要が生じた場合に PPID フィルタリングを追加する

---

### 4. Gemini のベストエフォート対応方針

**課題**: Gemini は CLI ツールとして常に起動されているわけではなく、HTTP API 経由で呼ばれる場合もある。COMM 名 `gemini` で検出できるのはローカル CLI として起動された場合に限る。

**採用方針: ベストエフォート検出**

- COMM 名 `gemini` に完全一致するプロセスを検出対象に含める
- 検出できない場合（API 経由の利用など）はそのまま「未検出」とする
- baton の主目的は Claude Code セッションの監視であり、Gemini の補助的な検出は「あれば表示する」レベルで十分

**将来の検討事項**:
- Gemini CLI が COMM 名を変更した場合は `toolTypeMap` を更新する
- API 経由の Gemini セッションを検出するには別の仕組み（ログファイル監視等）が必要になるが、v2 のスコープ外とする

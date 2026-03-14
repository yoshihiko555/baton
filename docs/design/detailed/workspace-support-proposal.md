# ワークスペース対応 設計変更提案

**作成日**: 2026-03-14
**ステータス**: レビュー待ち
**背景**: WezTerm の運用をワークスペース駆動（プロジェクト別ワークスペース + 複数ワークスペース常時起動）に移行したことに伴い、baton v2 設計にワークスペース対応を組み込む。

---

## 前提

- 1 ワークスペース = 1 プロジェクト（基本 1:1 対応）
- 全ワークスペースを横断監視する
- `wezterm cli list --format json` は全ワークスペースの pane を返す（既にデータは取得可能）
- 現在の設計では `workspace` フィールドを無視している

---

## 変更一覧

| # | 対象ドキュメント | 変更内容 | 影響度 |
|---|----------------|---------|--------|
| 1 | terminal.md | `Pane` 構造体に `Workspace` フィールド追加 | 小 |
| 2 | model.md | `Project` に `Workspace` フィールド追加、グルーピングキー変更 | 中 |
| 3 | state-manager.md | CWD グルーピング → Workspace 優先グルーピングに変更 | 中 |
| 4 | tui.md | ProjectItem にワークスペース名表示 | 小 |
| 5 | exporter.md | `ProjectOutput` に `workspace` フィールド追加 | 小 |
| 6 | lua-plugin.md | JSON 出力にワークスペース情報が含まれることの記載 | 小 |

---

## 変更 #1: terminal.md — Pane 構造体

### 変更前（現在の設計）

```go
type Pane struct {
    ID         int
    Title      string
    TabID      int
    WorkingDir string
    TTYName    string
    IsActive   bool
}
```

### 変更後

```go
type Pane struct {
    ID         int
    Title      string
    TabID      int
    WorkingDir string
    TTYName    string
    IsActive   bool
    Workspace  string  // WezTerm ワークスペース名（例: "baton", "default"）
}
```

### JSON パース変更

```go
var rawPanes []struct {
    ID         int    `json:"pane_id"`
    Title      string `json:"title"`
    TabID      int    `json:"tab_id"`
    WorkingDir string `json:"cwd"`
    TTYName    string `json:"tty_name"`
    IsActive   bool   `json:"is_active"`
    Workspace  string `json:"workspace"`  // 追加
}
```

### v1 との差分サマリ表への追記

| 項目 | v1 | v2 |
|------|----|----|
| `Pane.Workspace` | なし | `string` を追加 |

### フィールド追加理由表への追記

| フィールド | 追加理由 |
|-----------|---------|
| `Workspace` | ワークスペース駆動運用において、プロジェクトのグルーピングキーおよび表示名として使用する |

---

## 変更 #2: model.md — Project 構造体

### 変更前（現在の設計）

```go
// Project のフィールド
Path     string
Name     string
Sessions []Session
```

### 変更後

```go
// Project のフィールド
Path      string
Name      string
Workspace string    // WezTerm ワークスペース名（グルーピングキー）
Sessions  []Session
```

### グルーピングキーの変更

| 観点 | 変更前 | 変更後 |
|------|--------|--------|
| グルーピングキー | CWD（WorkingDir） | Workspace（優先）→ CWD（フォールバック） |
| 表示名（Name） | CWD のベース名 | Workspace 名（優先）→ CWD のベース名（フォールバック） |

### グルーピングロジック

```
1. Pane.Workspace が空でない場合 → Workspace でグルーピング
   - Project.Workspace = Pane.Workspace
   - Project.Name = Pane.Workspace（ワークスペース名を表示名に使用）
   - Project.Path = 最初に検出された CWD（参考情報として保持）
2. Pane.Workspace が空 or "default" の場合 → CWD でグルーピング（v2 既存の動作）
   - Project.Workspace = ""
   - Project.Name = CWD のベース名
   - Project.Path = CWD
```

### 設計判断: "default" ワークスペースの扱い

WezTerm は初期状態で `"default"` ワークスペースを使用する。ワークスペース駆動ではない pane も `"default"` に属する。

- `"default"` は CWD ベースのフォールバックグルーピングに戻す
- 明示的に名前を付けたワークスペースのみ、ワークスペースベースでグルーピング
- これにより、ワークスペースを使わないユーザーの動作に影響しない

---

## 変更 #3: state-manager.md — UpdateFromScan のグルーピング

### 変更前（Step 2）

> `result.Processes` の各 `DetectedProcess` を `CWD` でグループ化し、プロジェクト単位に分類する。

### 変更後（Step 2）

> `result.Processes` の各 `DetectedProcess` を以下のルールでグループ化し、プロジェクト単位に分類する。
>
> 1. `ScanResult.Panes` から PaneID → Workspace のマッピングを構築する
> 2. 各 `DetectedProcess` の PaneID を使って Workspace を解決する
> 3. Workspace が空でなく `"default"` でもない場合 → Workspace でグルーピング
> 4. それ以外 → CWD でグルーピング（既存の動作）

### グルーピングキーの型

```go
type projectKey struct {
    Workspace string // 空の場合は CWD ベース
    CWD       string // Workspace が空の場合のフォールバック
}

func resolveProjectKey(proc DetectedProcess, paneWorkspaceMap map[int]string) projectKey {
    ws := paneWorkspaceMap[proc.PaneID]
    if ws != "" && ws != "default" {
        return projectKey{Workspace: ws}
    }
    return projectKey{CWD: proc.CWD}
}
```

---

## 変更 #4: tui.md — ProjectItem 表示

### 変更前

```
{プロジェクト名}  {N} sessions
```

### 変更後

変更なし。ただし、`Project.Name` の値がワークスペース名になるため、表示内容が改善される。

| ケース | 変更前の表示 | 変更後の表示 |
|--------|------------|------------|
| ワークスペース "baton" | `baton  3 sessions` | `baton  3 sessions`（同じ） |
| ワークスペース "my-project" | `my-project  1 sessions` | `my-project  1 sessions`（同じ） |
| default ワークスペース | `baton  2 sessions`（CWD ベース名） | `baton  2 sessions`（CWD ベース名、変更なし） |

TUI のコード変更は不要。`Project.Name` の設定ロジックが StateManager 側で変わるだけ。

---

## 変更 #5: exporter.md — ProjectOutput DTO

### 変更前

```go
type ProjectOutput struct {
    Name     string          `json:"name"`
    Path     string          `json:"path"`
    Sessions []SessionOutput `json:"sessions"`
}
```

### 変更後

```go
type ProjectOutput struct {
    Name      string          `json:"name"`
    Path      string          `json:"path"`
    Workspace string          `json:"workspace,omitempty"` // 追加
    Sessions  []SessionOutput `json:"sessions"`
}
```

### JSON 出力例

```json
{
  "version": 2,
  "projects": [
    {
      "name": "baton",
      "path": "/Users/foo/baton",
      "workspace": "baton",
      "sessions": [...]
    },
    {
      "name": "other-project",
      "path": "/Users/foo/other",
      "sessions": [...]
    }
  ]
}
```

- ワークスペースが設定されている場合: `"workspace": "baton"` が含まれる
- default/未設定の場合: `omitempty` でフィールド自体が省略される

### FormattedStatus テンプレート変数への追加（オプション）

将来的にワークスペース名をステータスバーに含めたい場合に備え、テンプレート変数に追加可能:

| 変数 | 型 | 説明 |
|------|----|------|
| `.Workspaces` | `[]string` | アクティブなワークスペース名の一覧 |

ただし、初期実装では追加しない（YAGNI）。必要になった時点で追加する。

---

## 変更 #6: lua-plugin.md — 記載追加

JSON 出力スキーマの説明に `workspace` フィールドの記載を追加する。
Lua 側のコード変更は不要（`formatted_status` をそのまま表示する設計のため）。

---

## 影響しないもの

以下のドキュメント/コンポーネントは変更不要:

| コンポーネント | 理由 |
|--------------|------|
| scanner.md | プロセス検出ロジックは変更なし。Pane 情報は Terminal 層から取得済み |
| state-resolver.md | JSONL 解析・状態判定にワークスペースは無関係 |
| parser.md | JSONL パーサーは変更なし |
| migration.md | マイグレーション計画への影響なし（v2 新機能として追加） |
| config.md | 設定ファイルへの変更は初期実装では不要 |

---

## リスクと注意点

### 1. "default" ワークスペースの判定

WezTerm のバージョンによって `workspace` フィールドの値が異なる可能性がある。
空文字・"default"・nil のいずれかを返す場合を想定し、フォールバックロジックで吸収する。

### 2. 1ワークスペース内に複数CWDがある場合

基本 1:1 だが、例外的に 1 ワークスペース内に異なる CWD の pane がある場合:
- 同一プロジェクトとしてグルーピングされる（ワークスペース優先のため）
- `Project.Path` は最初に検出された CWD が設定される
- 各セッションの `WorkingDir` は個別に正確な値を保持する

### 3. 後方互換性

- v1 ユーザー（ワークスペースを使っていない場合）: "default" ワークスペースとなり、CWD ベースのフォールバックが動作するため影響なし
- JSON スキーマ: `workspace` フィールドは `omitempty` のため、未設定時は出力されない

# 詳細設計: TUI (v2)

## 概要

`internal/tui/` が担う Bubble Tea ベースのターミナル UI の v2 仕様。
主な変更点は **ポーリング駆動への一本化**・**SessionItem/ProjectItem の表示強化**・**Waiting 状態追加**・**サブメニュー** の 4 点。

> 補足（2026-03-24）:
> 現行実装ではこの文書の初版以降に `a/d/A/D` 承認操作と `/` セッションフィルタが追加されています。
> 最新の操作一覧は `README.md` / `docs/README.ja.md` を正として参照してください。

---

## v1 との差分サマリ

| 項目 | v1 | v2 |
|------|----|----|
| 更新駆動 | Ticker + Watcher チャネル の二重駆動 | Ticker のみ (ポーリング) |
| Model 依存 | stateReader, stateWriter, watcher, terminal, config | scanner, stateUpdater, stateReader, terminal, config |
| 中間 Msg 型 | StateUpdateMsg([]Project), WatchEventMsg | ScanResultMsg |
| SessionItem 1行目 | セッション ID | `{アイコン} {ツール種別}  {状態}  {ブランチ}` |
| SessionItem 2行目 | 状態 / 最終活動時刻 | `    {ツール名}  \|  {tokens} tokens` |
| ProjectItem | `{name}` / `sessions:N / active:N` | `{name}  N sessions` |
| 色定義 | Idle/Thinking/ToolUse/Error の4色 | Waiting(208) 追加、ToolUse を 43 に変更 |
| セッションソート | なし (登録順) | Waiting > Error > Thinking > ToolUse > Idle |
| ステータスバー | `Projects:N \| Active:N \| Last update:HH:MM:SS` | `N sessions \| N active \| N waiting    q:quit enter:jump` |
| サブメニュー | なし | Ambiguous セッションで Enter → ペイン選択メニュー |
| FocusPane 引数 | string | int |

---

## Model (v2)

```go
// Model は TUI 全体を表す Bubble Tea のルートモデル。
type Model struct {
    projectList list.Model
    sessionList list.Model

    scanner      core.Scanner       // doScan() を持つインターフェース
    stateUpdater core.StateUpdater  // スキャン結果の永続化
    stateReader  core.StateReader   // 最新状態の読み取り（Projects/Summary/Panes）

    terminal terminal.Terminal
    config   config.Config

    // 最新スキャン結果
    latestProjects []core.Project
    latestSummary  core.Summary
    latestPanes    []terminal.Pane

    // レイアウト
    activePane      int
    width           int
    height          int
    err             error
    selectedProject string

    // サブメニュー
    showSubMenu    bool
    subMenuItems   []SubMenuItem
    subMenuCursor  int
}

// SubMenuItem はサブメニューの1行（ペイン候補）を表す。
type SubMenuItem struct {
    PaneID  int
    TTYName string
}
```

### 削除された依存

| 削除 | 理由 |
|------|------|
| `stateWriter core.StateWriter` | Watcher イベント処理が不要になる |
| `watcher core.EventSource` | ポーリング方式に統一するため |

---

## Msg 型 (v2)

```go
// TickMsg は定期リフレッシュタイマー発火時に送られる。
type TickMsg struct{}

// ScanResultMsg はスキャン完了時のスナップショットを運ぶ。
type ScanResultMsg struct {
    Projects []core.Project
    Summary  core.Summary
    Panes    []terminal.Pane
}

// ErrMsg は非同期コマンドで発生したエラーを運ぶ。
type ErrMsg error
```

`WatchEventMsg` / `StateUpdateMsg` は削除する。

---

## Init (v2)

```go
func (m Model) Init() tea.Cmd {
    // Watcher リスナーを削除し、Ticker のみを起動する。
    return tickCmd(m.config.ScanInterval)
}
```

---

## Update (v2) — 駆動フロー

```
TickMsg
  └─ doScanCmd(ctx, m.scanner, m.stateUpdater, m.stateReader)
        └─ scanner.Scan(ctx) → ScanResult
        └─ stateUpdater.UpdateFromScan(result)
        └─ stateReader.Projects() / Summary() / Panes()
        └─ ScanResultMsg{Projects, Summary, Panes}
              └─ updateProjectList() / updateSessionList()
              └─ tickCmd(m.config.ScanInterval) で次 tick を予約
```

### doScanCmd

```go
// doScanCmd は Scanner.Scan → StateManager.UpdateFromScan を実行し、
// 結果を ScanResultMsg として返す tea.Cmd。
// Scanner は常に ScanResult を返す（エラーは ScanResult.Err に格納）。
func doScanCmd(
    ctx context.Context,
    scanner core.Scanner,
    sm core.StateUpdater,
    sr core.StateReader,
) tea.Cmd {
    return func() tea.Msg {
        result := scanner.Scan(ctx)
        if err := sm.UpdateFromScan(result); err != nil {
            return ErrMsg(err)
        }
        return ScanResultMsg{
            Projects: sr.Projects(),
            Summary:  sr.Summary(),
            Panes:    sr.Panes(),
        }
    }
}
```

---

## キー入力ハンドリング (v2)

### サブメニューが開いているとき

| キー | 動作 |
|------|------|
| `j` / `↓` | サブメニュー選択を下へ |
| `k` / `↑` | サブメニュー選択を上へ |
| `enter` | 選択ペインに FocusPane → `exitOnJump` が true なら終了、false なら TUI に戻る（同期実行） |
| `esc` | サブメニューを閉じる |

### 通常モード (session ペインで Enter)

```go
case key.Matches(msg, enterKey):
    if m.activePane == 1 {
        selected, ok := m.sessionList.SelectedItem().(SessionItem)
        if !ok {
            return m, nil
        }
        if selected.Session.Ambiguous {
            // Ambiguous → サブメニューを開く
            m.showSubMenu = true
            m.subMenuItems = buildSubMenuItems(selected.Session.CandidatePaneIDs, m.latestPanes)
            m.subMenuCursor = 0
            return m, nil
        }
        // 通常 → 直接 FocusPane
        if err := m.terminal.FocusPane(selected.Session.PaneID); err != nil {
            m.err = err
        }
    }
```

### buildSubMenuItems

```go
func buildSubMenuItems(candidateIDs []int, panes []terminal.Pane) []SubMenuItem {
    paneMap := make(map[int]terminal.Pane, len(panes))
    for _, p := range panes {
        paneMap[p.ID] = p
    }

    items := make([]SubMenuItem, 0, len(candidateIDs))
    for _, id := range candidateIDs {
        item := SubMenuItem{PaneID: id, TTYName: fmt.Sprintf("pane %d", id)}
        if p, ok := paneMap[id]; ok && p.TTYName != "" {
            item.TTYName = p.TTYName
        }
        items = append(items, item)
    }
    return items
}
```

---

## SessionItem 表示 (v2)

`list.DefaultDelegate` は1行タイトル + 1行説明の2行構成をサポートしている。
v2 では `Title()` / `Description()` を以下のように変更する。

### Title()

```
{アイコン} {ツール種別}  {状態}  {ブランチ名}
```

- アイコン: `stateColors` に対応した色付き文字 (`●`)
- Ambiguous の場合は先頭に `~` を付ける: `~ ● Bash  ToolUse  feature/v2`
- ブランチ名がない場合はブランチ部分を省略

例:

```
● Bash  ToolUse  feature/v2
~ ● Edit  ToolUse  main
  ● Thinking
```

### Description()

```
    {ツール名}  |  {tokenCount} tokens
```

- ツール名・トークン数がない場合は空文字

例:

```
    go test ./...  |  12500 tokens
```

### 実装

```go
func (i SessionItem) Title() string {
    icon := "●"
    prefix := "  "
    if i.Session.Ambiguous {
        prefix = "~ "
    }

    parts := []string{prefix + stateStyle(i.Session.State).Render(icon), i.Session.Tool.String()}
    parts = append(parts, i.Session.State.String())
    if i.Session.Branch != "" {
        parts = append(parts, i.Session.Branch)
    }
    return strings.Join(parts, "  ")
}

func (i SessionItem) Description() string {
    if i.Session.CurrentTool == "" && i.Session.InputTokens == 0 {
        return ""
    }
    return fmt.Sprintf("    %s  |  %d tokens", i.Session.CurrentTool, i.Session.InputTokens)
}
```

---

## ProjectItem 表示 (v2)

### Title()

```
{プロジェクト名}  {N} sessions
```

例:

```
baton  3 sessions
```

### 実装

```go
func (i ProjectItem) Title() string {
    name := i.Project.Name
    if name == "" {
        name = i.Project.Path
    }
    return fmt.Sprintf("%s  %d sessions", name, len(i.Project.Sessions))
}

func (i ProjectItem) Description() string {
    return fmt.Sprintf("sessions: %d", len(i.Project.Sessions))
}
```

---

## 色定義 (v2)

```go
var stateColors = map[core.SessionState]lipgloss.Color{
    core.Idle:     lipgloss.Color("240"), // グレー (変更なし)
    core.Thinking: lipgloss.Color("220"), // 黄色 (変更なし)
    core.ToolUse:  lipgloss.Color("43"),  // シアン (v1: 82 → v2: 43)
    core.Waiting:  lipgloss.Color("208"), // オレンジ (新規追加)
    core.Error:    lipgloss.Color("196"), // 赤 (変更なし)
}
```

| 状態 | v1 | v2 | 変更理由 |
|------|----|----|----|
| ToolUse | `82` (緑) | `43` (シアン) | Thinking との視覚的区別を明確にする |
| Waiting | — | `208` (オレンジ) | 新状態。注意を促す中間色 |

---

## セッションソート (v2)

`updateSessionList()` 内で `sort.Slice` を使い以下の優先度でソート：

```go
priority := map[core.SessionState]int{
    core.Waiting:  0,
    core.Error:    1,
    core.Thinking: 2,
    core.ToolUse:  3,
    core.Idle:     4,
}
```

同一優先度内は `LastActivity` の降順 (最新を上位) とする。

---

## ステータスバー (v2)

```
{total} sessions | {active} active | {waiting} waiting    q:quit enter:jump
```

例:

```
3 sessions | 2 active | 1 waiting    q:quit enter:jump
```

### renderStatusBar の変更

```go
func (m Model) renderStatusBar(totalWidth int) string {
    s := m.latestSummary
    status := fmt.Sprintf(
        "%d sessions | %d active | %d waiting    q:quit enter:jump",
        s.TotalSessions, s.Active, s.Waiting,
    )
    return statusBarStyle.Width(max(1, totalWidth)).Render(status)
}
```

v1 の `Last update: HH:MM:SS` は削除する。ポーリング方式ではリフレッシュ間隔が固定のため表示する意義が薄い。

---

## サブメニュー描画 (v2)

### 実装方式

**自前描画**を採用する。bubbles/list の別インスタンスを使うと、
既存の `sessionList` とのキーイベント衝突が複雑になるため。

### View() でのサブメニュー表示

```go
if m.showSubMenu {
    return lipgloss.JoinVertical(lipgloss.Left, panes, m.renderSubMenu(), statusBar)
}
```

### renderSubMenu()

```go
func (m Model) renderSubMenu() string {
    lines := []string{"Select pane:"}
    for i, item := range m.subMenuItems {
        cursor := "  "
        if i == m.subMenuCursor {
            cursor = "> "
        }
        lines = append(lines, fmt.Sprintf("%s[%d] %s", cursor, item.PaneID, item.TTYName))
    }
    lines = append(lines, "  esc: cancel")

    style := lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color("208")).
        Padding(0, 1)

    return style.Render(strings.Join(lines, "\n"))
}
```

---

## ファイル配置

```
internal/tui/
  model.go    # Model 定義, Init, SubMenuItem, tickCmd, doScanCmd
  update.go   # Update, キーハンドリング, buildSubMenuItems, ソートロジック
  view.go     # View, renderStatusBar, renderSubMenu, stateColors
```

---

## 設計判断

### 1. Watcher 依存削除の理由

**問題**: v1 では Ticker + Watcher チャネルの二重駆動が存在し、
`StateUpdateMsg` の後に必ず `listenWatcherCmd` を再サブスクライブする必要があった。
チャネルブロックのバグが発生しやすく、実際に修正履歴にも残っている。

**判断**: ポーリング (Ticker → doScan) に一本化する。
ファイル変更を即時反映する必要性はなく、数秒のポーリング遅延は許容範囲。
Model から `stateWriter` と `watcher` を除去することで依存グラフが単純になる。

### 2. bubbles/list の delegate カスタマイズ要否

**結論**: カスタム delegate は不要。`list.DefaultDelegate` の `Title()` / `Description()` で2行表示が実現できる。

`DefaultDelegate` は `list.Item` インターフェースの `Title()` と `Description()` を呼ぶ。
SessionItem の両メソッドを変更するだけで表示が変わる。
カスタム delegate が必要になるのは行高を3行以上にしたい場合や、カーソル装飾を完全に置き換えたい場合に限る。

### 3. サブメニューの実装方式: 別 list.Model vs 自前描画

**結論**: 自前描画を採用する。

- **別 list.Model**: キーイベントを `sessionList` と `subMenuList` で切り替える必要があり、
  `Update()` の分岐が複雑になる。bubbles/list のフィルタ機能も不要。
- **自前描画**: 候補数が少ない (通常 2〜5件) ため、シンプルな `[]SubMenuItem` + `cursor int` で十分。
  `renderSubMenu()` で描画し、`Update()` で `subMenuCursor` を動かすだけで実装できる。

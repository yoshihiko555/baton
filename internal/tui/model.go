package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yoshihiko555/baton/internal/config"
	"github.com/yoshihiko555/baton/internal/core"
	"github.com/yoshihiko555/baton/internal/terminal"
)

// TickMsg は定期リフレッシュタイマー発火時に送られる。
type TickMsg struct{}

// ScanResultMsg はスキャン完了時のスナップショットを運ぶ。
type ScanResultMsg struct {
	Projects []core.Project
	Summary  core.Summary
	Panes    []terminal.Pane
}

// PreviewResultMsg はプレビューテキスト取得完了時に送られる。
type PreviewResultMsg struct {
	Text string
	Err  error
}

// ErrMsg は非同期コマンドで発生したエラーを運ぶ。
type ErrMsg error

// JumpDoneMsg はペインジャンプ完了を通知する。
type JumpDoneMsg struct{ Err error }

// SubMenuItem はサブメニューの1行（ペイン候補）を表す。
type SubMenuItem struct {
	PaneID  string
	TTYName string
}

// sessionEntry はセッションリストの1エントリ。ヘッダーまたはセッション行。
type sessionEntry struct {
	isHeader bool
	header   string
	icon     string
	state    core.SessionState
	session  *core.Session
	project  *core.Project
}

// Model は TUI 全体を表す Bubble Tea のルートモデル。
type Model struct {
	entries []sessionEntry // グループ化されたセッションリスト
	cursor  int           // 現在のカーソル位置（エントリ index）

	scanner      core.Scanner
	stateUpdater core.StateUpdater
	stateReader  core.StateReader
	terminal     terminal.Terminal
	config       config.Config

	latestProjects []core.Project
	latestSummary  core.Summary
	latestPanes    []terminal.Pane

	activePane int // 0=sessions, 1=preview
	width      int
	height     int
	err        error

	previewText     string
	previewPaneID   string // 現在プレビュー中の PaneID
	previewLoading  bool

	showSubMenu   bool
	subMenuItems  []SubMenuItem
	subMenuCursor int

	jumping bool
}

// NewModel はデフォルト設定で TUI モデルを初期化する。
func NewModel(
	scanner core.Scanner,
	stateUpdater core.StateUpdater,
	stateReader core.StateReader,
	term terminal.Terminal,
	cfg config.Config,
) Model {
	return Model{
		scanner:      scanner,
		stateUpdater: stateUpdater,
		stateReader:  stateReader,
		terminal:     term,
		config:       cfg,
	}
}

// Init は tea.Model の初期コマンドを返す。
func (m Model) Init() tea.Cmd {
	return tickCmd(m.config.ScanInterval)
}

func tickCmd(interval time.Duration) tea.Cmd {
	if interval <= 0 {
		interval = time.Second
	}
	return func() tea.Msg {
		<-time.After(interval)
		return TickMsg{}
	}
}

// doScanCmd は Scanner.Scan → StateUpdater.UpdateFromScan を実行し、
// 結果を ScanResultMsg として返す tea.Cmd。
func doScanCmd(
	ctx context.Context,
	scanner core.Scanner,
	sm core.StateUpdater,
	sr core.StateReader,
	term terminal.Terminal,
) tea.Cmd {
	return func() tea.Msg {
		result := scanner.Scan(ctx)
		if err := sm.UpdateFromScan(result); err != nil {
			return ErrMsg(err)
		}
		sm.RefineToolUseState(term)
		return ScanResultMsg{
			Projects: sr.Projects(),
			Summary:  sr.Summary(),
			Panes:    sr.Panes(),
		}
	}
}

// fetchPreviewCmd は指定ペインのテキストを非同期で取得する。
func fetchPreviewCmd(term terminal.Terminal, paneID string) tea.Cmd {
	return func() tea.Msg {
		text, err := term.GetPaneText(paneID)
		return PreviewResultMsg{Text: text, Err: err}
	}
}

// selectedSession はカーソル位置のセッションを返す。
func (m Model) selectedSession() *sessionEntry {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return nil
	}
	e := &m.entries[m.cursor]
	if e.isHeader {
		return nil
	}
	return e
}

// buildEntries はプロジェクト一覧からグループ化されたエントリリストを構築する。
func buildEntries(projects []core.Project) []sessionEntry {
	// 全セッションをフラットに集める
	type sessionWithProject struct {
		session *core.Session
		project *core.Project
	}
	var all []sessionWithProject
	for i := range projects {
		p := &projects[i]
		for _, s := range p.Sessions {
			if s != nil {
				all = append(all, sessionWithProject{session: s, project: p})
			}
		}
	}

	// 状態グループ別に分類
	// WORKING グループは Thinking + ToolUse をまとめる
	type groupDef struct {
		icon    string
		label   string
		state   core.SessionState
		entries []sessionWithProject
	}

	groups := []groupDef{
		{icon: "!", label: "WAITING", state: core.Waiting},
		{icon: "x", label: "ERROR", state: core.Error},
		{icon: "*", label: "WORKING", state: core.Thinking}, // Thinking + ToolUse
		{icon: "~", label: "IDLE", state: core.Idle},
	}

	for _, sp := range all {
		switch sp.session.State {
		case core.Waiting:
			groups[0].entries = append(groups[0].entries, sp)
		case core.Error:
			groups[1].entries = append(groups[1].entries, sp)
		case core.Thinking, core.ToolUse:
			groups[2].entries = append(groups[2].entries, sp)
		case core.Idle:
			groups[3].entries = append(groups[3].entries, sp)
		default:
			groups[3].entries = append(groups[3].entries, sp) // unknown → idle
		}
	}

	var entries []sessionEntry
	for _, g := range groups {
		if len(g.entries) == 0 {
			continue
		}
		// PID でソート
		sort.SliceStable(g.entries, func(i, j int) bool {
			return g.entries[i].session.PID < g.entries[j].session.PID
		})
		// グループヘッダー
		entries = append(entries, sessionEntry{
			isHeader: true,
			header:   g.label,
			icon:     g.icon,
			state:    g.state,
		})
		// セッション行
		for _, sp := range g.entries {
			entries = append(entries, sessionEntry{
				session: sp.session,
				project: sp.project,
				state:   sp.session.State,
			})
		}
	}

	return entries
}

// sessionDisplayName はセッション行の表示名を返す。
func sessionDisplayName(e *sessionEntry) string {
	if e.project == nil || e.session == nil {
		return "?"
	}
	name := e.project.Name
	if name == "" {
		// パスの最後のセグメントを使う
		parts := strings.Split(e.project.Path, "/")
		name = parts[len(parts)-1]
	}
	return name
}

// sessionDetailLine はセッション行の詳細情報を返す。
func sessionDetailLine(e *sessionEntry) string {
	if e.session == nil {
		return ""
	}
	s := e.session
	var parts []string
	if s.Branch != "" {
		parts = append(parts, s.Branch)
	}
	parts = append(parts, fmt.Sprintf("[%s]", s.State))
	if s.CurrentTool != "" {
		parts = append(parts, s.CurrentTool)
	}
	return strings.Join(parts, "  ")
}

// buildSubMenuItems は候補ペイン ID リストからサブメニュー項目を生成する。
func buildSubMenuItems(candidateIDs []string, panes []terminal.Pane) []SubMenuItem {
	paneMap := make(map[string]terminal.Pane, len(panes))
	for _, p := range panes {
		paneMap[p.ID] = p
	}
	items := make([]SubMenuItem, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		item := SubMenuItem{PaneID: id, TTYName: fmt.Sprintf("pane %s", id)}
		if p, ok := paneMap[id]; ok && p.TTYName != "" {
			item.TTYName = p.TTYName
		}
		items = append(items, item)
	}
	return items
}

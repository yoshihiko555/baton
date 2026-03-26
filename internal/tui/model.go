package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/yoshihiko555/baton/internal/config"
	"github.com/yoshihiko555/baton/internal/core"
	"github.com/yoshihiko555/baton/internal/terminal"
)

// inputMode はテキスト入力モードの種別を表す。
type inputMode int

const (
	inputNone    inputMode = iota // 通常モード
	inputApprove                  // プロンプト付き承認（A）
	inputDeny                     // プロンプト付き拒否（D）
)

// ApprovalResultMsg は承認/拒否操作の完了を通知する。
type ApprovalResultMsg struct {
	Err   error
	Label string // 操作の表示名（例: "Approved", "Denied: fix tests"）
	PaneID string // 承認/拒否を送信した対象ペイン
}

// FlashClearMsg はフラッシュメッセージの消去タイマー発火時に送られる。
type FlashClearMsg struct {
	Generation uint64
}

// flashDuration はフラッシュメッセージの表示時間。
const flashDuration = 5 * time.Second

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
	PaneID string
	Text   string
	Err    error
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

// sessionFilter はセッション一覧のフィルタ条件を表す。
type sessionFilter struct {
	textTokens    []string
	includeStates []string
	excludeStates []string
}

// Model は TUI 全体を表す Bubble Tea のルートモデル。
type Model struct {
	entries []sessionEntry // グループ化されたセッションリスト
	cursor  int            // 現在のカーソル位置（エントリ index）

	scanner      core.Scanner
	stateUpdater core.StateUpdater
	stateReader  core.StateReader
	terminal     terminal.Terminal
	config       config.Config
	theme        Theme

	latestProjects []core.Project
	latestSummary  core.Summary
	latestPanes    []terminal.Pane

	width int
	height     int
	err        error
	scanErr    error // スキャン由来のエラー（ScanResultMsg でのみクリア対象）

	previewText    string
	previewPaneID  string // 現在プレビュー中の PaneID
	previewLoading bool

	showSubMenu   bool
	subMenuItems  []SubMenuItem
	subMenuCursor int

	filtering   bool
	filterQuery string
	filterInput textinput.Model

	jumping    bool
	exitOnJump bool

	// 承認/拒否操作
	inputMode    inputMode
	textInput    textinput.Model
	flashMessage string // 操作結果の一時表示メッセージ
	flashGen     uint64 // フラッシュ消去タイマーの世代番号
}

// NewModel はデフォルト設定で TUI モデルを初期化する。
func NewModel(
	scanner core.Scanner,
	stateUpdater core.StateUpdater,
	stateReader core.StateReader,
	term terminal.Terminal,
	cfg config.Config,
	exitOnJump bool,
) Model {
	ti := textinput.New()
	ti.CharLimit = 500
	fti := textinput.New()
	fti.CharLimit = 300
	fti.Placeholder = "name/path/tool/status..."
	return Model{
		scanner:      scanner,
		stateUpdater: stateUpdater,
		stateReader:  stateReader,
		terminal:     term,
		config:       cfg,
		theme:        ResolveTheme(cfg.Theme),
		exitOnJump:   exitOnJump,
		textInput: ti,
		filterInput:  fti,
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
		return PreviewResultMsg{PaneID: paneID, Text: text, Err: err}
	}
}

// fetchPreviewDelayedCmd は指定遅延後にプレビューを取得する。
func fetchPreviewDelayedCmd(term terminal.Terminal, paneID string, delay time.Duration) tea.Cmd {
	return func() tea.Msg {
		if delay > 0 {
			<-time.After(delay)
		}
		text, err := term.GetPaneText(paneID)
		return PreviewResultMsg{PaneID: paneID, Text: text, Err: err}
	}
}

// canApprove は選択中のセッションが承認/拒否の送信可能かを返す。
// 条件: Waiting 状態、Claude Code セッション。
func (m Model) canApprove() bool {
	sel := m.selectedSession()
	if sel == nil || sel.session == nil {
		return false
	}
	return sel.session.State == core.Waiting && sel.session.Tool == core.ToolClaude && sel.session.PaneID != ""
}

// canInput は選択中のセッションがプロンプト入力モードに入れるかを返す。
// 条件: Claude Code セッション（Waiting でなくても可）。
func (m Model) canInput() bool {
	sel := m.selectedSession()
	if sel == nil || sel.session == nil {
		return false
	}
	return sel.session.Tool == core.ToolClaude && sel.session.PaneID != ""
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
	return buildEntriesWithFilter(projects, sessionFilter{})
}

// buildEntriesWithFilter はフィルタ適用後のグループ化エントリを構築する。
func buildEntriesWithFilter(projects []core.Project, filter sessionFilter) []sessionEntry {
	// 全セッションをフラットに集める
	type sessionWithProject struct {
		session *core.Session
		project *core.Project
	}
	var all []sessionWithProject
	for i := range projects {
		p := &projects[i]
		for _, s := range p.Sessions {
			if s != nil && sessionMatchesFilter(s, p, filter) {
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

func parseSessionFilter(query string) sessionFilter {
	filter := sessionFilter{}
	for _, raw := range strings.Fields(strings.ToLower(strings.TrimSpace(query))) {
		negate := strings.HasPrefix(raw, "!")
		token := strings.TrimPrefix(raw, "!")
		if token == "" {
			continue
		}
		normalized := normalizeStateToken(token)
		if normalized != "" {
			if negate {
				filter.excludeStates = append(filter.excludeStates, normalized)
			} else {
				filter.includeStates = append(filter.includeStates, normalized)
			}
			continue
		}
		filter.textTokens = append(filter.textTokens, token)
	}
	return filter
}

func normalizeStateToken(token string) string {
	switch token {
	case "idle":
		return "idle"
	case "thinking":
		return "thinking"
	case "tool_use", "tooluse", "tool":
		return "tool_use"
	case "working":
		return "working"
	case "waiting":
		return "waiting"
	case "error":
		return "error"
	default:
		return ""
	}
}

func stateMatchesToken(state core.SessionState, token string) bool {
	switch token {
	case "idle":
		return state == core.Idle
	case "thinking":
		return state == core.Thinking
	case "tool_use":
		return state == core.ToolUse
	case "working":
		return state == core.Thinking || state == core.ToolUse
	case "waiting":
		return state == core.Waiting
	case "error":
		return state == core.Error
	default:
		return false
	}
}

func sessionMatchesFilter(session *core.Session, project *core.Project, filter sessionFilter) bool {
	if session == nil {
		return false
	}

	if len(filter.includeStates) > 0 {
		matched := false
		for _, stateToken := range filter.includeStates {
			if stateMatchesToken(session.State, stateToken) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, stateToken := range filter.excludeStates {
		if stateMatchesToken(session.State, stateToken) {
			return false
		}
	}

	if len(filter.textTokens) == 0 {
		return true
	}

	entry := sessionEntry{session: session, project: project}
	searchTargets := []string{
		sessionDisplayName(&entry),
		session.Tool.String(),
		session.WorkingDir,
	}
	if project != nil {
		searchTargets = append(searchTargets, project.Path, project.Name)
	}
	haystack := strings.ToLower(strings.Join(searchTargets, " "))
	for _, token := range filter.textTokens {
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
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

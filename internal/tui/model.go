package tui

import (
	"context"
	"fmt"
	"path/filepath"
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
	Err    error
	Label  string // 操作の表示名（例: "Approved", "Denied: fix tests"）
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
	Seq    uint64 // 発行時点の previewFetchSeq
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

	width   int
	height  int
	err     error
	scanErr error // スキャン由来のエラー（ScanResultMsg でのみクリア対象）

	previewText      string
	previewPaneID    string // 現在プレビュー中の PaneID
	previewLoading   bool
	previewFetchSeq  uint64    // preview fetch 発行ごとにインクリメント。stale 結果検出に使用。
	previewUpdatedAt time.Time // 最新 PreviewResultMsg 受信時刻。ヘッダに表示して silent refresh の視覚フィードバックを提供。

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

	// 自動承認モード: PaneID → true で自動承認 ON
	autoApprove map[string]bool
	// autoApproved は既に自動承認 Enter を送信済みの PaneID を記録する。
	// セッションが Waiting から離脱したらエントリを削除する。
	autoApproved map[string]bool
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
		textInput:    ti,
		filterInput:  fti,
		autoApprove:  make(map[string]bool),
		autoApproved: make(map[string]bool),
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
func fetchPreviewCmd(term terminal.Terminal, paneID string, seq uint64) tea.Cmd {
	return func() tea.Msg {
		text, err := term.GetPaneText(paneID)
		return PreviewResultMsg{PaneID: paneID, Text: text, Err: err, Seq: seq}
	}
}

// fetchPreviewDelayedCmd は指定遅延後にプレビューを取得する。
func fetchPreviewDelayedCmd(term terminal.Terminal, paneID string, seq uint64, delay time.Duration) tea.Cmd {
	return func() tea.Msg {
		if delay > 0 {
			<-time.After(delay)
		}
		text, err := term.GetPaneText(paneID)
		return PreviewResultMsg{PaneID: paneID, Text: text, Err: err, Seq: seq}
	}
}

// PreviewFetchSeq は現在の previewFetchSeq を返す（テスト用）。
func (m Model) PreviewFetchSeq() uint64 {
	return m.previewFetchSeq
}

// canApprove は選択中のセッションが手動承認/拒否の送信可能かを返す。
// 条件: Waiting 状態、Claude Code または Codex セッション、自動承認 OFF。
func (m Model) canApprove() bool {
	sel := m.selectedSession()
	if sel == nil || sel.session == nil {
		return false
	}
	return sel.session.State == core.Waiting && (sel.session.Tool == core.ToolClaude || sel.session.Tool == core.ToolCodex) && sel.session.PaneID != ""
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

	sort.SliceStable(all, func(i, j int) bool {
		pi := projectSortKey(all[i].project)
		pj := projectSortKey(all[j].project)
		if pi != pj {
			return pi < pj
		}

		ti := all[i].session.Tool.String()
		tj := all[j].session.Tool.String()
		if ti != tj {
			return ti < tj
		}

		if all[i].session.PID != all[j].session.PID {
			return all[i].session.PID < all[j].session.PID
		}

		return all[i].session.PaneID < all[j].session.PaneID
	})

	var (
		entries         []sessionEntry
		currentProjSort string
	)
	for _, sp := range all {
		projectKey := projectSortKey(sp.project)
		if projectKey != currentProjSort {
			currentProjSort = projectKey
			entries = append(entries, sessionEntry{
				isHeader: true,
				header:   projectDisplayName(sp.project),
				project:  sp.project,
			})
		}
		entries = append(entries, sessionEntry{
			session: sp.session,
			project: sp.project,
			state:   sp.session.State,
		})
	}

	return entries
}

func projectDisplayName(project *core.Project) string {
	if project == nil {
		return "?"
	}
	if strings.TrimSpace(project.Name) != "" {
		return project.Name
	}
	if strings.TrimSpace(project.Path) != "" {
		return filepath.Base(project.Path)
	}
	return "?"
}

func projectSortKey(project *core.Project) string {
	if project == nil {
		return ""
	}
	name := strings.ToLower(projectDisplayName(project))
	path := strings.ToLower(strings.TrimSpace(project.Path))
	return name + "\x00" + path
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
		sessionPrimaryLabel(&entry),
		session.Tool.String(),
		displayStateLabel(session.State),
		session.WorkingDir,
		session.Branch,
		session.CurrentTool,
		session.FirstPrompt,
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
	return projectDisplayName(e.project)
}

func sessionPrimaryLabel(e *sessionEntry) string {
	if e == nil || e.session == nil {
		return "?"
	}

	s := e.session
	switch {
	case strings.TrimSpace(s.Branch) != "":
		return s.Branch
	case strings.TrimSpace(s.FirstPrompt) != "":
		return strings.TrimSpace(s.FirstPrompt)
	case strings.TrimSpace(s.CurrentTool) != "":
		return s.CurrentTool
	case strings.TrimSpace(s.PaneID) != "":
		return fmt.Sprintf("pane %s", s.PaneID)
	default:
		return fmt.Sprintf("session %d", s.PID)
	}
}

func sessionListLabel(e *sessionEntry) string {
	project := sessionDisplayName(e)
	primary := sessionPrimaryLabel(e)
	switch {
	case project == "?" || project == "":
		return primary
	case primary == "?" || primary == "":
		return project
	default:
		return fmt.Sprintf("%s / %s", project, primary)
	}
}

func displayStateLabel(state core.SessionState) string {
	switch state {
	case core.Waiting:
		return "waiting"
	case core.Error:
		return "error"
	case core.Thinking, core.ToolUse:
		return "working"
	case core.Idle:
		return "idle"
	default:
		return state.String()
	}
}

// sessionDetailLine はセッション行の詳細情報を返す。
func sessionDetailLine(e *sessionEntry) string {
	if e.session == nil {
		return ""
	}
	s := e.session
	parts := []string{
		fmt.Sprintf("[%s]", displayStateLabel(s.State)),
		fmt.Sprintf("PID:%d", s.PID),
	}
	if s.CurrentTool != "" && s.CurrentTool != sessionPrimaryLabel(e) {
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

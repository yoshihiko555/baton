package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yoshihiko555/baton/internal/config"
	"github.com/yoshihiko555/baton/internal/core"
	"github.com/yoshihiko555/baton/internal/terminal"
)

const (
	defaultListWidth  = 32
	defaultListHeight = 16
)

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

// JumpDoneMsg はペインジャンプ完了を通知する。
type JumpDoneMsg struct{ Err error }

// SubMenuItem はサブメニューの1行（ペイン候補）を表す。
type SubMenuItem struct {
	PaneID  int
	TTYName string
}

// ProjectItem はプロジェクト一覧の1行を表す。
type ProjectItem struct {
	Project core.Project
}

// Title はプロジェクト行のタイトル文字列を返す。
func (i ProjectItem) Title() string {
	name := i.Project.Name
	if name == "" {
		name = i.Project.Path
	}
	return fmt.Sprintf("%s  %d sessions", name, len(i.Project.Sessions))
}

// Description はプロジェクト行の補足説明文字列を返す。
// 各状態のセッション数を表示する（例: "thinking: 3 · idle: 1 · waiting: 1"）。
func (i ProjectItem) Description() string {
	counts := make(map[core.SessionState]int)
	for _, s := range i.Project.Sessions {
		if s != nil {
			counts[s.State]++
		}
	}

	// 優先度順に表示する（重要な状態が先）
	order := []core.SessionState{core.Waiting, core.Error, core.Thinking, core.ToolUse, core.Idle}
	var parts []string
	for _, state := range order {
		if n, ok := counts[state]; ok && n > 0 {
			parts = append(parts, fmt.Sprintf("%s: %d", state, n))
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// FilterValue はプロジェクト行の検索対象文字列を返す。
func (i ProjectItem) FilterValue() string {
	return strings.Join([]string{i.Project.Name, i.Project.Path}, " ")
}

// SessionItem はセッション一覧の1行を表す。
type SessionItem struct {
	Session core.Session
}

// Title はセッション行のタイトル文字列を返す。
func (i SessionItem) Title() string {
	icon := "●"
	s := i.Session
	stateIcon := stateStyle(s.State).Render(icon)

	parts := []string{stateIcon, s.Tool.String(), s.State.String()}
	if s.Branch != "" {
		parts = append(parts, s.Branch)
	}
	return strings.Join(parts, "  ")
}

// Description はセッション行の補足説明文字列を返す。
func (i SessionItem) Description() string {
	s := i.Session
	parts := []string{fmt.Sprintf("PID:%d", s.PID)}
	if s.CurrentTool != "" {
		parts = append(parts, s.CurrentTool)
	}
	if s.InputTokens > 0 {
		parts = append(parts, fmt.Sprintf("%dk tokens", s.InputTokens/1000))
	}
	return strings.Join(parts, " · ")
}

// FilterValue はセッション行の検索対象文字列を返す。
func (i SessionItem) FilterValue() string {
	return strings.Join([]string{i.Session.ID, i.Session.ProjectPath, i.Session.State.String()}, " ")
}

// Model は TUI 全体を表す Bubble Tea のルートモデル。
type Model struct {
	projectList list.Model
	sessionList list.Model

	scanner      core.Scanner
	stateUpdater core.StateUpdater
	stateReader  core.StateReader
	terminal     terminal.Terminal
	config       config.Config

	latestProjects []core.Project
	latestSummary  core.Summary
	latestPanes    []terminal.Pane

	activePane      int
	width           int
	height          int
	err             error
	selectedProject string

	showSubMenu   bool
	subMenuItems  []SubMenuItem
	subMenuCursor int

	jumping bool // ペインジャンプ実行中（キー入力をブロック）
}

// NewModel はデフォルト delegate を使って TUI モデルを初期化する。
func NewModel(
	scanner core.Scanner,
	stateUpdater core.StateUpdater,
	stateReader core.StateReader,
	term terminal.Terminal,
	cfg config.Config,
) Model {
	brand := lipgloss.Color("#E8832A")
	secondary := lipgloss.Color("#F5A623")
	normalText := lipgloss.Color("#E8E4E0")

	titleStyle := lipgloss.NewStyle().
		Foreground(brand).
		Bold(true).
		Padding(0, 1)

	delegate := list.NewDefaultDelegate()
	delegate.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(normalText).
		Padding(0, 0, 0, 2)
	delegate.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(secondary).
		Padding(0, 0, 0, 2)
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(brand).
		Bold(true).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(brand).
		Padding(0, 0, 0, 1)
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(secondary).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(brand).
		Padding(0, 0, 0, 1)

	projectList := list.New([]list.Item{}, delegate, defaultListWidth, defaultListHeight)
	projectList.Title = "Projects"
	projectList.Styles.Title = titleStyle
	projectList.SetShowHelp(false)

	sessionList := list.New([]list.Item{}, delegate, defaultListWidth, defaultListHeight)
	sessionList.Title = "Sessions"
	sessionList.Styles.Title = titleStyle
	sessionList.SetShowHelp(false)

	return Model{
		projectList:  projectList,
		sessionList:  sessionList,
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
		// 無効値が来た場合のフォールバック
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

package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

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
func (i ProjectItem) Description() string {
	return fmt.Sprintf("sessions: %d", len(i.Project.Sessions))
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

// Description はセッション行の補足説明文字列を返す。
func (i SessionItem) Description() string {
	if i.Session.CurrentTool == "" && i.Session.InputTokens == 0 {
		return ""
	}
	return fmt.Sprintf("    %s  |  %d tokens", i.Session.CurrentTool, i.Session.InputTokens)
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
}

// NewModel はデフォルト delegate を使って TUI モデルを初期化する。
func NewModel(
	scanner core.Scanner,
	stateUpdater core.StateUpdater,
	stateReader core.StateReader,
	term terminal.Terminal,
	cfg config.Config,
) Model {
	projectList := list.New([]list.Item{}, list.NewDefaultDelegate(), defaultListWidth, defaultListHeight)
	projectList.Title = "Projects"

	sessionList := list.New([]list.Item{}, list.NewDefaultDelegate(), defaultListWidth, defaultListHeight)
	sessionList.Title = "Sessions"

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

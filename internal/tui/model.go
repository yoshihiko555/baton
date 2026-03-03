package tui

import (
	"errors"
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

// StateUpdateMsg は最新のプロジェクト一覧スナップショットを運ぶ。
type StateUpdateMsg []core.Project

// ErrMsg は非同期コマンドで発生したエラーを運ぶ。
type ErrMsg error

// WatchEventMsg はファイル監視イベントを運ぶ。
type WatchEventMsg core.WatchEvent

// ProjectItem はプロジェクト一覧の1行を表す。
type ProjectItem struct {
	Project core.Project
}

// Title はプロジェクト行のタイトル文字列を返す。
func (i ProjectItem) Title() string {
	if i.Project.DisplayName != "" {
		return i.Project.DisplayName
	}
	return i.Project.Path
}

// Description はプロジェクト行の補足説明文字列を返す。
func (i ProjectItem) Description() string {
	return fmt.Sprintf("sessions: %d / active: %d", len(i.Project.Sessions), i.Project.ActiveCount)
}

// FilterValue はプロジェクト行の検索対象文字列を返す。
func (i ProjectItem) FilterValue() string {
	return strings.Join([]string{i.Project.DisplayName, i.Project.Path}, " ")
}

// SessionItem はセッション一覧の1行を表す。
type SessionItem struct {
	Session core.Session
}

// Title はセッション行のタイトル文字列を返す。
func (i SessionItem) Title() string {
	return i.Session.ID
}

// Description はセッション行の補足説明文字列を返す。
func (i SessionItem) Description() string {
	if i.Session.LastActivity.IsZero() {
		return i.Session.State.String()
	}

	return fmt.Sprintf("%s / %s", i.Session.State.String(), i.Session.LastActivity.Format(time.RFC3339))
}

// FilterValue はセッション行の検索対象文字列を返す。
func (i SessionItem) FilterValue() string {
	return strings.Join([]string{i.Session.ID, i.Session.ProjectPath, i.Session.State.String()}, " ")
}

// Model は TUI 全体を表す Bubble Tea のルートモデル。
type Model struct {
	projectList list.Model
	sessionList list.Model

	stateReader core.StateReader
	stateWriter core.StateWriter
	watcher     core.EventSource
	terminal    terminal.Terminal
	config      config.Config

	activePane      int
	width           int
	height          int
	err             error
	selectedProject string
}

// NewModel はデフォルト delegate を使って TUI モデルを初期化する。
func NewModel(
	stateReader core.StateReader,
	stateWriter core.StateWriter,
	watcher core.EventSource,
	terminal terminal.Terminal,
	cfg config.Config,
) Model {
	projectList := list.New([]list.Item{}, list.NewDefaultDelegate(), defaultListWidth, defaultListHeight)
	projectList.Title = "Projects"

	sessionList := list.New([]list.Item{}, list.NewDefaultDelegate(), defaultListWidth, defaultListHeight)
	sessionList.Title = "Sessions"

	return Model{
		projectList: projectList,
		sessionList: sessionList,
		stateReader: stateReader,
		stateWriter: stateWriter,
		watcher:     watcher,
		terminal:    terminal,
		config:      cfg,
	}
}

// Init は tea.Model の初期コマンドを返す。
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(m.config.RefreshInterval),
		listenWatcherCmd(m.watcher),
	)
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

func listenWatcherCmd(watcher core.EventSource) tea.Cmd {
	return func() tea.Msg {
		if watcher == nil {
			return ErrMsg(errors.New("watcher is nil"))
		}

		event, ok := <-watcher.Events()
		if !ok {
			return ErrMsg(errors.New("watcher event channel closed"))
		}

		return WatchEventMsg(event)
	}
}

func refreshStateCmd(stateReader core.StateReader) tea.Cmd {
	return func() tea.Msg {
		if stateReader == nil {
			return ErrMsg(errors.New("state reader is nil"))
		}
		return StateUpdateMsg(stateReader.GetProjects())
	}
}

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

// TickMsg is emitted when the periodic refresh timer fires.
type TickMsg struct{}

// StateUpdateMsg carries the latest project snapshot.
type StateUpdateMsg []core.Project

// ErrMsg carries an async command error.
type ErrMsg error

// WatchEventMsg carries a file watcher event.
type WatchEventMsg core.WatchEvent

// ProjectItem represents a project row in the project list.
type ProjectItem struct {
	Project core.Project
}

// Title returns the list title for the project row.
func (i ProjectItem) Title() string {
	if i.Project.DisplayName != "" {
		return i.Project.DisplayName
	}
	return i.Project.Path
}

// Description returns the list description for the project row.
func (i ProjectItem) Description() string {
	return fmt.Sprintf("sessions: %d / active: %d", len(i.Project.Sessions), i.Project.ActiveCount)
}

// FilterValue returns searchable text for the project row.
func (i ProjectItem) FilterValue() string {
	return strings.Join([]string{i.Project.DisplayName, i.Project.Path}, " ")
}

// SessionItem represents a session row in the session list.
type SessionItem struct {
	Session core.Session
}

// Title returns the list title for the session row.
func (i SessionItem) Title() string {
	return i.Session.ID
}

// Description returns the list description for the session row.
func (i SessionItem) Description() string {
	if i.Session.LastActivity.IsZero() {
		return i.Session.State.String()
	}

	return fmt.Sprintf("%s / %s", i.Session.State.String(), i.Session.LastActivity.Format(time.RFC3339))
}

// FilterValue returns searchable text for the session row.
func (i SessionItem) FilterValue() string {
	return strings.Join([]string{i.Session.ID, i.Session.ProjectPath, i.Session.State.String()}, " ")
}

// Model is the root Bubble Tea model for the TUI.
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

// NewModel creates a new TUI model with default list delegates.
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

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(m.config.RefreshInterval),
		listenWatcherCmd(m.watcher),
	)
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

package tui

import (
	"errors"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/yoshihiko555/baton/internal/core"
)

var (
	quitKeys = key.NewBinding(key.WithKeys("q", "ctrl+c"))
	tabKey   = key.NewBinding(key.WithKeys("tab"))
	enterKey = key.NewBinding(key.WithKeys("enter"))

	leftKeys  = key.NewBinding(key.WithKeys("h", "left"))
	rightKeys = key.NewBinding(key.WithKeys("l", "right"))
	upKeys    = key.NewBinding(key.WithKeys("k", "up"))
	downKeys  = key.NewBinding(key.WithKeys("j", "down"))
)

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, quitKeys):
			return m, tea.Quit
		case key.Matches(msg, tabKey):
			m.activePane = 1 - m.activePane
			return m, nil
		case key.Matches(msg, leftKeys):
			m.activePane = 0
			return m, nil
		case key.Matches(msg, rightKeys):
			m.activePane = 1
			return m, nil
		case key.Matches(msg, enterKey):
			if m.activePane == 0 {
				if selected, ok := m.projectList.SelectedItem().(ProjectItem); ok {
					m.selectedProject = selected.Project.Path
					m.activePane = 1
					m.updateSessionList(m.projectsFromProjectList())
				}
				return m, nil
			}

			selected, ok := m.sessionList.SelectedItem().(SessionItem)
			if !ok {
				return m, nil
			}

			if m.terminal == nil {
				m.err = errors.New("terminal is nil")
				return m, nil
			}
			if !m.terminal.IsAvailable() {
				m.err = errors.New("terminal is not available")
				return m, nil
			}
			if selected.Session.PaneID == "" {
				m.err = errors.New("selected session has no pane id")
				return m, nil
			}
			if err := m.terminal.FocusPane(selected.Session.PaneID); err != nil {
				m.err = err
			}
			return m, nil
		case key.Matches(msg, upKeys, downKeys):
			return m.updateActiveList(msg)
		default:
			return m.updateActiveList(msg)
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		leftWidth := msg.Width / 2
		if leftWidth < 1 {
			leftWidth = 1
		}
		rightWidth := msg.Width - leftWidth
		if rightWidth < 1 {
			rightWidth = 1
		}

		m.projectList.SetSize(leftWidth, msg.Height)
		m.sessionList.SetSize(rightWidth, msg.Height)
		return m, nil
	case TickMsg:
		return m, tea.Batch(
			refreshStateCmd(m.stateReader),
			tickCmd(m.config.RefreshInterval),
		)
	case StateUpdateMsg:
		projects := []core.Project(msg)
		m.updateProjectList(projects)
		m.updateSessionList(projects)
		return m, listenWatcherCmd(m.watcher)
	case WatchEventMsg:
		if m.stateWriter != nil {
			if err := m.stateWriter.HandleEvent(core.WatchEvent(msg)); err != nil {
				m.err = err
			}
		}
		return m, refreshStateCmd(m.stateReader)
	case ErrMsg:
		m.err = msg
		return m, listenWatcherCmd(m.watcher)
	default:
		return m.updateActiveList(msg)
	}
}

func (m Model) updateActiveList(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.activePane == 1 {
		var cmd tea.Cmd
		m.sessionList, cmd = m.sessionList.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.projectList, cmd = m.projectList.Update(msg)
	return m, cmd
}

func (m *Model) updateProjectList(projects []core.Project) {
	currentPath := m.selectedProject
	if currentPath == "" {
		if selected, ok := m.projectList.SelectedItem().(ProjectItem); ok {
			currentPath = selected.Project.Path
		}
	}

	items := make([]list.Item, 0, len(projects))
	selectedIndex := 0
	selectedFound := false

	for i := range projects {
		project := projects[i]
		items = append(items, ProjectItem{Project: project})
		if project.Path == currentPath {
			selectedIndex = i
			selectedFound = true
		}
	}

	m.projectList.SetItems(items)

	if len(items) == 0 {
		m.selectedProject = ""
		m.sessionList.SetItems([]list.Item{})
		return
	}

	m.projectList.Select(selectedIndex)
	if m.selectedProject != "" && !selectedFound {
		m.selectedProject = ""
	}
}

func (m *Model) updateSessionList(projects []core.Project) {
	targetProjectPath := m.selectedProject
	if targetProjectPath == "" {
		if selected, ok := m.projectList.SelectedItem().(ProjectItem); ok {
			targetProjectPath = selected.Project.Path
		}
	}

	var targetProject *core.Project
	for i := range projects {
		if projects[i].Path == targetProjectPath {
			targetProject = &projects[i]
			break
		}
	}
	if targetProject == nil && len(projects) > 0 {
		targetProject = &projects[0]
	}

	if targetProject == nil {
		m.sessionList.SetItems([]list.Item{})
		return
	}

	currentSessionID := ""
	if selected, ok := m.sessionList.SelectedItem().(SessionItem); ok {
		currentSessionID = selected.Session.ID
	}

	items := make([]list.Item, 0, len(targetProject.Sessions))
	selectedIndex := 0
	index := 0
	for _, session := range targetProject.Sessions {
		if session == nil {
			continue
		}

		items = append(items, SessionItem{Session: *session})
		if session.ID == currentSessionID {
			selectedIndex = index
		}
		index++
	}

	m.sessionList.SetItems(items)
	if len(items) > 0 {
		m.sessionList.Select(selectedIndex)
	}
}

func (m Model) projectsFromProjectList() []core.Project {
	items := m.projectList.Items()
	projects := make([]core.Project, 0, len(items))

	for _, item := range items {
		projectItem, ok := item.(ProjectItem)
		if !ok {
			continue
		}
		projects = append(projects, projectItem.Project)
	}

	return projects
}

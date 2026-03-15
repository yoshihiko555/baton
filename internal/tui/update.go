package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/yoshihiko555/baton/internal/core"
	"github.com/yoshihiko555/baton/internal/terminal"
)

var (
	quitKeys = key.NewBinding(key.WithKeys("q", "ctrl+c"))
	tabKey   = key.NewBinding(key.WithKeys("tab"))
	enterKey = key.NewBinding(key.WithKeys("enter"))
	escKey   = key.NewBinding(key.WithKeys("esc"))

	leftKeys  = key.NewBinding(key.WithKeys("h", "left"))
	rightKeys = key.NewBinding(key.WithKeys("l", "right"))
	upKeys    = key.NewBinding(key.WithKeys("k", "up"))
	downKeys  = key.NewBinding(key.WithKeys("j", "down"))
)

// Update は tea.Model のメッセージ処理を行う。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case JumpDoneMsg:
		if msg.Err != nil {
			m.err = msg.Err
			m.jumping = false
			return m, nil
		}
		return m, tea.Quit
	case tea.KeyMsg:
		// ジャンプ実行中はキー入力を無視する。
		if m.jumping {
			return m, nil
		}
		// サブメニュー表示中は専用のキーハンドリングを行う。
		if m.showSubMenu {
			return m.updateSubMenu(msg)
		}
		switch {
		case key.Matches(msg, quitKeys):
			return m, tea.Quit
		case key.Matches(msg, tabKey):
			// 左右ペインのアクティブ状態をトグルする。
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
				// project 側で Enter: 選択プロジェクトを固定して session 側へ移動。
				if selected, ok := m.projectList.SelectedItem().(ProjectItem); ok {
					m.selectedProject = selected.Project.Path
					m.activePane = 1
					m.updateSessionList(m.latestProjects)
				}
				return m, nil
			}

			selected, ok := m.sessionList.SelectedItem().(SessionItem)
			if !ok {
				return m, nil
			}

			if selected.Session.Ambiguous {
				m.showSubMenu = true
				m.subMenuItems = buildSubMenuItems(selected.Session.CandidatePaneIDs, m.latestPanes)
				m.subMenuCursor = 0
				return m, nil
			}

			if selected.Session.PaneID == "" {
				m.err = errors.New("selected session has no pane id")
				return m, nil
			}
			paneID, atoiErr := strconv.Atoi(selected.Session.PaneID)
			if atoiErr != nil {
				m.err = fmt.Errorf("invalid pane id %q: %w", selected.Session.PaneID, atoiErr)
				return m, nil
			}
			m.jumping = true
			term := m.terminal
			return m, func() tea.Msg {
				err := term.FocusPane(paneID)
				return JumpDoneMsg{Err: err}
			}
		case key.Matches(msg, upKeys, downKeys):
			return m.updateActiveList(msg)
		default:
			return m.updateActiveList(msg)
		}
	case tea.WindowSizeMsg:
		// 画面サイズ変更時に左右リストのサイズを再計算する。
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
		// 定期ポーリング: スキャンを実行する。
		return m, doScanCmd(context.Background(), m.scanner, m.stateUpdater, m.stateReader)
	case ScanResultMsg:
		// スキャン結果で状態を更新し、次 tick を予約する。
		m.latestProjects = msg.Projects
		m.latestSummary = msg.Summary
		m.latestPanes = msg.Panes
		m.updateProjectList(msg.Projects)
		m.updateSessionList(msg.Projects)
		return m, tickCmd(m.config.ScanInterval)
	case ErrMsg:
		m.err = msg
		return m, tickCmd(m.config.ScanInterval)
	default:
		return m.updateActiveList(msg)
	}
}

// updateSubMenu はサブメニュー表示中のキー入力を処理する。
func (m Model) updateSubMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, escKey):
		m.showSubMenu = false
		return m, nil
	case key.Matches(msg, upKeys):
		if m.subMenuCursor > 0 {
			m.subMenuCursor--
		}
		return m, nil
	case key.Matches(msg, downKeys):
		if m.subMenuCursor < len(m.subMenuItems)-1 {
			m.subMenuCursor++
		}
		return m, nil
	case key.Matches(msg, enterKey):
		if len(m.subMenuItems) == 0 {
			return m, nil
		}
		item := m.subMenuItems[m.subMenuCursor]
		m.showSubMenu = false
		if m.terminal == nil {
			m.err = errors.New("terminal is nil")
			return m, nil
		}
		if err := m.terminal.FocusPane(item.PaneID); err != nil {
			m.err = err
		}
		return m, nil
	}
	return m, nil
}

func (m Model) updateActiveList(msg tea.Msg) (tea.Model, tea.Cmd) {
	// アクティブな側の list.Model にのみ入力を渡す。
	if m.activePane == 1 {
		var cmd tea.Cmd
		m.sessionList, cmd = m.sessionList.Update(msg)
		return m, cmd
	}

	// プロジェクト側のカーソル移動前の選択を記憶する。
	prevSelected := ""
	if sel, ok := m.projectList.SelectedItem().(ProjectItem); ok {
		prevSelected = sel.Project.Path
	}

	var cmd tea.Cmd
	m.projectList, cmd = m.projectList.Update(msg)

	// カーソルが別プロジェクトに移動したら右ペインを即座に更新する。
	if sel, ok := m.projectList.SelectedItem().(ProjectItem); ok {
		if sel.Project.Path != prevSelected {
			m.updateSessionList(m.latestProjects)
		}
	}

	return m, cmd
}

func (m *Model) updateProjectList(projects []core.Project) {
	// 既存選択をできるだけ維持する。
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
	// selectedProject があれば優先し、無ければ現在選択中の project を使う。
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
		// 対象未決定時は先頭プロジェクトを採用する。
		targetProject = &projects[0]
	}

	if targetProject == nil {
		m.sessionList.SetItems([]list.Item{})
		return
	}

	// 現在選択中のセッションの PID を保持する。
	currentPID := 0
	if selected, ok := m.sessionList.SelectedItem().(SessionItem); ok {
		currentPID = selected.Session.PID
	}

	// nil を除外してコピーし、PID 順で安定ソートする。
	sessions := make([]*core.Session, 0, len(targetProject.Sessions))
	for _, s := range targetProject.Sessions {
		if s != nil {
			sessions = append(sessions, s)
		}
	}

	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].PID < sessions[j].PID
	})

	items := make([]list.Item, 0, len(sessions))
	selectedIndex := 0
	for idx, s := range sessions {
		items = append(items, SessionItem{Session: *s})
		if s.PID == currentPID && currentPID != 0 {
			selectedIndex = idx
		}
	}

	m.sessionList.SetItems(items)
	if len(items) > 0 {
		m.sessionList.Select(selectedIndex)
	}
}

func (m Model) projectsFromProjectList() []core.Project {
	// 画面表示中の project items を core.Project のスライスへ戻す。
	items := m.projectList.Items()
	projects := make([]core.Project, 0, len(items))

	for _, item := range items {
		if pi, ok := item.(ProjectItem); ok {
			projects = append(projects, pi.Project)
		}
	}

	return projects
}

// buildSubMenuItems は候補ペイン ID リストからサブメニュー項目を生成する。
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

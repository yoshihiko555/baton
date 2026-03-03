package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/yoshihiko555/baton/internal/core"
)

var (
	activeBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62"))
	inactiveBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240"))
	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(0, 1)
)

var stateColors = map[core.SessionState]lipgloss.Color{
	core.Idle:     lipgloss.Color("240"),
	core.Thinking: lipgloss.Color("220"),
	core.ToolUse:  lipgloss.Color("82"),
	core.Error:    lipgloss.Color("196"),
}

// View は tea.Model の描画文字列を返す。
func (m Model) View() string {
	totalWidth := m.width
	if totalWidth <= 0 {
		// 初回描画時など WindowSize 未受信ならデフォルトサイズを使う。
		totalWidth = defaultListWidth*2 + 4
	}

	totalHeight := m.height
	if totalHeight <= 0 {
		totalHeight = defaultListHeight + 3
	}

	paneWidth := max(1, totalWidth/2-2)
	paneHeight := max(1, totalHeight-3)

	projectList := m.projectList
	projectList.SetSize(paneWidth, paneHeight)

	sessionList := m.sessionList
	sessionList.SetSize(paneWidth, paneHeight)

	leftPaneStyle := inactiveBorderStyle.Width(paneWidth).Height(paneHeight)
	rightPaneStyle := inactiveBorderStyle.Width(paneWidth).Height(paneHeight)

	if m.activePane == 0 {
		leftPaneStyle = activeBorderStyle.Width(paneWidth).Height(paneHeight)
	} else {
		rightPaneStyle = activeBorderStyle.Width(paneWidth).Height(paneHeight)
	}

	leftPane := leftPaneStyle.Render(projectList.View())
	rightPane := rightPaneStyle.Render(sessionList.View())

	panes := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)
	statusBar := m.renderStatusBar(totalWidth)
	view := lipgloss.JoinVertical(lipgloss.Left, panes, statusBar)

	if m.err != nil {
		// エラー時は上段に明示表示する。
		errLine := stateStyle(core.Error).Render(fmt.Sprintf("error: %v", m.err))
		return lipgloss.JoinVertical(lipgloss.Left, errLine, view)
	}

	return view
}

func (m Model) renderStatusBar(totalWidth int) string {
	projectCount := len(m.projectList.Items())
	activeCount := 0
	lastUpdate := time.Time{}

	for _, item := range m.projectList.Items() {
		projectItem, ok := item.(ProjectItem)
		if !ok {
			continue
		}

		activeCount += projectItem.Project.ActiveCount
		for _, session := range projectItem.Project.Sessions {
			if session == nil {
				continue
			}

			if session.LastActivity.After(lastUpdate) {
				lastUpdate = session.LastActivity
			}
		}
	}

	lastUpdateLabel := "-"
	if !lastUpdate.IsZero() {
		// 表示はローカル時刻に寄せる。
		lastUpdateLabel = lastUpdate.Local().Format("15:04:05")
	}

	status := fmt.Sprintf(
		"Projects: %d | Active: %d | Last update: %s",
		projectCount,
		activeCount,
		lastUpdateLabel,
	)

	return statusBarStyle.Width(max(1, totalWidth)).Render(status)
}

func stateStyle(state core.SessionState) lipgloss.Style {
	color, ok := stateColors[state]
	if !ok {
		// 未知状態は控えめな色で表示する。
		color = lipgloss.Color("240")
	}
	return lipgloss.NewStyle().Foreground(color)
}

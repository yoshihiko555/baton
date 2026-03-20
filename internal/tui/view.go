package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/yoshihiko555/baton/internal/core"
)

var (
	activeBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder()).
				BorderForeground(lipgloss.Color("#E8832A")) // brand orange
	inactiveBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#2AADE8")) // blue
	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E8E4E0")). // light gray
			Padding(0, 1)
)

var stateColors = map[core.SessionState]lipgloss.Color{
	core.Idle:     lipgloss.Color("240"),
	core.Thinking: lipgloss.Color("220"),
	core.ToolUse:  lipgloss.Color("43"),  // v1: 82 → v2: シアンに変更
	core.Waiting:  lipgloss.Color("208"), // 新規: オレンジ（承認待ち）
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

	if m.showSubMenu {
		return lipgloss.JoinVertical(lipgloss.Left, panes, m.renderSubMenu(), statusBar)
	}

	view := lipgloss.JoinVertical(lipgloss.Left, panes, statusBar)

	if m.jumping {
		jumpLine := stateStyle(core.Thinking).Render("Switching workspace...")
		return lipgloss.JoinVertical(lipgloss.Left, jumpLine, view)
	}

	if m.err != nil {
		errLine := stateStyle(core.Error).Render(fmt.Sprintf("error: %v", m.err))
		return lipgloss.JoinVertical(lipgloss.Left, errLine, view)
	}

	return view
}

func (m Model) renderStatusBar(totalWidth int) string {
	s := m.latestSummary

	left := fmt.Sprintf(" %d sessions · %d active · %d waiting", s.TotalSessions, s.Active, s.Waiting)
	right := "↑↓/jk:move  tab:pane  enter:jump  q:quit "

	gap := totalWidth - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	bar := left + strings.Repeat(" ", gap) + right
	return statusBarStyle.Width(max(1, totalWidth)).Render(bar)
}

// renderSubMenu はサブメニューを描画する。
func (m Model) renderSubMenu() string {
	lines := []string{"Select pane:"}
	for i, item := range m.subMenuItems {
		cursor := "  "
		if i == m.subMenuCursor {
			cursor = "> "
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s", cursor, item.PaneID, item.TTYName))
	}
	lines = append(lines, "  esc: cancel")
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("208")).
		Padding(0, 1)
	return style.Render(strings.Join(lines, "\n"))
}

func stateStyle(state core.SessionState) lipgloss.Style {
	color, ok := stateColors[state]
	if !ok {
		// 未知状態は控えめな色で表示する。
		color = lipgloss.Color("240")
	}
	return lipgloss.NewStyle().Foreground(color)
}

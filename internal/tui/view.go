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
				BorderForeground(lipgloss.Color("#836FFF")) // soft electric blue
	inactiveBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#3D2A7A")) // muted midnight
	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Padding(0, 1)
	actionBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Padding(0, 1)
)

// グループヘッダーの色
var groupHeaderColors = map[string]lipgloss.Color{
	"WAITING": lipgloss.Color("#FF2DAA"), // hot pink
	"ERROR":   lipgloss.Color("#FF4444"), // red (keep)
	"WORKING": lipgloss.Color("#15F5BA"), // neon mint
	"IDLE":    lipgloss.Color("#836FFF"), // soft electric blue
}

// セッション状態の色
var stateColors = map[core.SessionState]lipgloss.Color{
	core.Idle:     lipgloss.Color("#836FFF"),
	core.Thinking: lipgloss.Color("#15F5BA"),
	core.ToolUse:  lipgloss.Color("#15F5BA"),
	core.Waiting:  lipgloss.Color("#FF2DAA"),
	core.Error:    lipgloss.Color("#FF4444"),
}

// ツールタイプの色
var toolColors = map[core.ToolType]lipgloss.Color{
	core.ToolClaude: lipgloss.Color("#F0F3FF"),
	core.ToolCodex:  lipgloss.Color("#15F5BA"),
	core.ToolGemini: lipgloss.Color("#836FFF"),
}

// 外側マージン
const outerPadH = 2 // 左右パディング
const outerPadV = 1 // 上下パディング

// View は tea.Model の描画文字列を返す。
func (m Model) View() string {
	totalWidth := m.width
	if totalWidth <= 0 {
		totalWidth = 80
	}
	totalHeight := m.height
	if totalHeight <= 0 {
		totalHeight = 24
	}

	// 内側で使える幅・高さ（外側余白を引く）
	innerWidth := max(40, totalWidth-outerPadH*2)
	innerHeight := max(6, totalHeight-outerPadV*2)

	// ── ヘッダー行: アプリ名 + セッション数 ──
	headerLine := m.renderHeader(innerWidth)

	// ペイン領域の高さを計算:
	// innerHeight から以下を引く:
	//   ヘッダー行(1) + 空行(1) + ステータスバー(1) + アクションバー(1) = 4行
	//   + ボーダー上下(2行) = 合計6行
	paneHeight := max(1, innerHeight-6)

	// 左右の幅（左40%、右60%）
	leftWidth := max(20, innerWidth*2/5-2)
	rightWidth := max(20, innerWidth-leftWidth-4)

	// 左ペイン: セッションリスト
	leftContent := m.renderSessionList(leftWidth, paneHeight)
	leftStyle := inactiveBorderStyle.Width(leftWidth).Height(paneHeight)
	if m.activePane == 0 {
		leftStyle = activeBorderStyle.Width(leftWidth).Height(paneHeight)
	}
	leftPane := leftStyle.Render(leftContent)

	// 右ペイン: プレビュー
	rightContent := m.renderPreview(rightWidth, paneHeight)
	rightStyle := inactiveBorderStyle.Width(rightWidth).Height(paneHeight)
	if m.activePane == 1 {
		rightStyle = activeBorderStyle.Width(rightWidth).Height(paneHeight)
	}
	rightPane := rightStyle.Render(rightContent)

	panes := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)

	// ステータスバー + アクションバー
	statusBar := m.renderStatusBar(innerWidth)
	actionBar := m.renderActionBar(innerWidth)

	// テキスト入力バー（プロンプト付き承認/拒否）
	var inputBar string
	if m.inputMode != inputNone {
		label := "Approve prompt"
		if m.inputMode == inputDeny {
			label = "Reject feedback"
		}
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF2DAA")).Bold(true)
		inputBar = labelStyle.Render(label+": ") + m.textInput.View()
	}

	// 中身を組み立て
	var inner string
	if m.inputMode != inputNone {
		inner = lipgloss.JoinVertical(lipgloss.Left, headerLine, "", panes, inputBar, statusBar)
	} else if m.showSubMenu {
		inner = lipgloss.JoinVertical(lipgloss.Left, headerLine, "", panes, m.renderSubMenu(), statusBar, actionBar)
	} else {
		inner = lipgloss.JoinVertical(lipgloss.Left, headerLine, "", panes, statusBar, actionBar)
	}

	if m.flashMessage != "" {
		flashStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#15F5BA")).Bold(true)
		flashLine := flashStyle.Render(">> " + m.flashMessage)
		inner = lipgloss.JoinVertical(lipgloss.Left, flashLine, inner)
	}

	if m.jumping {
		jumpLine := stateStyle(core.Thinking).Render("Switching workspace...")
		inner = lipgloss.JoinVertical(lipgloss.Left, jumpLine, inner)
	}

	if m.err != nil {
		errLine := stateStyle(core.Error).Render(fmt.Sprintf("error: %v", m.err))
		inner = lipgloss.JoinVertical(lipgloss.Left, errLine, inner)
	}

	// 外側余白を適用
	outerStyle := lipgloss.NewStyle().
		Padding(outerPadV, outerPadH)

	return outerStyle.Render(inner)
}

// renderHeader はアプリ名 + セッション概要のヘッダー行を描画する。
func (m Model) renderHeader(totalWidth int) string {
	brand := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#836FFF")).
		Bold(true)
	subtitle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	activeColor := lipgloss.NewStyle().Foreground(lipgloss.Color("#15F5BA"))
	waitColor := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF2DAA"))

	left := brand.Render("baton") + subtitle.Render("  AI Session Monitor")

	s := m.latestSummary
	idle := s.TotalSessions - s.Active
	var infoParts []string
	infoParts = append(infoParts, dim.Render(fmt.Sprintf("%d sessions", s.TotalSessions)))
	infoParts = append(infoParts, activeColor.Render(fmt.Sprintf("%d active", s.Active)))
	infoParts = append(infoParts, waitColor.Render(fmt.Sprintf("%d waiting", s.Waiting)))
	infoParts = append(infoParts, dim.Render(fmt.Sprintf("%d idle", idle)))
	right := strings.Join(infoParts, dim.Render("  "))


	gap := max(0, totalWidth-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

// renderSessionList は状態グループ付きセッションリストを描画する。
func (m Model) renderSessionList(width, height int) string {
	if len(m.entries) == 0 {
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
		return dim.Render("  No sessions found")
	}

	// カーソル行の絶対位置を算出
	visibleLines := height
	cursorLine := 0
	for i := 0; i < m.cursor && i < len(m.entries); i++ {
		cursorLine += entryHeight(m.entries[i])
	}
	cursorHeight := 0
	if m.cursor >= 0 && m.cursor < len(m.entries) {
		cursorHeight = entryHeight(m.entries[m.cursor])
	}

	// スクロールオフセット: カーソルが画面内に収まるよう調整
	startLine := 0
	if cursorLine+cursorHeight > visibleLines {
		startLine = cursorLine + cursorHeight - visibleLines
	}

	var lines []string
	currentLine := 0

	for i, e := range m.entries {
		h := entryHeight(e)

		// スクロール範囲外はスキップ
		if currentLine+h <= startLine {
			currentLine += h
			continue
		}
		if currentLine >= startLine+visibleLines {
			break
		}

		if e.isHeader {
			lines = append(lines, renderGroupHeader(e, width))
		} else {
			isSelected := i == m.cursor
			lines = append(lines, renderSessionEntry(&e, width, isSelected)...)
		}

		currentLine += h
	}

	return strings.Join(lines, "\n")
}

// entryHeight はエントリの描画行数を返す。
func entryHeight(e sessionEntry) int {
	if e.isHeader {
		return 1
	}
	return 2 // メイン行 + 詳細行
}

// renderGroupHeader はグループヘッダー行を描画する。
func renderGroupHeader(e sessionEntry, width int) string {
	color, ok := groupHeaderColors[e.header]
	if !ok {
		color = lipgloss.Color("#888888")
	}
	style := lipgloss.NewStyle().Foreground(color).Bold(true)
	lineStyle := lipgloss.NewStyle().Foreground(color)

	label := fmt.Sprintf(" %s %s ", e.icon, e.header)
	labelWidth := lipgloss.Width(label)

	// ラベルの両側に区切り線
	leftLine := lineStyle.Render("──")
	rightLineWidth := max(0, width-labelWidth-4)
	rightLine := lineStyle.Render(strings.Repeat("─", rightLineWidth))

	return leftLine + style.Render(label) + rightLine
}

// renderSessionEntry はセッション行を描画する。
func renderSessionEntry(e *sessionEntry, width int, isSelected bool) []string {
	if e.session == nil {
		return []string{"  ?"}
	}

	s := e.session
	name := sessionDisplayName(e)

	// 状態インジケーター
	stateColor := stateColors[s.State]
	indicator := lipgloss.NewStyle().Foreground(stateColor).Render("●")

	// ツール名
	toolColor, ok := toolColors[s.Tool]
	if !ok {
		toolColor = lipgloss.Color("#AAAAAA")
	}
	toolName := lipgloss.NewStyle().Foreground(toolColor).Render(s.Tool.String())

	// カーソル
	cursor := "  "
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E8E4E0"))
	if isSelected {
		cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("#836FFF")).Render("▎ ")
		nameStyle = nameStyle.Bold(true)
	}

	// メイン行: ▎ project-name    ● claude
	mainRight := fmt.Sprintf("%s %s", indicator, toolName)
	mainRightWidth := lipgloss.Width(mainRight)
	nameWidth := max(1, width-lipgloss.Width(cursor)-mainRightWidth-2)
	displayName := truncate(name, nameWidth)
	gap := max(0, nameWidth-lipgloss.Width(displayName))
	mainLine := cursor + nameStyle.Render(displayName) + strings.Repeat(" ", gap) + "  " + mainRight

	// 詳細行: branch  [state]  currentTool
	detail := sessionDetailLine(e)
	detailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	if isSelected {
		detailStyle = detailStyle.Foreground(lipgloss.Color("#AAAAAA"))
	}
	detailLine := cursor + detailStyle.Render(truncate(detail, max(1, width-lipgloss.Width(cursor))))

	return []string{mainLine, detailLine}
}

// renderPreview は右ペインのプレビューを描画する。
func (m Model) renderPreview(width, height int) string {
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#836FFF")).
		Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))

	header := titleStyle.Render("Preview")

	sel := m.selectedSession()
	if sel == nil {
		return header + "\n\n" + dimStyle.Render("  Select a session to preview")
	}

	// セッション情報ヘッダー
	s := sel.session
	name := sessionDisplayName(sel)
	info := fmt.Sprintf("  %s / %s  PID:%d", name, s.Tool, s.PID)
	if s.Branch != "" {
		info += fmt.Sprintf("  [%s]", s.Branch)
	}
	infoLine := lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA")).Render(info)

	separator := dimStyle.Render(strings.Repeat("─", width))

	// プレビューテキスト
	var previewContent string
	if m.previewLoading {
		previewContent = dimStyle.Render("  Loading...")
	} else if m.previewText == "" {
		previewContent = dimStyle.Render("  No output")
	} else {
		// 末尾の行を表示（高さに収まるように）
		previewLines := strings.Split(m.previewText, "\n")
		maxLines := max(1, height-4)
		start := max(0, len(previewLines)-maxLines)
		visible := previewLines[start:]

		// 各行を幅に収める
		var trimmed []string
		for _, line := range visible {
			trimmed = append(trimmed, truncate(line, width))
		}
		previewContent = strings.Join(trimmed, "\n")
	}

	return header + "\n" + infoLine + "\n" + separator + "\n" + previewContent
}

// renderStatusBar はセッション統計を描画する（ヘッダーと重複しないよう簡潔に）。
func (m Model) renderStatusBar(totalWidth int) string {
	s := m.latestSummary
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))

	// ツール別の内訳
	var toolParts []string
	for _, tool := range []string{"claude", "codex", "gemini"} {
		if count, ok := s.ByTool[tool]; ok && count > 0 {
			toolParts = append(toolParts, fmt.Sprintf("%s:%d", tool, count))
		}
	}
	left := ""
	if len(toolParts) > 0 {
		left = " " + dim.Render(strings.Join(toolParts, "  "))
	}

	return statusBarStyle.Width(max(1, totalWidth)).Render(left)
}

// renderActionBar はキーバインドヘルプを描画する。
func (m Model) renderActionBar(totalWidth int) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	key := lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA"))

	actions := []string{
		key.Render("j/k") + dim.Render(" move"),
		key.Render("tab") + dim.Render(" pane"),
		key.Render("enter") + dim.Render(" jump"),
	}
	if m.canApprove() {
		actions = append(actions,
			key.Render("a") + dim.Render(" approve"),
			key.Render("d") + dim.Render(" deny"),
			key.Render("A") + dim.Render(" approve+msg"),
			key.Render("D") + dim.Render(" deny+msg"),
		)
	}
	actions = append(actions, key.Render("q")+dim.Render(" quit"))

	bar := " " + strings.Join(actions, dim.Render(" . "))
	return actionBarStyle.Width(max(1, totalWidth)).Render(bar)
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
		BorderForeground(lipgloss.Color("#836FFF")).
		Padding(0, 1)
	return style.Render(strings.Join(lines, "\n"))
}

func stateStyle(state core.SessionState) lipgloss.Style {
	color, ok := stateColors[state]
	if !ok {
		color = lipgloss.Color("#666666")
	}
	return lipgloss.NewStyle().Foreground(color)
}

// truncate は文字列を指定幅に切り詰める（rune 単位で安全に処理）。
func truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for i := len(runes); i > 0; i-- {
		candidate := string(runes[:i]) + ".."
		if lipgloss.Width(candidate) <= maxWidth {
			return candidate
		}
	}
	return ""
}

package tui

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/yoshihiko555/baton/internal/core"
)

var (
	quitKeys = key.NewBinding(key.WithKeys("q", "ctrl+c"))
	tabKey   = key.NewBinding(key.WithKeys("tab"))
	enterKey = key.NewBinding(key.WithKeys("enter"))
	escKey   = key.NewBinding(key.WithKeys("esc"))

	upKeys   = key.NewBinding(key.WithKeys("k", "up"))
	downKeys = key.NewBinding(key.WithKeys("j", "down"))

	approveKey     = key.NewBinding(key.WithKeys("a"))
	denyKey        = key.NewBinding(key.WithKeys("d"))
	promptApprove  = key.NewBinding(key.WithKeys("A"))
	promptDeny     = key.NewBinding(key.WithKeys("D"))
)

// Update は tea.Model のメッセージ処理を行う。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ApprovalResultMsg:
		if msg.Err != nil {
			m.err = msg.Err
			m.flashMessage = ""
		} else {
			m.err = nil
			m.flashMessage = msg.Label
		}
		return m, nil
	case JumpDoneMsg:
		if msg.Err != nil {
			m.err = msg.Err
			m.jumping = false
			return m, nil
		}
		if m.exitOnJump {
			return m, tea.Quit
		}
		m.jumping = false
		return m, nil
	case tea.KeyMsg:
		if m.jumping {
			return m, nil
		}
		// テキスト入力モード中
		if m.inputMode != inputNone {
			return m.updateTextInput(msg)
		}
		if m.showSubMenu {
			return m.updateSubMenu(msg)
		}
		switch {
		case key.Matches(msg, quitKeys):
			return m, tea.Quit
		case key.Matches(msg, tabKey):
			m.activePane = 1 - m.activePane
			return m, nil
		case key.Matches(msg, enterKey):
			return m.handleEnter()
		case key.Matches(msg, upKeys):
			return m.moveCursor(-1)
		case key.Matches(msg, downKeys):
			return m.moveCursor(1)
		case key.Matches(msg, approveKey):
			return m.handleSimpleApprove()
		case key.Matches(msg, denyKey):
			return m.handleSimpleDeny()
		case key.Matches(msg, promptApprove):
			return m.enterInputMode(inputApprove)
		case key.Matches(msg, promptDeny):
			return m.enterInputMode(inputDeny)
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case TickMsg:
		return m, doScanCmd(context.Background(), m.scanner, m.stateUpdater, m.stateReader, m.terminal)
	case ScanResultMsg:
		m.err = nil          // スキャン成功で前回エラーをクリア
		m.flashMessage = ""  // フラッシュメッセージもクリア
		m.latestProjects = msg.Projects
		m.latestSummary = msg.Summary
		m.latestPanes = msg.Panes
		m.rebuildEntries()
		// 選択セッションが変わったらプレビュー更新
		cmd := m.maybeUpdatePreview()
		return m, tea.Batch(tickCmd(m.config.ScanInterval), cmd)
	case PreviewResultMsg:
		m.previewLoading = false
		if msg.Err != nil {
			m.previewText = fmt.Sprintf("Error: %v", msg.Err)
		} else {
			m.previewText = msg.Text
		}
		return m, nil
	case ErrMsg:
		m.err = msg
		return m, tickCmd(m.config.ScanInterval)
	}
	return m, nil
}

// handleEnter は Enter キーの処理を行う。
func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	sel := m.selectedSession()
	if sel == nil {
		return m, nil
	}

	s := sel.session
	if s.Ambiguous {
		m.showSubMenu = true
		m.subMenuItems = buildSubMenuItems(s.CandidatePaneIDs, m.latestPanes)
		m.subMenuCursor = 0
		return m, nil
	}

	if s.PaneID == "" {
		m.err = errors.New("selected session has no pane id")
		return m, nil
	}
	m.jumping = true
	term := m.terminal
	paneID := s.PaneID
	return m, func() tea.Msg {
		err := term.FocusPane(paneID)
		return JumpDoneMsg{Err: err}
	}
}

// moveCursor はカーソルを上下に移動する（ヘッダーをスキップ）。
func (m Model) moveCursor(delta int) (tea.Model, tea.Cmd) {
	if len(m.entries) == 0 {
		return m, nil
	}

	newCursor := m.cursor
	for {
		newCursor += delta
		if newCursor < 0 || newCursor >= len(m.entries) {
			// 範囲外なら元の位置のまま
			return m, m.maybeUpdatePreview()
		}
		if !m.entries[newCursor].isHeader {
			break
		}
	}

	m.cursor = newCursor
	cmd := m.maybeUpdatePreview()
	return m, cmd
}

// rebuildEntries はスキャン結果からエントリリストを再構築する。
func (m *Model) rebuildEntries() {
	// 現在選択中のセッション PID を記憶
	currentPID := 0
	if sel := m.selectedSession(); sel != nil && sel.session != nil {
		currentPID = sel.session.PID
	}

	m.entries = buildEntries(m.latestProjects)

	// カーソル復元: 同じ PID のエントリを探す
	m.cursor = -1
	for i, e := range m.entries {
		if !e.isHeader && e.session != nil && e.session.PID == currentPID {
			m.cursor = i
			break
		}
	}

	// 見つからなかった場合は最初のセッション行へ
	if m.cursor < 0 {
		for i, e := range m.entries {
			if !e.isHeader {
				m.cursor = i
				break
			}
		}
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// maybeUpdatePreview は選択セッションが変わった場合にプレビューを更新する。
func (m *Model) maybeUpdatePreview() tea.Cmd {
	sel := m.selectedSession()
	if sel == nil || sel.session == nil {
		m.previewPaneID = ""
		m.previewText = ""
		return nil
	}

	paneID := sel.session.PaneID
	if paneID == "" || paneID == m.previewPaneID {
		return nil
	}

	m.previewPaneID = paneID
	m.previewLoading = true
	return fetchPreviewCmd(m.terminal, paneID)
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
		// サブメニュー経由のジャンプは同期実行（通常パスは JumpDoneMsg 経由の非同期）
		if err := m.terminal.FocusPane(item.PaneID); err != nil {
			m.err = err
			return m, nil
		}
		if m.exitOnJump {
			return m, tea.Quit
		}
		return m, nil
	}
	return m, nil
}

// handleSimpleApprove は単純承認（y + Enter）を送信する。
func (m Model) handleSimpleApprove() (tea.Model, tea.Cmd) {
	if !m.canApprove() {
		return m, nil
	}
	paneID := m.selectedSession().session.PaneID
	term := m.terminal
	return m, func() tea.Msg {
		err := term.SendKeys(paneID, "y", "Enter")
		return ApprovalResultMsg{Err: err, Label: "Approved"}
	}
}

// handleSimpleDeny は単純拒否（n + Enter）を送信する。
func (m Model) handleSimpleDeny() (tea.Model, tea.Cmd) {
	if !m.canApprove() {
		return m, nil
	}
	paneID := m.selectedSession().session.PaneID
	term := m.terminal
	return m, func() tea.Msg {
		err := term.SendKeys(paneID, "n", "Enter")
		return ApprovalResultMsg{Err: err, Label: "Denied"}
	}
}

// enterInputMode はプロンプト付き承認/拒否のテキスト入力モードに入る。
func (m Model) enterInputMode(mode inputMode) (tea.Model, tea.Cmd) {
	if !m.canApprove() {
		return m, nil
	}
	m.inputMode = mode
	m.textInput.Reset()
	if mode == inputApprove {
		m.textInput.Placeholder = "approval prompt..."
	} else {
		m.textInput.Placeholder = "rejection feedback..."
	}
	m.textInput.Focus()
	return m, nil
}

// updateTextInput はテキスト入力モード中のキー処理を行う。
func (m Model) updateTextInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, escKey):
		m.inputMode = inputNone
		m.textInput.Blur()
		return m, nil
	case key.Matches(msg, enterKey):
		text := m.textInput.Value()
		m.textInput.Blur()
		mode := m.inputMode
		m.inputMode = inputNone

		sel := m.selectedSession()
		if sel == nil || sel.session == nil || sel.session.PaneID == "" {
			return m, nil
		}
		// 状態が変わっていたら送信しない
		if sel.session.State != core.Waiting || sel.session.Tool != core.ToolClaude {
			return m, nil
		}
		paneID := sel.session.PaneID
		term := m.terminal

		return m, func() tea.Msg {
			var keys []string
			var label string
			if mode == inputApprove {
				keys = append(keys, "Tab")
				label = "Approved"
			} else {
				keys = append(keys, "Escape")
				label = "Denied"
			}
			if text != "" {
				keys = append(keys, text)
				label += ": " + text
			}
			keys = append(keys, "Enter")
			err := term.SendKeys(paneID, keys...)
			return ApprovalResultMsg{Err: err, Label: label}
		}
	}

	// その他のキーは textinput に委譲
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// --- 以下は v1 互換のために残す型（テストが参照） ---

// ProjectItem はプロジェクト一覧の1行を表す（後方互換）。
type ProjectItem struct {
	Project core.Project
}

func (i ProjectItem) Title() string       { return i.Project.Name }
func (i ProjectItem) Description() string { return "" }
func (i ProjectItem) FilterValue() string { return i.Project.Path }

// SessionItem はセッション一覧の1行を表す（後方互換）。
type SessionItem struct {
	Session core.Session
}

func (i SessionItem) Title() string       { return i.Session.Tool.String() }
func (i SessionItem) Description() string { return "" }
func (i SessionItem) FilterValue() string { return i.Session.ID }

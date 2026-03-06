package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yoshihiko555/baton/internal/core"
)

func TestViewContainsProjectsTitle(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// 0 サイズ描画を避けるため、先にウィンドウサイズを設定する。
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "Projects") {
		t.Error("view should contain 'Projects' title")
	}
	if !strings.Contains(view, "Sessions") {
		t.Error("view should contain 'Sessions' title")
	}
}

func TestViewContainsStatusBar(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	// セッション付きプロジェクトを投入してステータスバー表示を確認する。
	projects := []core.Project{
		{
			Path:        "/project-a",
			DisplayName: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking},
				{ID: "s2", State: core.Idle},
			},
			ActiveCount: 1,
		},
	}
	updated, _ = m.Update(StateUpdateMsg(projects))
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "Projects: 1") {
		t.Error("status bar should show project count")
	}
	if !strings.Contains(view, "Active: 1") {
		t.Error("status bar should show active count")
	}
}

func TestViewShowsErrorWhenSet(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	updated, _ = m.Update(ErrMsg(tea.ErrProgramKilled))
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "error") {
		t.Error("view should display error message")
	}
}

func TestStateStyleKnownStates(t *testing.T) {
	states := []core.SessionState{core.Idle, core.Thinking, core.ToolUse, core.Error}
	for _, s := range states {
		style := stateStyle(s)
		rendered := style.Render("test")
		if rendered == "" {
			t.Errorf("stateStyle(%v) rendered empty", s)
		}
	}
}

func TestStateStyleUnknownState(t *testing.T) {
	unknown := core.SessionState(999)
	style := stateStyle(unknown)
	rendered := style.Render("test")
	if rendered == "" {
		t.Error("stateStyle for unknown state should still render")
	}
}

func TestViewDefaultSizeWhenNoWindowSizeMsg(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// WindowSizeMsg を送らずに View() を呼ぶ。
	view := m.View()
	if view == "" {
		t.Error("View() should return non-empty string even without WindowSizeMsg")
	}
}

func TestRenderStatusBarWithLastUpdate(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	now := time.Now()
	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking, LastActivity: now},
			},
			ActiveCount: 1,
		},
	}
	updated, _ = m.Update(StateUpdateMsg(projects))
	m = updated.(Model)

	view := m.View()
	expectedTime := now.Local().Format("15:04:05")
	if !strings.Contains(view, expectedTime) {
		t.Errorf("status bar should contain last update time %q", expectedTime)
	}
}

func TestRenderStatusBarNilSessionSkipped(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				nil,
				{ID: "s1", State: core.Idle},
			},
			ActiveCount: 0,
		},
	}
	updated, _ = m.Update(StateUpdateMsg(projects))
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "Projects: 1") {
		t.Error("status bar should show project count")
	}
}

func TestViewActivePaneToggle(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	if m.activePane != 0 {
		t.Errorf("initial activePane = %d, want 0", m.activePane)
	}

	// Tab で pane 1 に切り替え後も View() がパニックしないことを確認。
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)

	if m.activePane != 1 {
		t.Errorf("activePane after tab = %d, want 1", m.activePane)
	}

	view := m.View()
	if view == "" {
		t.Error("View() should return non-empty string after pane toggle")
	}
}

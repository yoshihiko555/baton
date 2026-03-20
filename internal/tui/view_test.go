package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yoshihiko555/baton/internal/core"
)

func TestViewContainsPreviewTitle(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// 0 サイズ描画を避けるため、先にウィンドウサイズを設定する。
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "Preview") {
		t.Error("view should contain 'Preview' title")
	}
}

func TestViewContainsStatusBar(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	// セッション付きプロジェクトを投入してステータスバー表示を確認する。
	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking},
				{ID: "s2", State: core.Idle},
			},
		},
	}
	updated, _ = m.Update(ScanResultMsg{Projects: projects})
	m = updated.(Model)

	view := m.View()
	// ステータスバーは "N sessions" を含む
	if !strings.Contains(view, "sessions") {
		t.Error("status bar should show 'sessions' keyword")
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
	states := []core.SessionState{core.Idle, core.Thinking, core.ToolUse, core.Waiting, core.Error}
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
		},
	}
	updated, _ = m.Update(ScanResultMsg{Projects: projects})
	m = updated.(Model)

	view := m.View()
	// v2 ステータスバーは latestSummary ベース
	if !strings.Contains(view, "sessions") {
		t.Error("status bar should show 'sessions' keyword")
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

// ── TDD Red Phase: view rendering tests ──

func TestHeaderContainsAppNameAndSubtitle(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "baton") {
		t.Error("header should contain app name 'baton'")
	}
	if !strings.Contains(view, "AI Session Monitor") {
		t.Error("header should contain subtitle 'AI Session Monitor'")
	}
}

func TestHeaderShowsAllCountsAlways(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", PID: 100, State: core.Thinking},
				{ID: "s2", PID: 200, State: core.Idle},
			},
		},
	}
	m = feedProjects(m, projects)

	view := m.View()
	for _, keyword := range []string{"sessions", "active", "waiting", "idle"} {
		if !strings.Contains(view, keyword) {
			t.Errorf("header should always show %q keyword even when count is 0", keyword)
		}
	}
}

func TestViewShowsGroupHeaders(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", PID: 100, State: core.Thinking},
				{ID: "s2", PID: 200, State: core.Idle},
			},
		},
	}
	m = feedProjects(m, projects)

	view := m.View()
	if !strings.Contains(view, "WORKING") {
		t.Error("view should contain WORKING group header")
	}
	if !strings.Contains(view, "IDLE") {
		t.Error("view should contain IDLE group header")
	}
}

func TestPreviewShowsSessionInfo(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	projects := []core.Project{
		{
			Path: "/my-proj",
			Name: "my-proj",
			Sessions: []*core.Session{
				{
					ID:     "s1",
					PID:    12345,
					State:  core.Thinking,
					Tool:   core.ToolClaude,
					PaneID: "pane-1",
				},
			},
		},
	}
	m = feedProjects(m, projects)
	m.previewText = "some preview content"

	view := m.View()
	if !strings.Contains(view, "my-proj") {
		t.Error("preview should show project name 'my-proj'")
	}
	if !strings.Contains(view, "claude") {
		t.Error("preview should show tool name 'claude'")
	}
	if !strings.Contains(view, "12345") {
		t.Error("preview should show PID '12345'")
	}
}

func TestStatusBarShowsToolBreakdown(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	summary := core.Summary{
		TotalSessions: 3,
		Active:        3,
		Waiting:       0,
		ByTool: map[string]int{
			"claude": 2,
			"codex":  1,
		},
	}
	updated, _ = m.Update(ScanResultMsg{
		Projects: []core.Project{
			{
				Path: "/project-a",
				Name: "project-a",
				Sessions: []*core.Session{
					{ID: "s1", PID: 100, State: core.Thinking, Tool: core.ToolClaude},
					{ID: "s2", PID: 200, State: core.Thinking, Tool: core.ToolClaude},
					{ID: "s3", PID: 300, State: core.Thinking, Tool: core.ToolCodex},
				},
			},
		},
		Summary: summary,
	})
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "claude:2") {
		t.Errorf("status bar should show tool breakdown 'claude:2', view snippet: %q",
			view[:min(200, len(view))])
	}
}

func TestActionBarContainsKeybindings(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	view := m.View()
	for _, k := range []string{"j/k", "tab", "enter", "q"} {
		if !strings.Contains(view, k) {
			t.Errorf("action bar should contain keybinding %q", k)
		}
	}
}

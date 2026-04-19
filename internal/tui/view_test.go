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
	theme := deepSeaGlow()
	states := []core.SessionState{core.Idle, core.Thinking, core.ToolUse, core.Waiting, core.Error}
	for _, s := range states {
		style := stateStyle(s, theme)
		rendered := style.Render("test")
		if rendered == "" {
			t.Errorf("stateStyle(%v) rendered empty", s)
		}
	}
}

func TestStateStyleUnknownState(t *testing.T) {
	theme := deepSeaGlow()
	unknown := core.SessionState(999)
	style := stateStyle(unknown, theme)
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

func TestViewActivePaneDefault(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	view := m.View()
	if view == "" {
		t.Error("View() should return non-empty string")
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

func TestViewShowsAttentionAndProjectHeaders(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", PID: 100, State: core.Waiting, Tool: core.ToolClaude, Branch: "feature/auth"},
				{ID: "s2", PID: 200, State: core.Idle, Tool: core.ToolCodex},
			},
		},
	}
	m = feedProjects(m, projects)

	view := m.View()
	if !strings.Contains(view, "Attention") {
		t.Error("view should contain Attention title")
	}
	if !strings.Contains(view, "project-a") {
		t.Error("view should contain project header")
	}
	if !strings.Contains(view, "feature/auth") {
		t.Error("view should contain attention entry branch label")
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
	for _, k := range []string{"j/k", "enter", "q"} {
		if !strings.Contains(view, k) {
			t.Errorf("action bar should contain keybinding %q", k)
		}
	}
	if strings.Contains(view, "tab") {
		t.Error("action bar should not contain 'tab' keybinding after Tab key removal")
	}
}

// TestViewShowsFlashMessage verifies View() contains flash text when flashMessage is set.
func TestViewShowsFlashMessage(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	m.flashMessage = "Approved"

	view := m.View()
	if !strings.Contains(view, "Approved") {
		t.Error("View() should contain flash message text 'Approved'")
	}
}

// TestActionBarShowsApproveHints verifies action bar shows "approve" when canApprove() is true.
func TestActionBarShowsApproveHints(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	// Arrange: Waiting Claude session with right pane active so canApprove() returns true
	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{PID: 100, State: core.Waiting, Tool: core.ToolClaude, PaneID: "%1"},
			},
		},
	}
	m = feedProjects(m, projects)

	view := m.View()
	if !strings.Contains(view, "approve") {
		t.Error("action bar should contain 'approve' hint when canApprove() is true")
	}
}

// TestActionBarShowsPromptHints verifies action bar shows "approve+msg" when canInput() is true but not canApprove().
func TestActionBarShowsPromptHints(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	// Arrange: Idle Claude session — canInput() = true, canApprove() = false
	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{PID: 100, State: core.Idle, Tool: core.ToolClaude, PaneID: "%1"},
			},
		},
	}
	m = feedProjects(m, projects)

	view := m.View()
	if !strings.Contains(view, "approve+msg") {
		t.Error("action bar should contain 'approve+msg' hint when canInput() is true")
	}
}

func TestViewShowsFilterQuery(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	m.filterQuery = "waiting"

	view := m.View()
	if !strings.Contains(view, "Filter:") {
		t.Error("view should contain filter label")
	}
	if !strings.Contains(view, "waiting") {
		t.Error("view should contain current filter query")
	}
}

func TestViewShowsFilterInputWhileEditing(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	m.filtering = true
	m.filterInput.SetValue("!idle")
	m.filterInput.Focus()

	view := m.View()
	if !strings.Contains(view, "Filter:") {
		t.Error("view should contain filter label while editing")
	}
	if !strings.Contains(view, "!idle") {
		t.Error("view should contain filter input value while editing")
	}
}

func TestRenderAttentionShowsAtMostFiveEntries(t *testing.T) {
	m, _, _, _, _ := newTestModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(Model)

	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", PID: 101, State: core.Waiting, Tool: core.ToolClaude},
				{ID: "s2", PID: 102, State: core.Waiting, Tool: core.ToolClaude},
				{ID: "s3", PID: 103, State: core.Waiting, Tool: core.ToolClaude},
				{ID: "s4", PID: 104, State: core.Waiting, Tool: core.ToolClaude},
				{ID: "s5", PID: 105, State: core.Waiting, Tool: core.ToolClaude},
				{ID: "s6", PID: 106, State: core.Waiting, Tool: core.ToolClaude},
			},
		},
	}
	m = feedProjects(m, projects)

	lines := m.renderAttention(60, 10)
	if len(lines) != 8 {
		t.Fatalf("attention lines = %d, want 8 (title + summary + 5 items + separator)", len(lines))
	}

	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "106") {
		t.Error("attention should not render more than five alert rows")
	}
	if !strings.Contains(joined, "105") {
		t.Error("attention should include the fifth alert row")
	}
	if strings.Contains(joined, "200") {
		t.Error("attention should only render waiting sessions")
	}
}

func TestActionBarShowsWaitingShortcutWhenAttentionExists(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{PID: 100, State: core.Waiting, Tool: core.ToolClaude, PaneID: "%1"},
				{PID: 200, State: core.Idle, Tool: core.ToolCodex, PaneID: "%2"},
			},
		},
	}
	m = feedProjects(m, projects)

	view := m.View()
	if !strings.Contains(view, "w") || !strings.Contains(view, "next waiting") {
		t.Error("action bar should contain waiting shortcut when waiting sessions exist")
	}
}

func TestRenderSessionListKeepsEntriesVisibleWhenHeightIsSmall(t *testing.T) {
	m, _, _, _, _ := newTestModel()
	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", PID: 101, State: core.Waiting, Tool: core.ToolClaude},
				{ID: "s2", PID: 102, State: core.Waiting, Tool: core.ToolClaude},
				{ID: "s3", PID: 103, State: core.Waiting, Tool: core.ToolClaude},
				{ID: "s4", PID: 104, State: core.Waiting, Tool: core.ToolClaude},
				{ID: "s5", PID: 105, State: core.Waiting, Tool: core.ToolClaude},
				{ID: "s6", PID: 106, State: core.Idle, Tool: core.ToolCodex},
			},
		},
	}
	m = feedProjects(m, projects)

	rendered := m.renderSessionList(60, 6)
	if !strings.Contains(rendered, "Attention") {
		t.Fatal("rendered list should contain attention section")
	}
	if !strings.Contains(rendered, "project-a / pane") && !strings.Contains(rendered, "project-a / session") {
		t.Fatal("rendered list should still contain at least one session row when height is small")
	}
}

func TestAttentionSummaryDoesNotTreatErrorAsIdle(t *testing.T) {
	m, _, _, _, _ := newTestModel()
	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", PID: 101, State: core.Waiting, Tool: core.ToolClaude},
				{ID: "s2", PID: 102, State: core.Error, Tool: core.ToolCodex},
				{ID: "s3", PID: 103, State: core.Idle, Tool: core.ToolClaude},
			},
		},
	}
	m = feedProjects(m, projects)

	summary := renderAttentionSummary(attentionCounts(m.entries), m.theme, 80)
	if !strings.Contains(summary, "waiting 1") {
		t.Fatal("summary should count waiting sessions")
	}
	if !strings.Contains(summary, "idle 1") {
		t.Fatal("summary should count only actual idle sessions")
	}
	if strings.Contains(summary, "idle 2") {
		t.Fatal("error sessions must not be folded into idle count")
	}
}

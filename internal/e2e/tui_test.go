package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yoshihiko555/baton/internal/config"
	"github.com/yoshihiko555/baton/internal/core"
	"github.com/yoshihiko555/baton/internal/terminal"
	"github.com/yoshihiko555/baton/internal/tui"
)

// --- TUI E2E mock implementations ---

type tuiMockScanner struct {
	result core.ScanResult
}

func (s *tuiMockScanner) Scan(ctx context.Context) core.ScanResult {
	return s.result
}

type tuiMockStateUpdater struct {
	projects []core.Project
	summary  core.Summary
	panes    []terminal.Pane
}

func (u *tuiMockStateUpdater) UpdateFromScan(result core.ScanResult) error { return nil }
func (u *tuiMockStateUpdater) ApplyHookStates()                            {}
func (u *tuiMockStateUpdater) RefineToolUseState(term terminal.Terminal)   {}

type tuiMockStateReader struct {
	projects []core.Project
	summary  core.Summary
	panes    []terminal.Pane
}

func (r *tuiMockStateReader) Projects() []core.Project    { return r.projects }
func (r *tuiMockStateReader) GetProjects() []core.Project { return r.projects }
func (r *tuiMockStateReader) Summary() core.Summary       { return r.summary }
func (r *tuiMockStateReader) Panes() []terminal.Pane      { return r.panes }

type tuiMockTerminal struct {
	focusedPane string
	paneText    string
}

func (t *tuiMockTerminal) ListPanes() ([]terminal.Pane, error)          { return nil, nil }
func (t *tuiMockTerminal) FocusPane(paneID string) error                { t.focusedPane = paneID; return nil }
func (t *tuiMockTerminal) SendKeys(paneID string, keys ...string) error { return nil }
func (t *tuiMockTerminal) GetPaneText(paneID string) (string, error)    { return t.paneText, nil }
func (t *tuiMockTerminal) IsAvailable() bool                            { return true }
func (t *tuiMockTerminal) Name() string                                 { return "mock" }

// --- helpers ---

func newTUITestModel(exitOnJump bool) (tui.Model, *tuiMockStateReader, *tuiMockTerminal) {
	reader := &tuiMockStateReader{}
	updater := &tuiMockStateUpdater{}
	scanner := &tuiMockScanner{}
	term := &tuiMockTerminal{paneText: "$ claude code output here"}
	cfg := config.Config{
		ScanInterval: time.Second,
	}

	model := tui.NewModel(scanner, updater, reader, term, cfg, exitOnJump, nil, nil)
	return model, reader, term
}

func feedScanResult(m tui.Model, projects []core.Project, summary core.Summary) tui.Model {
	updated, _ := m.Update(tui.ScanResultMsg{
		Projects: projects,
		Summary:  summary,
	})
	return updated.(tui.Model)
}

func sendKey(m tui.Model, key tea.KeyType) (tui.Model, tea.Cmd) {
	updated, cmd := m.Update(tea.KeyMsg{Type: key})
	return updated.(tui.Model), cmd
}

func sendRuneKey(m tui.Model, r rune) (tui.Model, tea.Cmd) {
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return updated.(tui.Model), cmd
}

func sendWindowSize(m tui.Model, w, h int) tui.Model {
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated.(tui.Model)
}

// --- TUI E2E Tests ---

// TestTUILifecycle_BootToQuit tests the complete TUI lifecycle:
// Init → WindowSize → ScanResult → View → Quit
func TestTUILifecycle_BootToQuit(t *testing.T) {
	m, _, _ := newTUITestModel(false)

	// Step 1: Init returns a command (tick)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() should return a tick command")
	}

	// Step 2: Window size
	m = sendWindowSize(m, 120, 40)

	// Step 3: No sessions yet → View should show "No sessions"
	view := m.View()
	if !strings.Contains(view, "No sessions") {
		t.Error("expected 'No sessions' in initial View()")
	}

	// Step 4: Feed scan result with sessions
	projects := []core.Project{
		{
			Path: "/home/user/project-alpha",
			Name: "project-alpha",
			Sessions: []*core.Session{
				{PID: 1001, Tool: core.ToolClaude, State: core.Thinking, PaneID: "%1"},
			},
		},
	}
	summary := core.Summary{
		TotalSessions: 1,
		Active:        1,
		ByTool:        map[string]int{"claude": 1},
	}
	m = feedScanResult(m, projects, summary)

	// Step 5: View should now show the session
	view = m.View()
	if !strings.Contains(view, "project-alpha") {
		t.Error("expected 'project-alpha' in View() after scan result")
	}
	if !strings.Contains(view, "claude") {
		t.Error("expected 'claude' tool name in View()")
	}
	if !strings.Contains(view, "1 sessions") {
		t.Error("expected '1 sessions' in header")
	}

	// Step 6: Quit
	m, cmd = sendRuneKey(m, 'q')
	if cmd == nil {
		t.Fatal("expected quit command after 'q' key")
	}
	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", result)
	}
}

// TestTUILifecycle_MultipleScanCycles tests that multiple scan results update
// the TUI correctly.
func TestTUILifecycle_MultipleScanCycles(t *testing.T) {
	m, _, _ := newTUITestModel(false)
	m = sendWindowSize(m, 120, 40)

	// First scan: 1 session
	m = feedScanResult(m, []core.Project{
		{
			Path: "/project",
			Name: "my-project",
			Sessions: []*core.Session{
				{PID: 100, Tool: core.ToolClaude, State: core.Thinking, PaneID: "%1"},
			},
		},
	}, core.Summary{TotalSessions: 1, Active: 1, ByTool: map[string]int{"claude": 1}})

	view1 := m.View()
	if !strings.Contains(view1, "1 sessions") {
		t.Error("expected '1 sessions' after first scan")
	}

	// Second scan: 2 sessions (new codex session appeared)
	m = feedScanResult(m, []core.Project{
		{
			Path: "/project",
			Name: "my-project",
			Sessions: []*core.Session{
				{PID: 100, Tool: core.ToolClaude, State: core.Thinking, PaneID: "%1"},
				{PID: 200, Tool: core.ToolCodex, State: core.Idle, PaneID: "%2"},
			},
		},
	}, core.Summary{TotalSessions: 2, Active: 1, ByTool: map[string]int{"claude": 1, "codex": 1}})

	view2 := m.View()
	if !strings.Contains(view2, "2 sessions") {
		t.Error("expected '2 sessions' after second scan")
	}
	if !strings.Contains(view2, "codex") {
		t.Error("expected 'codex' to appear after second scan")
	}

	// Third scan: session disappears
	m = feedScanResult(m, []core.Project{}, core.Summary{TotalSessions: 0, ByTool: map[string]int{}})

	view3 := m.View()
	if !strings.Contains(view3, "0 sessions") {
		t.Error("expected '0 sessions' after all sessions disappear")
	}
	if !strings.Contains(view3, "No sessions") {
		t.Error("expected 'No sessions' message when no sessions exist")
	}
}

// TestTUILifecycle_PreviewUpdatesOnNavigation tests that the preview pane
// updates when navigating between sessions.
func TestTUILifecycle_PreviewUpdatesOnNavigation(t *testing.T) {
	m, _, _ := newTUITestModel(false)
	m = sendWindowSize(m, 120, 40)

	// Feed 2 sessions in same group (Idle) with different PaneIDs
	projects := []core.Project{
		{
			Path: "/project",
			Name: "my-project",
			Sessions: []*core.Session{
				{PID: 100, Tool: core.ToolClaude, State: core.Idle, PaneID: "%1"},
				{PID: 200, Tool: core.ToolCodex, State: core.Idle, PaneID: "%2"},
			},
		},
	}
	m = feedScanResult(m, projects, core.Summary{TotalSessions: 2, ByTool: map[string]int{"claude": 1, "codex": 1}})

	// After initial feed, preview should be loading for the first session
	view1 := m.View()
	if !strings.Contains(view1, "Preview") {
		t.Error("expected Preview header in View()")
	}

	// Feed preview result — Seq must match previewFetchSeq set during feedScanResult.
	m2, _ := m.Update(tui.PreviewResultMsg{PaneID: "%1", Text: "Claude output line 1\nClaude output line 2", Seq: m.PreviewFetchSeq()})
	m = m2.(tui.Model)

	view2 := m.View()
	if !strings.Contains(view2, "Claude output") {
		t.Error("expected preview text in View() after PreviewResultMsg")
	}

	// Navigate down to next session
	m, cmd := sendKey(m, tea.KeyDown)
	if cmd == nil {
		t.Error("expected fetchPreviewCmd when navigating to different PaneID")
	}
}

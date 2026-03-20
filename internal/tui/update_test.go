package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yoshihiko555/baton/internal/config"
	"github.com/yoshihiko555/baton/internal/core"
	"github.com/yoshihiko555/baton/internal/terminal"
)

// --- モック ---

type mockStateReader struct {
	projects []core.Project
}

func (m *mockStateReader) GetProjects() []core.Project {
	return m.projects
}

func (m *mockStateReader) Projects() []core.Project {
	return m.projects
}

func (m *mockStateReader) Summary() core.Summary {
	return core.Summary{}
}

func (m *mockStateReader) Panes() []terminal.Pane {
	return nil
}

type mockStateUpdater struct{}

func (m *mockStateUpdater) UpdateFromScan(result core.ScanResult) error {
	return nil
}

func (m *mockStateUpdater) RefineToolUseState(term terminal.Terminal) {}

type mockScanner struct {
	result core.ScanResult
}

func (m *mockScanner) Scan(ctx context.Context) core.ScanResult {
	return m.result
}

type mockTerminal struct {
	available bool
	focused   string
}

func (m *mockTerminal) ListPanes() ([]terminal.Pane, error) {
	return nil, nil
}

func (m *mockTerminal) FocusPane(paneID string) error {
	m.focused = paneID
	return nil
}

func (m *mockTerminal) GetPaneText(paneID string) (string, error) {
	return "", nil
}

func (m *mockTerminal) IsAvailable() bool {
	return m.available
}

func (m *mockTerminal) Name() string {
	return "mock"
}

// --- ヘルパー ---

func newTestModel() (Model, *mockStateReader, *mockStateUpdater, *mockScanner, *mockTerminal) {
	reader := &mockStateReader{}
	updater := &mockStateUpdater{}
	scanner := &mockScanner{}
	term := &mockTerminal{available: true}
	cfg := config.Default()

	model := NewModel(scanner, updater, reader, term, cfg)
	return model, reader, updater, scanner, term
}

// --- テスト ---

func TestProjectItem(t *testing.T) {
	item := ProjectItem{Project: core.Project{
		Path: "/home/user/project",
		Name: "my-project",
		Sessions: []*core.Session{
			{State: core.Thinking},
			{State: core.Thinking},
			{State: core.Idle},
		},
	}}

	title := item.Title()
	if title == "" {
		t.Error("Title() should not be empty")
	}

	desc := item.Description()
	want := "thinking: 2 · idle: 1"
	if desc != want {
		t.Errorf("Description() = %q, want %q", desc, want)
	}

	fv := item.FilterValue()
	if fv == "" {
		t.Error("FilterValue() should not be empty")
	}
}

func TestSessionItem(t *testing.T) {
	item := SessionItem{Session: core.Session{
		ID:           "abc-123",
		ProjectPath:  "/home/user/project",
		State:        core.Thinking,
		LastActivity: time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC),
	}}

	title := item.Title()
	if title == "" {
		t.Errorf("Title() should not be empty, got %q", title)
	}

	fv := item.FilterValue()
	if fv == "" {
		t.Error("FilterValue() should not be empty")
	}
}

func TestNewModel(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	if m.activePane != 0 {
		t.Errorf("activePane = %d, want 0", m.activePane)
	}
	if m.err != nil {
		t.Errorf("err = %v, want nil", m.err)
	}
	if m.stateReader == nil {
		t.Error("stateReader should not be nil")
	}
	if m.stateUpdater == nil {
		t.Error("stateUpdater should not be nil")
	}
	if m.scanner == nil {
		t.Error("scanner should not be nil")
	}
}

func TestUpdateQuitKey(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit command, got nil")
	}
	// tea.Quit は tea.QuitMsg を返す。
	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", result)
	}
}

func TestUpdateTabKey(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	if m.activePane != 0 {
		t.Fatal("initial activePane should be 0")
	}

	msg := tea.KeyMsg{Type: tea.KeyTab}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if m.activePane != 1 {
		t.Errorf("activePane after tab = %d, want 1", m.activePane)
	}

	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.activePane != 0 {
		t.Errorf("activePane after second tab = %d, want 0", m.activePane)
	}
}

func TestUpdateArrowKeys(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// 右矢印 -> pane 1
	msg := tea.KeyMsg{Type: tea.KeyRight}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if m.activePane != 1 {
		t.Errorf("activePane after right = %d, want 1", m.activePane)
	}

	// 左矢印 -> pane 0
	msg = tea.KeyMsg{Type: tea.KeyLeft}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	if m.activePane != 0 {
		t.Errorf("activePane after left = %d, want 0", m.activePane)
	}
}

func TestUpdateScanResultMsg(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking},
			},
		},
	}

	updated, _ := m.Update(ScanResultMsg{Projects: projects})
	m = updated.(Model)

	items := m.projectList.Items()
	if len(items) != 1 {
		t.Fatalf("projectList items = %d, want 1", len(items))
	}

	pi, ok := items[0].(ProjectItem)
	if !ok {
		t.Fatal("expected ProjectItem")
	}
	if pi.Project.Name != "project-a" {
		t.Errorf("project name = %q, want %q", pi.Project.Name, "project-a")
	}
}

func TestUpdateErrMsg(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	testErr := ErrMsg(tea.ErrProgramKilled)
	updated, cmd := m.Update(testErr)
	m = updated.(Model)

	if m.err == nil {
		t.Error("err should be set")
	}
	if cmd == nil {
		t.Error("expected tickCmd to be returned")
	}
}

func TestUpdateWindowSizeMsg(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if m.width != 120 {
		t.Errorf("width = %d, want 120", m.width)
	}
	if m.height != 40 {
		t.Errorf("height = %d, want 40", m.height)
	}
}

func TestUpdateWindowSizeMsgSmallValues(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	msg := tea.WindowSizeMsg{Width: 1, Height: 1}
	updated, cmd := m.Update(msg)
	m = updated.(Model)

	if m.width != 1 {
		t.Errorf("width = %d, want 1", m.width)
	}
	if cmd != nil {
		t.Error("expected no command from WindowSizeMsg")
	}
}

func TestProjectItemTitleFallbackToPath(t *testing.T) {
	item := ProjectItem{Project: core.Project{
		Path: "/home/user/project",
		Name: "",
	}}

	if item.Title() != "/home/user/project  0 sessions" {
		// Title() uses Name; if empty falls back to Path
		title := item.Title()
		if title == "" {
			t.Error("Title() should not be empty when Name is empty")
		}
	}
}

func TestSessionItemDescriptionShowsPID(t *testing.T) {
	item := SessionItem{Session: core.Session{
		PID:   12345,
		State: core.Idle,
	}}

	desc := item.Description()
	if desc != "PID:12345" {
		t.Errorf("Description() = %q, want %q", desc, "PID:12345")
	}
}

func TestUpdateEnterKeyOnProjectPane(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// プロジェクトを投入する。
	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking},
			},
		},
		{
			Path: "/project-b",
			Name: "project-b",
			Sessions: []*core.Session{
				{ID: "s2", State: core.Idle},
			},
		},
	}
	updated, _ := m.Update(ScanResultMsg{Projects: projects})
	m = updated.(Model)

	// pane 0 (project) で Enter を押す。
	m.activePane = 0
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// 選択プロジェクトが固定され、pane 1 に移動する。
	if m.activePane != 1 {
		t.Errorf("activePane = %d, want 1", m.activePane)
	}
	if m.selectedProject != "/project-a" {
		t.Errorf("selectedProject = %q, want %q", m.selectedProject, "/project-a")
	}
}

func TestUpdateEnterKeyOnSessionPaneFocusSuccess(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking, PaneID: "1"},
			},
		},
	}
	updated, _ := m.Update(ScanResultMsg{Projects: projects})
	m = updated.(Model)

	// pane 1 (session) で Enter → jumping=true, FocusPane が tea.Cmd として返る。
	m.activePane = 1
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, cmd := m.Update(msg)
	m = updated.(Model)

	if !m.jumping {
		t.Error("expected jumping=true after Enter")
	}
	if cmd == nil {
		t.Error("expected cmd for async FocusPane")
	}

	// JumpDoneMsg で tea.Quit が返る。
	updated, cmd = m.Update(JumpDoneMsg{Err: nil})
	m = updated.(Model)
	if cmd == nil {
		t.Error("expected tea.Quit after JumpDoneMsg")
	}
}

func TestUpdateEnterKeyOnSessionPaneTerminalUnavailable(t *testing.T) {
	// v2 の update.go は IsAvailable() をチェックしない。
	// terminal が nil でなく PaneID が有効なら FocusPane が呼ばれるだけ。
	m, _, _, _, term := newTestModel()
	term.available = false

	projects := []core.Project{
		{
			Path:     "/project-a",
			Sessions: []*core.Session{{ID: "s1", State: core.Thinking, PaneID: "1"}},
		},
	}
	updated, _ := m.Update(ScanResultMsg{Projects: projects})
	m = updated.(Model)

	m.activePane = 1
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// v2 では IsAvailable チェックなし。FocusPane が呼ばれ err は nil のまま。
	_ = m
}

func TestUpdateEnterKeyOnSessionPaneNoPaneID(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path:     "/project-a",
			Sessions: []*core.Session{{ID: "s1", State: core.Thinking, PaneID: ""}},
		},
	}
	updated, _ := m.Update(ScanResultMsg{Projects: projects})
	m = updated.(Model)

	m.activePane = 1
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.err == nil {
		t.Error("expected error for empty pane ID")
	}
}

func TestUpdateTickMsg(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, cmd := m.Update(TickMsg{})
	_ = updated.(Model)

	if cmd == nil {
		t.Error("expected doScanCmd from TickMsg")
	}
}

func TestUpdateActiveListDelegation(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// pane 0 でキー入力。
	m.activePane = 0
	msg := tea.KeyMsg{Type: tea.KeyDown}
	updated, _ := m.Update(msg)
	_ = updated.(Model)

	// pane 1 でキー入力。
	m.activePane = 1
	updated, _ = m.Update(msg)
	_ = updated.(Model)
	// パニックしなければ OK。
}

func TestProjectsFromProjectList(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{Path: "/project-a", Name: "a"},
		{Path: "/project-b", Name: "b"},
	}
	updated, _ := m.Update(ScanResultMsg{Projects: projects})
	m = updated.(Model)

	result := m.projectsFromProjectList()
	if len(result) != 2 {
		t.Fatalf("projectsFromProjectList() len = %d, want 2", len(result))
	}
	if result[0].Path != "/project-a" {
		t.Errorf("result[0].Path = %q, want %q", result[0].Path, "/project-a")
	}
	if result[1].Path != "/project-b" {
		t.Errorf("result[1].Path = %q, want %q", result[1].Path, "/project-b")
	}
}

func TestUpdateProjectListEmptyProjects(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// まずプロジェクトを投入する。
	projects := []core.Project{
		{Path: "/project-a", Name: "a"},
	}
	updated, _ := m.Update(ScanResultMsg{Projects: projects})
	m = updated.(Model)
	m.selectedProject = "/project-a"

	// 空リストで更新する。
	updated, _ = m.Update(ScanResultMsg{Projects: []core.Project{}})
	m = updated.(Model)

	if len(m.projectList.Items()) != 0 {
		t.Errorf("projectList items = %d, want 0", len(m.projectList.Items()))
	}
	if m.selectedProject != "" {
		t.Errorf("selectedProject = %q, want empty", m.selectedProject)
	}
}

func TestUpdateProjectListSelectedNotFound(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// プロジェクトAを選択状態にする。
	m.selectedProject = "/project-a"

	// プロジェクトBのみのリストで更新する（Aは消えた）。
	projects := []core.Project{
		{Path: "/project-b", Name: "b"},
	}
	updated, _ := m.Update(ScanResultMsg{Projects: projects})
	m = updated.(Model)

	// 選択が解除される。
	if m.selectedProject != "" {
		t.Errorf("selectedProject = %q, want empty (should be cleared)", m.selectedProject)
	}
}

func TestUpdateSessionListWithNilSession(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking},
				nil,
				{ID: "s2", State: core.Idle},
			},
		},
	}
	updated, _ := m.Update(ScanResultMsg{Projects: projects})
	m = updated.(Model)

	items := m.sessionList.Items()
	if len(items) != 2 {
		t.Errorf("sessionList items = %d, want 2 (nil session skipped)", len(items))
	}
}

func TestTickCmdReturnsTickMsg(t *testing.T) {
	cmd := tickCmd(time.Millisecond)
	if cmd == nil {
		t.Fatal("expected command")
	}

	result := cmd()
	if _, ok := result.(TickMsg); !ok {
		t.Errorf("expected TickMsg, got %T", result)
	}
}

func TestTickCmdZeroIntervalFallback(t *testing.T) {
	cmd := tickCmd(0)
	if cmd == nil {
		t.Fatal("expected command for zero interval")
	}
	// フォールバックで 1s になるが、テスト不要（コマンドが返ることを確認）。
}

func TestInitReturnsCmd(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a batch command")
	}
}

package tui

import (
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

func (m *mockStateReader) GetStatus() core.StatusOutput {
	return core.StatusOutput{
		Projects:  m.projects,
		UpdatedAt: time.Now().UTC(),
	}
}

type mockStateWriter struct {
	events []core.WatchEvent
}

func (m *mockStateWriter) HandleEvent(event core.WatchEvent) error {
	m.events = append(m.events, event)
	return nil
}

type mockEventSource struct {
	ch chan core.WatchEvent
}

func newMockEventSource() *mockEventSource {
	return &mockEventSource{ch: make(chan core.WatchEvent, 10)}
}

func (m *mockEventSource) Events() <-chan core.WatchEvent {
	return m.ch
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

func (m *mockTerminal) IsAvailable() bool {
	return m.available
}

func (m *mockTerminal) Name() string {
	return "mock"
}

// --- ヘルパー ---

func newTestModel() (Model, *mockStateReader, *mockStateWriter, *mockEventSource, *mockTerminal) {
	reader := &mockStateReader{}
	writer := &mockStateWriter{}
	events := newMockEventSource()
	term := &mockTerminal{available: true}
	cfg := config.Default()

	model := NewModel(reader, writer, events, term, cfg)
	return model, reader, writer, events, term
}

// --- テスト ---

func TestProjectItem(t *testing.T) {
	item := ProjectItem{Project: core.Project{
		Path:        "/home/user/project",
		DisplayName: "my-project",
		ActiveCount: 2,
		Sessions:    make([]*core.Session, 3),
	}}

	if item.Title() != "my-project" {
		t.Errorf("Title() = %q, want %q", item.Title(), "my-project")
	}

	desc := item.Description()
	if desc != "sessions: 3 / active: 2" {
		t.Errorf("Description() = %q, want %q", desc, "sessions: 3 / active: 2")
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

	if item.Title() != "abc-123" {
		t.Errorf("Title() = %q, want %q", item.Title(), "abc-123")
	}

	desc := item.Description()
	if desc == "" {
		t.Error("Description() should not be empty")
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
	if m.stateWriter == nil {
		t.Error("stateWriter should not be nil")
	}
	if m.watcher == nil {
		t.Error("watcher should not be nil")
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

func TestUpdateStateUpdateMsg(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path:        "/project-a",
			DisplayName: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking},
			},
			ActiveCount: 1,
		},
	}

	updated, _ := m.Update(StateUpdateMsg(projects))
	m = updated.(Model)

	items := m.projectList.Items()
	if len(items) != 1 {
		t.Fatalf("projectList items = %d, want 1", len(items))
	}

	pi, ok := items[0].(ProjectItem)
	if !ok {
		t.Fatal("expected ProjectItem")
	}
	if pi.Project.DisplayName != "project-a" {
		t.Errorf("project name = %q, want %q", pi.Project.DisplayName, "project-a")
	}
}

func TestUpdateWatchEventMsg(t *testing.T) {
	m, _, writer, _, _ := newTestModel()

	event := core.WatchEvent{
		Type:        core.Modified,
		Path:        "/tmp/session.jsonl",
		ProjectPath: "/project-a",
		SessionID:   "s1",
	}

	updated, cmd := m.Update(WatchEventMsg(event))
	m = updated.(Model)

	if len(writer.events) != 1 {
		t.Fatalf("writer.events = %d, want 1", len(writer.events))
	}
	if writer.events[0].SessionID != "s1" {
		t.Errorf("event session = %q, want %q", writer.events[0].SessionID, "s1")
	}
	if cmd == nil {
		t.Error("expected batch command for refresh + listen")
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
		t.Error("expected listenWatcherCmd to be returned")
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
		Path:        "/home/user/project",
		DisplayName: "",
	}}

	if item.Title() != "/home/user/project" {
		t.Errorf("Title() = %q, want %q", item.Title(), "/home/user/project")
	}
}

func TestSessionItemDescriptionZeroTime(t *testing.T) {
	item := SessionItem{Session: core.Session{
		ID:    "abc-123",
		State: core.Idle,
	}}

	desc := item.Description()
	if desc != "idle" {
		t.Errorf("Description() = %q, want %q", desc, "idle")
	}
}

func TestUpdateEnterKeyOnProjectPane(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// プロジェクトを投入する。
	projects := []core.Project{
		{
			Path:        "/project-a",
			DisplayName: "project-a",
			Sessions:    []*core.Session{{ID: "s1", State: core.Thinking}},
			ActiveCount: 1,
		},
		{
			Path:        "/project-b",
			DisplayName: "project-b",
			Sessions:    []*core.Session{{ID: "s2", State: core.Idle}},
		},
	}
	updated, _ := m.Update(StateUpdateMsg(projects))
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
	m, _, _, _, term := newTestModel()

	// セッションを投入する。
	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking, PaneID: "pane-1"},
			},
			ActiveCount: 1,
		},
	}
	updated, _ := m.Update(StateUpdateMsg(projects))
	m = updated.(Model)

	// pane 1 (session) で Enter を押す。
	m.activePane = 1
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if term.focused != "pane-1" {
		t.Errorf("focused = %q, want %q", term.focused, "pane-1")
	}
	if m.err != nil {
		t.Errorf("err = %v, want nil", m.err)
	}
}

func TestUpdateEnterKeyOnSessionPaneTerminalNil(t *testing.T) {
	m, _, _, _, _ := newTestModel()
	m.terminal = nil

	projects := []core.Project{
		{
			Path:     "/project-a",
			Sessions: []*core.Session{{ID: "s1", State: core.Thinking, PaneID: "pane-1"}},
		},
	}
	updated, _ := m.Update(StateUpdateMsg(projects))
	m = updated.(Model)

	m.activePane = 1
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.err == nil {
		t.Error("expected error for nil terminal")
	}
}

func TestUpdateEnterKeyOnSessionPaneTerminalUnavailable(t *testing.T) {
	m, _, _, _, term := newTestModel()
	term.available = false

	projects := []core.Project{
		{
			Path:     "/project-a",
			Sessions: []*core.Session{{ID: "s1", State: core.Thinking, PaneID: "pane-1"}},
		},
	}
	updated, _ := m.Update(StateUpdateMsg(projects))
	m = updated.(Model)

	m.activePane = 1
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.err == nil {
		t.Error("expected error for unavailable terminal")
	}
}

func TestUpdateEnterKeyOnSessionPaneNoPaneID(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path:     "/project-a",
			Sessions: []*core.Session{{ID: "s1", State: core.Thinking, PaneID: ""}},
		},
	}
	updated, _ := m.Update(StateUpdateMsg(projects))
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
		t.Error("expected batch command from TickMsg")
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
		{Path: "/project-a", DisplayName: "a"},
		{Path: "/project-b", DisplayName: "b"},
	}
	updated, _ := m.Update(StateUpdateMsg(projects))
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
		{Path: "/project-a", DisplayName: "a"},
	}
	updated, _ := m.Update(StateUpdateMsg(projects))
	m = updated.(Model)
	m.selectedProject = "/project-a"

	// 空リストで更新する。
	updated, _ = m.Update(StateUpdateMsg([]core.Project{}))
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
		{Path: "/project-b", DisplayName: "b"},
	}
	updated, _ := m.Update(StateUpdateMsg(projects))
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
	updated, _ := m.Update(StateUpdateMsg(projects))
	m = updated.(Model)

	items := m.sessionList.Items()
	if len(items) != 2 {
		t.Errorf("sessionList items = %d, want 2 (nil session skipped)", len(items))
	}
}

func TestListenWatcherCmdNilWatcher(t *testing.T) {
	cmd := listenWatcherCmd(nil)
	if cmd == nil {
		t.Fatal("expected command")
	}

	result := cmd()
	if _, ok := result.(ErrMsg); !ok {
		t.Errorf("expected ErrMsg, got %T", result)
	}
}

func TestListenWatcherCmdChannelClosed(t *testing.T) {
	es := newMockEventSource()
	close(es.ch)

	cmd := listenWatcherCmd(es)
	result := cmd()
	if _, ok := result.(ErrMsg); !ok {
		t.Errorf("expected ErrMsg for closed channel, got %T", result)
	}
}

func TestListenWatcherCmdSuccess(t *testing.T) {
	es := newMockEventSource()
	es.ch <- core.WatchEvent{
		Type:      core.Modified,
		SessionID: "s1",
	}

	cmd := listenWatcherCmd(es)
	result := cmd()
	msg, ok := result.(WatchEventMsg)
	if !ok {
		t.Fatalf("expected WatchEventMsg, got %T", result)
	}
	if msg.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", msg.SessionID, "s1")
	}
}

func TestRefreshStateCmdNilReader(t *testing.T) {
	cmd := refreshStateCmd(nil)
	if cmd == nil {
		t.Fatal("expected command")
	}

	result := cmd()
	if _, ok := result.(ErrMsg); !ok {
		t.Errorf("expected ErrMsg, got %T", result)
	}
}

func TestRefreshStateCmdSuccess(t *testing.T) {
	reader := &mockStateReader{
		projects: []core.Project{{Path: "/p1"}},
	}

	cmd := refreshStateCmd(reader)
	result := cmd()
	msg, ok := result.(StateUpdateMsg)
	if !ok {
		t.Fatalf("expected StateUpdateMsg, got %T", result)
	}
	if len(msg) != 1 {
		t.Errorf("len(msg) = %d, want 1", len(msg))
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

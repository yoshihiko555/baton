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

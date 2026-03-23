package tui

import (
	"context"
	"fmt"
	"strings"
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
	paneText  string
	sentKeys  []string
}

func (m *mockTerminal) ListPanes() ([]terminal.Pane, error) {
	return nil, nil
}

func (m *mockTerminal) FocusPane(paneID string) error {
	m.focused = paneID
	return nil
}

func (m *mockTerminal) GetPaneText(paneID string) (string, error) {
	return m.paneText, nil
}

func (m *mockTerminal) IsAvailable() bool {
	return m.available
}

func (m *mockTerminal) SendKeys(paneID string, keys ...string) error {
	m.sentKeys = append(m.sentKeys, keys...)
	return nil
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

	model := NewModel(scanner, updater, reader, term, cfg, false)
	return model, reader, updater, scanner, term
}

// feedProjects はテスト用にプロジェクトをモデルに投入する。
func feedProjects(m Model, projects []core.Project) Model {
	updated, _ := m.Update(ScanResultMsg{Projects: projects})
	return updated.(Model)
}

// --- テスト ---

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

func TestUpdateScanResultMsg(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking, PID: 100},
			},
		},
	}

	m = feedProjects(m, projects)

	// entries にはヘッダー + セッション行が含まれるはず
	sessionCount := 0
	for _, e := range m.entries {
		if !e.isHeader {
			sessionCount++
		}
	}
	if sessionCount != 1 {
		t.Fatalf("session entries = %d, want 1", sessionCount)
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

func TestUpdateEnterKeyJumpSuccess(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking, PaneID: "1", PID: 100},
			},
		},
	}
	m = feedProjects(m, projects)

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, cmd := m.Update(msg)
	m = updated.(Model)

	if !m.jumping {
		t.Error("expected jumping=true after Enter")
	}
	if cmd == nil {
		t.Error("expected cmd for async FocusPane")
	}

	// デフォルト（exitOnJump=false）: JumpDoneMsg 後 TUI に戻る
	updated, cmd = m.Update(JumpDoneMsg{Err: nil})
	m = updated.(Model)
	if cmd != nil {
		t.Error("expected nil cmd (no quit) when exitOnJump=false")
	}
	if m.jumping {
		t.Error("expected jumping=false after JumpDoneMsg")
	}
}

func TestJumpDoneExitOnJumpTrue(t *testing.T) {
	m, _, _, _, _ := newTestModel()
	m.exitOnJump = true

	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking, PaneID: "1", PID: 100},
			},
		},
	}
	m = feedProjects(m, projects)

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, cmd := m.Update(msg)
	m = updated.(Model)

	if !m.jumping {
		t.Error("expected jumping=true after Enter")
	}
	if cmd == nil {
		t.Fatal("expected cmd for async FocusPane")
	}

	// exitOnJump=true: JumpDoneMsg 後 tea.Quit が返る
	updated, cmd = m.Update(JumpDoneMsg{Err: nil})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected tea.Quit after JumpDoneMsg with exitOnJump=true")
	}
	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", result)
	}
}

func TestJumpDoneErrorIgnoresExitOnJump(t *testing.T) {
	m, _, _, _, _ := newTestModel()
	m.exitOnJump = true

	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking, PaneID: "1", PID: 100},
			},
		},
	}
	m = feedProjects(m, projects)

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	// エラー時は exitOnJump に関係なく jumping=false + エラー設定
	updated, cmd := m.Update(JumpDoneMsg{Err: fmt.Errorf("focus failed")})
	m = updated.(Model)

	if cmd != nil {
		t.Error("expected nil cmd on JumpDoneMsg error")
	}
	if m.jumping {
		t.Error("expected jumping=false on JumpDoneMsg error")
	}
	if m.err == nil {
		t.Error("expected err to be set on JumpDoneMsg error")
	}
}

func TestSubMenuEnterExitOnJumpFalse(t *testing.T) {
	m, _, _, _, term := newTestModel()

	// サブメニュー状態をセットアップ
	m.showSubMenu = true
	m.subMenuItems = []SubMenuItem{{PaneID: "%5", TTYName: "pane %5"}}
	m.subMenuCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, cmd := m.Update(msg)
	m = updated.(Model)

	// exitOnJump=false（デフォルト）: FocusPane 実行後 TUI に戻る
	if cmd != nil {
		t.Error("expected nil cmd (no quit) when exitOnJump=false in submenu")
	}
	if m.showSubMenu {
		t.Error("expected showSubMenu=false after submenu Enter")
	}
	if term.focused != "%5" {
		t.Errorf("expected focused pane %%5, got %q", term.focused)
	}
}

func TestSubMenuEnterExitOnJumpTrue(t *testing.T) {
	m, _, _, _, term := newTestModel()
	m.exitOnJump = true

	// サブメニュー状態をセットアップ
	m.showSubMenu = true
	m.subMenuItems = []SubMenuItem{{PaneID: "%5", TTYName: "pane %5"}}
	m.subMenuCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, cmd := m.Update(msg)
	m = updated.(Model)

	// exitOnJump=true: FocusPane 実行後 tea.Quit が返る
	if cmd == nil {
		t.Fatal("expected tea.Quit after submenu Enter with exitOnJump=true")
	}
	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", result)
	}
	if term.focused != "%5" {
		t.Errorf("expected focused pane %%5, got %q", term.focused)
	}
}

func TestUpdateEnterKeyNoPaneID(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path:     "/project-a",
			Sessions: []*core.Session{{ID: "s1", State: core.Thinking, PaneID: "", PID: 100}},
		},
	}
	m = feedProjects(m, projects)

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ := m.Update(msg)
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

func TestMoveCursorSkipsHeaders(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking, PID: 100},
				{ID: "s2", State: core.Idle, PID: 200},
			},
		},
	}
	m = feedProjects(m, projects)

	// カーソルはヘッダーではなくセッション行にあるはず
	sel := m.selectedSession()
	if sel == nil {
		t.Fatal("expected a selected session")
	}
	if sel.isHeader {
		t.Error("cursor should not be on a header")
	}

	// 下に移動
	msg := tea.KeyMsg{Type: tea.KeyDown}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	sel = m.selectedSession()
	if sel == nil {
		t.Fatal("expected a selected session after move")
	}
	if sel.isHeader {
		t.Error("cursor should skip headers")
	}
}

func TestBuildEntriesGrouping(t *testing.T) {
	projects := []core.Project{
		{
			Path: "/p1",
			Name: "p1",
			Sessions: []*core.Session{
				{PID: 1, State: core.Thinking},
				{PID: 2, State: core.Idle},
				{PID: 3, State: core.Waiting},
				{PID: 4, State: core.ToolUse},
			},
		},
	}

	entries := buildEntries(projects)

	// グループ順: WAITING, WORKING (Thinking+ToolUse), IDLE
	headerOrder := []string{}
	for _, e := range entries {
		if e.isHeader {
			headerOrder = append(headerOrder, e.header)
		}
	}

	if len(headerOrder) != 3 {
		t.Fatalf("expected 3 group headers, got %d: %v", len(headerOrder), headerOrder)
	}
	if headerOrder[0] != "WAITING" {
		t.Errorf("first group = %q, want WAITING", headerOrder[0])
	}
	if headerOrder[1] != "WORKING" {
		t.Errorf("second group = %q, want WORKING", headerOrder[1])
	}
	if headerOrder[2] != "IDLE" {
		t.Errorf("third group = %q, want IDLE", headerOrder[2])
	}
}

func TestBuildEntriesNilSessionSkipped(t *testing.T) {
	projects := []core.Project{
		{
			Path: "/p1",
			Sessions: []*core.Session{
				{PID: 1, State: core.Thinking},
				nil,
				{PID: 2, State: core.Idle},
			},
		},
	}

	entries := buildEntries(projects)
	sessionCount := 0
	for _, e := range entries {
		if !e.isHeader {
			sessionCount++
		}
	}
	if sessionCount != 2 {
		t.Errorf("session entries = %d, want 2 (nil skipped)", sessionCount)
	}
}

func TestBuildEntriesEmpty(t *testing.T) {
	entries := buildEntries(nil)
	if len(entries) != 0 {
		t.Errorf("entries = %d, want 0 for nil projects", len(entries))
	}
}

func TestRebuildEntriesPreservesCursor(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path: "/p1",
			Name: "p1",
			Sessions: []*core.Session{
				{PID: 100, State: core.Thinking},
				{PID: 200, State: core.Idle},
			},
		},
	}
	m = feedProjects(m, projects)

	// カーソルを2番目のセッション（PID:200）に移動
	msg := tea.KeyMsg{Type: tea.KeyDown}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	sel := m.selectedSession()
	if sel == nil || sel.session == nil {
		t.Fatal("expected selected session")
	}
	pid := sel.session.PID

	// 再スキャンでリビルド（同じデータ）
	m = feedProjects(m, projects)

	// カーソルが同じ PID を指しているか
	sel2 := m.selectedSession()
	if sel2 == nil || sel2.session == nil {
		t.Fatal("expected selected session after rebuild")
	}
	if sel2.session.PID != pid {
		t.Errorf("cursor PID = %d, want %d (cursor should be preserved)", sel2.session.PID, pid)
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
}

func TestInitReturnsCmd(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a command")
	}
}

func TestPreviewResultMsg(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(PreviewResultMsg{Text: "hello world", Err: nil})
	m = updated.(Model)

	if m.previewText != "hello world" {
		t.Errorf("previewText = %q, want %q", m.previewText, "hello world")
	}
	if m.previewLoading {
		t.Error("previewLoading should be false after result")
	}
}

func TestSessionDisplayName(t *testing.T) {
	e := &sessionEntry{
		project: &core.Project{Path: "/home/user/my-project", Name: ""},
		session: &core.Session{},
	}
	name := sessionDisplayName(e)
	if name != "my-project" {
		t.Errorf("sessionDisplayName = %q, want %q", name, "my-project")
	}

	e.project.Name = "custom-name"
	name = sessionDisplayName(e)
	if name != "custom-name" {
		t.Errorf("sessionDisplayName = %q, want %q", name, "custom-name")
	}
}

func TestProjectItemCompat(t *testing.T) {
	item := ProjectItem{Project: core.Project{
		Path: "/home/user/project",
		Name: "my-project",
	}}

	if item.Title() != "my-project" {
		t.Errorf("Title() = %q, want %q", item.Title(), "my-project")
	}
	if item.FilterValue() != "/home/user/project" {
		t.Errorf("FilterValue() = %q, want %q", item.FilterValue(), "/home/user/project")
	}
}

func TestSessionItemCompat(t *testing.T) {
	item := SessionItem{Session: core.Session{
		ID:    "abc-123",
		State: core.Thinking,
		Tool:  core.ToolClaude,
	}}

	if item.Title() != "claude" {
		t.Errorf("Title() = %q, want %q", item.Title(), "claude")
	}
	if item.FilterValue() != "abc-123" {
		t.Errorf("FilterValue() = %q, want %q", item.FilterValue(), "abc-123")
	}
}

// C1: カーソルが別セッションに移動したときプレビューが更新される
func TestPreviewUpdatesOnCursorChange(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// 同じ状態グループ（Idle）に2セッション、異なる PaneID
	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Idle, PaneID: "%1", PID: 100},
				{ID: "s2", State: core.Idle, PaneID: "%2", PID: 200},
			},
		},
	}
	m = feedProjects(m, projects)

	// 投入後、最初のセッションが選択され previewPaneID がセットされているはず
	if m.previewPaneID == "" {
		t.Fatal("previewPaneID should be set after feeding projects with PaneID")
	}
	firstPaneID := m.previewPaneID

	// カーソルを下に移動（第2セッションへ）
	msg := tea.KeyMsg{Type: tea.KeyDown}
	updated, cmd := m.Update(msg)
	m = updated.(Model)

	// fetchPreviewCmd が返るはず（PaneID が変わったため）
	if cmd == nil {
		t.Error("expected fetchPreviewCmd to be returned when cursor moves to different PaneID")
	}

	// previewPaneID が第2セッションに切り替わっているはず
	if m.previewPaneID == firstPaneID {
		t.Errorf("previewPaneID = %q, want different pane (was %q)", m.previewPaneID, firstPaneID)
	}
	if m.previewPaneID != "%2" {
		t.Errorf("previewPaneID = %q, want %%2", m.previewPaneID)
	}
}

// C2: 同じ PaneID のままカーソルが動かないとき再フェッチしない
func TestPreviewNotRefetchedForSamePaneID(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Idle, PaneID: "%1", PID: 100},
			},
		},
	}
	m = feedProjects(m, projects)

	if m.previewPaneID != "%1" {
		t.Fatalf("previewPaneID = %q, want %%1", m.previewPaneID)
	}

	// セッションが1件なのでカーソルは動かない → 同じ PaneID
	msg := tea.KeyMsg{Type: tea.KeyDown}
	_, cmd := m.Update(msg)

	// 再フェッチ不要なので cmd は nil
	if cmd != nil {
		t.Error("expected nil cmd when pane ID did not change")
	}
}

// C3: PreviewResultMsg にエラーがあるとき previewText にエラー内容が入る
func TestPreviewResultMsgError(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(PreviewResultMsg{Text: "", Err: fmt.Errorf("connection failed")})
	m = updated.(Model)

	if !strings.Contains(m.previewText, "Error") {
		t.Errorf("previewText = %q, want to contain 'Error'", m.previewText)
	}
	if !strings.Contains(m.previewText, "connection failed") {
		t.Errorf("previewText = %q, want to contain 'connection failed'", m.previewText)
	}
	if m.previewLoading {
		t.Error("previewLoading should be false after receiving PreviewResultMsg")
	}
}

// C4: セッションが存在しないとき View() が "Select a session" を含む
func TestPreviewShowsSelectMessageWhenNoSession(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// セッションを投入しない（空のまま）
	view := m.View()

	if !strings.Contains(view, "Select a session") {
		t.Errorf("View() output does not contain 'Select a session', got: %q", view)
	}
}

// B1: カーソルが下移動でグループヘッダーをスキップする
func TestCursorDownSkipsGroupHeader(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// Entries will be: [WORKING header, session(PID=100), IDLE header, session(PID=200)]
	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking, PID: 100},
				{ID: "s2", State: core.Idle, PID: 200},
			},
		},
	}
	m = feedProjects(m, projects)

	// Cursor should start on the first session (PID=100)
	sel := m.selectedSession()
	if sel == nil {
		t.Fatal("expected a selected session after feedProjects")
	}
	if sel.session.PID != 100 {
		t.Fatalf("initial cursor PID = %d, want 100", sel.session.PID)
	}

	// Move cursor down — should skip the IDLE header and land on session(PID=200)
	msg := tea.KeyMsg{Type: tea.KeyDown}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	sel = m.selectedSession()
	if sel == nil {
		t.Fatal("expected a selected session after moving down")
	}
	if sel.isHeader {
		t.Error("cursor should not be on a header after moving down")
	}
	if sel.session.PID != 200 {
		t.Errorf("cursor PID = %d, want 200 (should have skipped IDLE header)", sel.session.PID)
	}
}

// B2: カーソルが上移動でグループヘッダーをスキップする
func TestCursorUpSkipsGroupHeader(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// Entries will be: [WORKING header, session(PID=100), IDLE header, session(PID=200)]
	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking, PID: 100},
				{ID: "s2", State: core.Idle, PID: 200},
			},
		},
	}
	m = feedProjects(m, projects)

	// Move down to land on session(PID=200)
	downMsg := tea.KeyMsg{Type: tea.KeyDown}
	updated, _ := m.Update(downMsg)
	m = updated.(Model)

	sel := m.selectedSession()
	if sel == nil || sel.session.PID != 200 {
		t.Fatalf("setup failed: expected cursor on PID=200, got %v", sel)
	}

	// Move cursor up — should skip the WORKING header and land on session(PID=100)
	upMsg := tea.KeyMsg{Type: tea.KeyUp}
	updated, _ = m.Update(upMsg)
	m = updated.(Model)

	sel = m.selectedSession()
	if sel == nil {
		t.Fatal("expected a selected session after moving up")
	}
	if sel.isHeader {
		t.Error("cursor should not be on a header after moving up")
	}
	if sel.session.PID != 100 {
		t.Errorf("cursor PID = %d, want 100 (should have skipped WORKING header)", sel.session.PID)
	}
}

// B3: カーソルが先頭で上移動しても範囲外にならない
func TestCursorUpAtTopStaysInBounds(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking, PID: 100},
			},
		},
	}
	m = feedProjects(m, projects)

	// Verify cursor is on the only session
	sel := m.selectedSession()
	if sel == nil || sel.session.PID != 100 {
		t.Fatalf("setup failed: expected cursor on PID=100")
	}
	initialCursor := m.cursor

	// Move cursor up — should stay in place
	upMsg := tea.KeyMsg{Type: tea.KeyUp}
	updated, _ := m.Update(upMsg)
	m = updated.(Model)

	if m.cursor < 0 {
		t.Errorf("cursor = %d, must not go negative", m.cursor)
	}
	if m.cursor != initialCursor {
		t.Errorf("cursor = %d, want %d (should stay at top)", m.cursor, initialCursor)
	}
	sel = m.selectedSession()
	if sel == nil {
		t.Fatal("expected a selected session after up-at-top")
	}
	if sel.session.PID != 100 {
		t.Errorf("cursor PID = %d, want 100", sel.session.PID)
	}
}

// B4: カーソルが末尾で下移動しても範囲外にならない
func TestCursorDownAtBottomStaysInBounds(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking, PID: 100},
			},
		},
	}
	m = feedProjects(m, projects)

	// Verify cursor is on the only session
	sel := m.selectedSession()
	if sel == nil || sel.session.PID != 100 {
		t.Fatalf("setup failed: expected cursor on PID=100")
	}
	initialCursor := m.cursor

	// Move cursor down — should stay in place
	downMsg := tea.KeyMsg{Type: tea.KeyDown}
	updated, _ := m.Update(downMsg)
	m = updated.(Model)

	if m.cursor >= len(m.entries) {
		t.Errorf("cursor = %d, must not exceed entries length %d", m.cursor, len(m.entries))
	}
	if m.cursor != initialCursor {
		t.Errorf("cursor = %d, want %d (should stay at bottom)", m.cursor, initialCursor)
	}
	sel = m.selectedSession()
	if sel == nil {
		t.Fatal("expected a selected session after down-at-bottom")
	}
	if sel.session.PID != 100 {
		t.Errorf("cursor PID = %d, want 100", sel.session.PID)
	}
}

// B5: カーソルが複数グループをまたいで移動できる
func TestCursorMovesAcrossGroups(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// Entries: [WAITING header, session(PID=100), WORKING header, session(PID=200), IDLE header, session(PID=300)]
	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Waiting, PID: 100},
				{ID: "s2", State: core.Thinking, PID: 200},
				{ID: "s3", State: core.Idle, PID: 300},
			},
		},
	}
	m = feedProjects(m, projects)

	// Cursor should start on the first session (PID=100, Waiting)
	sel := m.selectedSession()
	if sel == nil {
		t.Fatal("expected a selected session after feedProjects")
	}
	if sel.session.PID != 100 {
		t.Fatalf("initial cursor PID = %d, want 100", sel.session.PID)
	}

	// Move down twice — should skip WORKING header to land on PID=200, then skip IDLE header to land on PID=300
	downMsg := tea.KeyMsg{Type: tea.KeyDown}

	updated, _ := m.Update(downMsg)
	m = updated.(Model)

	sel = m.selectedSession()
	if sel == nil || sel.session.PID != 200 {
		pid := 0
		if sel != nil && sel.session != nil {
			pid = sel.session.PID
		}
		t.Fatalf("after 1st down: cursor PID = %d, want 200", pid)
	}

	updated, _ = m.Update(downMsg)
	m = updated.(Model)

	sel = m.selectedSession()
	if sel == nil {
		t.Fatal("expected a selected session after 2nd down")
	}
	if sel.isHeader {
		t.Error("cursor should not be on a header after 2nd down")
	}
	if sel.session.PID != 300 {
		t.Errorf("cursor PID = %d, want 300 (Idle session)", sel.session.PID)
	}
	if sel.state != core.Idle {
		t.Errorf("cursor state = %v, want core.Idle", sel.state)
	}
}

// --- 承認/拒否テスト ---

// waitingClaudeModel は Waiting 状態の Claude Code セッションを持つモデルを返す。
// activePane=1（右ペイン）に設定済み。
func waitingClaudeModel() (Model, *mockTerminal) {
	m, _, _, _, term := newTestModel()
	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{PID: 100, State: core.Waiting, Tool: core.ToolClaude, PaneID: "%1"},
			},
		},
	}
	m = feedProjects(m, projects)
	m.activePane = 1 // 右ペインをアクティブに
	return m, term
}

func TestSimpleApproveOnWaitingClaude(t *testing.T) {
	m, term := waitingClaudeModel()

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	updated, cmd := m.Update(msg)
	_ = updated.(Model)

	if cmd == nil {
		t.Fatal("expected cmd for approve")
	}

	// コマンドを実行して SendKeys を確認
	result := cmd()
	if _, ok := result.(ApprovalResultMsg); !ok {
		t.Fatalf("expected ApprovalResultMsg, got %T", result)
	}
	if len(term.sentKeys) != 1 || term.sentKeys[0] != "Enter" {
		t.Errorf("sentKeys = %v, want [Enter]", term.sentKeys)
	}
}

func TestSimpleDenyOnWaitingClaude(t *testing.T) {
	m, term := waitingClaudeModel()

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}
	updated, cmd := m.Update(msg)
	_ = updated.(Model)

	if cmd == nil {
		t.Fatal("expected cmd for deny")
	}

	result := cmd()
	if _, ok := result.(ApprovalResultMsg); !ok {
		t.Fatalf("expected ApprovalResultMsg, got %T", result)
	}
	if len(term.sentKeys) != 3 || term.sentKeys[0] != "Down" || term.sentKeys[1] != "Down" || term.sentKeys[2] != "Enter" {
		t.Errorf("sentKeys = %v, want [Down Down Enter]", term.sentKeys)
	}
}

func TestApproveIgnoredWhenNotActivePane(t *testing.T) {
	m, _ := waitingClaudeModel()
	m.activePane = 0 // 左ペイン

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	_, cmd := m.Update(msg)

	if cmd != nil {
		t.Error("expected nil cmd when left pane is active")
	}
}

func TestApproveIgnoredOnNonWaitingState(t *testing.T) {
	m, _, _, _, _ := newTestModel()
	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{PID: 100, State: core.Thinking, Tool: core.ToolClaude, PaneID: "%1"},
			},
		},
	}
	m = feedProjects(m, projects)
	m.activePane = 1

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	_, cmd := m.Update(msg)

	if cmd != nil {
		t.Error("expected nil cmd for non-Waiting session")
	}
}

func TestApproveIgnoredOnNonClaudeTool(t *testing.T) {
	m, _, _, _, _ := newTestModel()
	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{PID: 100, State: core.Waiting, Tool: core.ToolCodex, PaneID: "%1"},
			},
		},
	}
	m = feedProjects(m, projects)
	m.activePane = 1

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	_, cmd := m.Update(msg)

	if cmd != nil {
		t.Error("expected nil cmd for non-Claude tool")
	}
}

func TestPromptApproveInputMode(t *testing.T) {
	m, term := waitingClaudeModel()

	// Shift+A でテキスト入力モードに入る
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if m.inputMode != inputApprove {
		t.Fatalf("inputMode = %d, want inputApprove", m.inputMode)
	}

	// テキストを入力（文字を1つずつ送信）
	for _, r := range "fix tests" {
		charMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		updated, _ = m.Update(charMsg)
		m = updated.(Model)
	}

	// Enter で確定
	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, cmd := m.Update(enterMsg)
	m = updated.(Model)

	if m.inputMode != inputNone {
		t.Errorf("inputMode = %d, want inputNone after Enter", m.inputMode)
	}
	if cmd == nil {
		t.Fatal("expected cmd for prompt approve")
	}

	result := cmd()
	if _, ok := result.(ApprovalResultMsg); !ok {
		t.Fatalf("expected ApprovalResultMsg, got %T", result)
	}

	// Enter（承認）+ テキスト + Enter が送信されるはず
	if len(term.sentKeys) == 0 {
		t.Fatal("expected sentKeys to be non-empty")
	}
	if term.sentKeys[0] != "Enter" {
		t.Errorf("first sentKey = %q, want Enter", term.sentKeys[0])
	}
}

func TestPromptDenyInputMode(t *testing.T) {
	m, term := waitingClaudeModel()

	// Shift+D でテキスト入力モードに入る
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if m.inputMode != inputDeny {
		t.Fatalf("inputMode = %d, want inputDeny", m.inputMode)
	}

	// テキストを入力
	for _, r := range "bad idea" {
		charMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		updated, _ = m.Update(charMsg)
		m = updated.(Model)
	}

	// Enter で確定
	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, cmd := m.Update(enterMsg)
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected cmd for prompt deny")
	}

	result := cmd()
	if _, ok := result.(ApprovalResultMsg); !ok {
		t.Fatalf("expected ApprovalResultMsg, got %T", result)
	}

	if len(term.sentKeys) == 0 {
		t.Fatal("expected sentKeys to be non-empty")
	}
	if term.sentKeys[0] != "Escape" {
		t.Errorf("first sentKey = %q, want Escape", term.sentKeys[0])
	}
}

func TestInputModeEscapeCancels(t *testing.T) {
	m, _ := waitingClaudeModel()

	// A でテキスト入力モードに入る
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if m.inputMode != inputApprove {
		t.Fatalf("inputMode = %d, want inputApprove", m.inputMode)
	}

	// Esc でキャンセル
	escMsg := tea.KeyMsg{Type: tea.KeyEsc}
	updated, cmd := m.Update(escMsg)
	m = updated.(Model)

	if m.inputMode != inputNone {
		t.Errorf("inputMode = %d, want inputNone after Esc", m.inputMode)
	}
	if cmd != nil {
		t.Error("expected nil cmd after Esc cancel")
	}
}

// --- canInput tests ---

// TestCanInputOnClaudeSession verifies canInput returns true for a Claude session (even non-Waiting) on right pane.
func TestCanInputOnClaudeSession(t *testing.T) {
	m, _, _, _, _ := newTestModel()
	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{PID: 100, State: core.Idle, Tool: core.ToolClaude, PaneID: "%1"},
			},
		},
	}
	m = feedProjects(m, projects)
	m.activePane = 1

	if !m.canInput() {
		t.Error("canInput() = false, want true for Claude session on right pane")
	}
}

// TestCanInputFalseOnLeftPane verifies canInput returns false when left pane is active.
func TestCanInputFalseOnLeftPane(t *testing.T) {
	m, _, _, _, _ := newTestModel()
	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{PID: 100, State: core.Waiting, Tool: core.ToolClaude, PaneID: "%1"},
			},
		},
	}
	m = feedProjects(m, projects)
	m.activePane = 0 // left pane

	if m.canInput() {
		t.Error("canInput() = true, want false when left pane is active")
	}
}

// TestCanInputFalseOnNonClaude verifies canInput returns false for Codex and Gemini sessions.
func TestCanInputFalseOnNonClaude(t *testing.T) {
	for _, tool := range []core.ToolType{core.ToolCodex, core.ToolGemini} {
		m, _, _, _, _ := newTestModel()
		projects := []core.Project{
			{
				Path: "/project-a",
				Sessions: []*core.Session{
					{PID: 100, State: core.Waiting, Tool: tool, PaneID: "%1"},
				},
			},
		}
		m = feedProjects(m, projects)
		m.activePane = 1

		if m.canInput() {
			t.Errorf("canInput() = true for tool=%v, want false", tool)
		}
	}
}

// --- flash message tests ---

// TestFlashClearMsg verifies FlashClearMsg clears flashMessage.
func TestFlashClearMsg(t *testing.T) {
	m, _, _, _, _ := newTestModel()
	m.flashMessage = "some flash"

	updated, cmd := m.Update(FlashClearMsg{})
	m = updated.(Model)

	if m.flashMessage != "" {
		t.Errorf("flashMessage = %q, want empty after FlashClearMsg", m.flashMessage)
	}
	if cmd != nil {
		t.Error("expected nil cmd after FlashClearMsg")
	}
}

// TestApprovalResultSetsFlash verifies successful ApprovalResultMsg sets flashMessage to Label.
func TestApprovalResultSetsFlash(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, cmd := m.Update(ApprovalResultMsg{Err: nil, Label: "Approved"})
	m = updated.(Model)

	if m.flashMessage != "Approved" {
		t.Errorf("flashMessage = %q, want 'Approved'", m.flashMessage)
	}
	if m.err != nil {
		t.Errorf("err = %v, want nil on success", m.err)
	}
	if cmd == nil {
		t.Error("expected flash-clear timer cmd")
	}
}

// TestApprovalResultErrorClearsFlash verifies failed ApprovalResultMsg sets err and clears flashMessage.
func TestApprovalResultErrorClearsFlash(t *testing.T) {
	m, _, _, _, _ := newTestModel()
	m.flashMessage = "previous flash"

	testErr := fmt.Errorf("terminal error")
	updated, cmd := m.Update(ApprovalResultMsg{Err: testErr, Label: "Approved"})
	m = updated.(Model)

	if m.err == nil {
		t.Error("err should be set on failure")
	}
	if m.flashMessage != "" {
		t.Errorf("flashMessage = %q, want empty on error", m.flashMessage)
	}
	if cmd == nil {
		t.Error("expected flash-clear timer cmd even on error")
	}
}

// --- prompt input mode tests ---

// TestPromptInputOnNonWaitingShowsWarning verifies that entering text and pressing Enter
// on a non-Waiting session sets a warning flash instead of sending keys.
func TestPromptInputOnNonWaitingShowsWarning(t *testing.T) {
	m, _, _, _, term := newTestModel()
	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{PID: 100, State: core.Idle, Tool: core.ToolClaude, PaneID: "%1"},
			},
		},
	}
	m = feedProjects(m, projects)
	m.activePane = 1

	// Arrange: enter input mode with A key (Idle Claude — canInput true, canApprove false)
	aMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}}
	updated, _ := m.Update(aMsg)
	m = updated.(Model)

	if m.inputMode != inputApprove {
		t.Fatalf("inputMode = %d, want inputApprove", m.inputMode)
	}

	// Act: type some text
	for _, r := range "hello" {
		charMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		updated, _ = m.Update(charMsg)
		m = updated.(Model)
	}

	// Act: press Enter — should NOT send keys, should set warning flash
	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, cmd := m.Update(enterMsg)
	m = updated.(Model)

	// Assert
	if m.inputMode != inputNone {
		t.Errorf("inputMode = %d, want inputNone after Enter", m.inputMode)
	}
	if !strings.Contains(m.flashMessage, "Not in Waiting state") {
		t.Errorf("flashMessage = %q, want to contain 'Not in Waiting state'", m.flashMessage)
	}
	if cmd == nil {
		t.Error("expected flash-clear timer cmd")
	}
	if len(term.sentKeys) > 0 {
		t.Errorf("sentKeys = %v, want no keys sent for non-Waiting session", term.sentKeys)
	}
}

// --- failingMockTerminal: first SendKeys call returns an error ---

type failingMockTerminal struct {
	calls    int
	sentKeys []string
}

func (f *failingMockTerminal) ListPanes() ([]terminal.Pane, error) { return nil, nil }
func (f *failingMockTerminal) FocusPane(paneID string) error        { return nil }
func (f *failingMockTerminal) GetPaneText(paneID string) (string, error) {
	return "", nil
}
func (f *failingMockTerminal) IsAvailable() bool { return true }
func (f *failingMockTerminal) Name() string       { return "failing-mock" }
func (f *failingMockTerminal) SendKeys(paneID string, keys ...string) error {
	f.calls++
	f.sentKeys = append(f.sentKeys, keys...)
	if f.calls == 1 {
		return fmt.Errorf("terminal unavailable")
	}
	return nil
}

// TestPromptApproveFullKeySequence verifies A -> type "fix tests" -> Enter
// sends ["Enter", "fix tests", "Enter"] to the terminal.
func TestPromptApproveFullKeySequence(t *testing.T) {
	m, term := waitingClaudeModel()

	// Enter input mode with A
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = updated.(Model)

	if m.inputMode != inputApprove {
		t.Fatalf("inputMode = %d, want inputApprove", m.inputMode)
	}

	// Type "fix tests"
	for _, r := range "fix tests" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}

	// Confirm with Enter
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.inputMode != inputNone {
		t.Errorf("inputMode = %d, want inputNone after Enter", m.inputMode)
	}
	if cmd == nil {
		t.Fatal("expected cmd after Enter in inputApprove mode")
	}

	result := cmd()
	if _, ok := result.(ApprovalResultMsg); !ok {
		t.Fatalf("expected ApprovalResultMsg, got %T", result)
	}

	// Expect ["Enter", "fix tests", "Enter"]
	want := []string{"Enter", "fix tests", "Enter"}
	if len(term.sentKeys) != len(want) {
		t.Fatalf("sentKeys = %v, want %v", term.sentKeys, want)
	}
	for i, k := range want {
		if term.sentKeys[i] != k {
			t.Errorf("sentKeys[%d] = %q, want %q", i, term.sentKeys[i], k)
		}
	}
}

// TestPromptDenyFullKeySequence verifies D -> type "bad idea" -> Enter
// sends ["Escape", "bad idea", "Enter"] to the terminal.
func TestPromptDenyFullKeySequence(t *testing.T) {
	m, term := waitingClaudeModel()

	// Enter input mode with D
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	m = updated.(Model)

	if m.inputMode != inputDeny {
		t.Fatalf("inputMode = %d, want inputDeny", m.inputMode)
	}

	// Type "bad idea"
	for _, r := range "bad idea" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}

	// Confirm with Enter
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.inputMode != inputNone {
		t.Errorf("inputMode = %d, want inputNone after Enter", m.inputMode)
	}
	if cmd == nil {
		t.Fatal("expected cmd after Enter in inputDeny mode")
	}

	result := cmd()
	if _, ok := result.(ApprovalResultMsg); !ok {
		t.Fatalf("expected ApprovalResultMsg, got %T", result)
	}

	// Expect ["Escape", "bad idea", "Enter"]
	want := []string{"Escape", "bad idea", "Enter"}
	if len(term.sentKeys) != len(want) {
		t.Fatalf("sentKeys = %v, want %v", term.sentKeys, want)
	}
	for i, k := range want {
		if term.sentKeys[i] != k {
			t.Errorf("sentKeys[%d] = %q, want %q", i, term.sentKeys[i], k)
		}
	}
}

// TestPromptApproveEmptyText verifies A -> Enter (no text) sends only ["Enter"].
func TestPromptApproveEmptyText(t *testing.T) {
	m, term := waitingClaudeModel()

	// Enter input mode with A
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = updated.(Model)

	// Confirm immediately (no text typed)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("expected cmd after Enter in inputApprove mode")
	}

	result := cmd()
	if _, ok := result.(ApprovalResultMsg); !ok {
		t.Fatalf("expected ApprovalResultMsg, got %T", result)
	}

	// Expect only ["Enter"] — no second SendKeys with empty text
	if len(term.sentKeys) != 1 {
		t.Fatalf("sentKeys = %v, want [Enter] only", term.sentKeys)
	}
	if term.sentKeys[0] != "Enter" {
		t.Errorf("sentKeys[0] = %q, want Enter", term.sentKeys[0])
	}
}

// TestPromptDenyEmptyText verifies D -> Enter (no text) sends only ["Escape"].
func TestPromptDenyEmptyText(t *testing.T) {
	m, term := waitingClaudeModel()

	// Enter input mode with D
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	m = updated.(Model)

	// Confirm immediately (no text typed)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("expected cmd after Enter in inputDeny mode")
	}

	result := cmd()
	if _, ok := result.(ApprovalResultMsg); !ok {
		t.Fatalf("expected ApprovalResultMsg, got %T", result)
	}

	// Expect only ["Escape"] — no second SendKeys with empty text
	if len(term.sentKeys) != 1 {
		t.Fatalf("sentKeys = %v, want [Escape] only", term.sentKeys)
	}
	if term.sentKeys[0] != "Escape" {
		t.Errorf("sentKeys[0] = %q, want Escape", term.sentKeys[0])
	}
}

// TestPromptApproveFirstSendKeysError verifies that when the first SendKeys fails,
// ApprovalResultMsg.Err is non-nil and no further keys are sent.
func TestPromptApproveFirstSendKeysError(t *testing.T) {
	failTerm := &failingMockTerminal{}

	reader := &mockStateReader{}
	updater := &mockStateUpdater{}
	scanner := &mockScanner{}
	cfg := config.Default()

	m := NewModel(scanner, updater, reader, failTerm, cfg, false)
	projects := []core.Project{
		{
			Path: "/project-a",
			Name: "project-a",
			Sessions: []*core.Session{
				{PID: 100, State: core.Waiting, Tool: core.ToolClaude, PaneID: "%1"},
			},
		},
	}
	m = feedProjects(m, projects)
	m.activePane = 1

	// Simple approve (a key) — triggers handleSimpleApprove which calls SendKeys once
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	_ = updated.(Model)

	if cmd == nil {
		t.Fatal("expected cmd for approve")
	}

	result := cmd()
	msg, ok := result.(ApprovalResultMsg)
	if !ok {
		t.Fatalf("expected ApprovalResultMsg, got %T", result)
	}

	if msg.Err == nil {
		t.Error("expected Err to be non-nil when first SendKeys fails")
	}

	// Only the first (failing) SendKeys call should have been made
	if len(failTerm.sentKeys) != 1 {
		t.Errorf("sentKeys = %v (len %d), want exactly 1 key from the failed call", failTerm.sentKeys, len(failTerm.sentKeys))
	}
}

// TestPromptInputAvailableOnIdleClaude verifies A/D keys enter input mode on Idle Claude session (right pane).
func TestPromptInputAvailableOnIdleClaude(t *testing.T) {
	m, _, _, _, _ := newTestModel()
	projects := []core.Project{
		{
			Path: "/project-a",
			Sessions: []*core.Session{
				{PID: 100, State: core.Idle, Tool: core.ToolClaude, PaneID: "%1"},
			},
		},
	}
	m = feedProjects(m, projects)
	m.activePane = 1

	// Arrange & Act: A key should enter inputApprove mode
	aMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}}
	updated, _ := m.Update(aMsg)
	m = updated.(Model)

	// Assert
	if m.inputMode != inputApprove {
		t.Errorf("inputMode = %d after A on Idle Claude, want inputApprove", m.inputMode)
	}

	// Reset: press Esc
	escMsg := tea.KeyMsg{Type: tea.KeyEsc}
	updated, _ = m.Update(escMsg)
	m = updated.(Model)

	if m.inputMode != inputNone {
		t.Fatalf("inputMode = %d, want inputNone after Esc", m.inputMode)
	}

	// Arrange & Act: D key should enter inputDeny mode
	dMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}}
	updated, _ = m.Update(dMsg)
	m = updated.(Model)

	// Assert
	if m.inputMode != inputDeny {
		t.Errorf("inputMode = %d after D on Idle Claude, want inputDeny", m.inputMode)
	}
}

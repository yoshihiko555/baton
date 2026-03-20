package core

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/yoshihiko555/baton/internal/terminal"
)

// newScanResult は指定プロセス一覧と空ペインで ScanResult を生成するヘルパー。
func newScanResult(procs ...DetectedProcess) ScanResult {
	return ScanResult{
		Processes: procs,
		Panes:     []terminal.Pane{},
		Timestamp: time.Now(),
	}
}

// newProc は DetectedProcess を生成するヘルパー。
func newProc(pid int, tool ToolType, cwd string) DetectedProcess {
	return DetectedProcess{
		PID:      pid,
		ToolType: tool,
		CWD:      cwd,
	}
}

func TestStateManagerUpdateFromScanBasic(t *testing.T) {
	// 正常系: Codex/Gemini プロセスが Thinking 状態でセッション化されることを確認する。
	manager := NewStateManager(nil)

	result := newScanResult(
		newProc(100, ToolCodex, "/home/user/project-a"),
		newProc(200, ToolGemini, "/home/user/project-b"),
	)

	if err := manager.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	projects := manager.Projects()
	if len(projects) != 2 {
		t.Fatalf("unexpected project count: got %d, want 2", len(projects))
	}

	// 各プロジェクトにセッションが1つあり、状態が Thinking であることを確認する。
	for _, p := range projects {
		if len(p.Sessions) != 1 {
			t.Errorf("project %q: unexpected session count: got %d, want 1", p.Name, len(p.Sessions))
			continue
		}
		if p.Sessions[0].State != Thinking {
			t.Errorf("project %q: unexpected state: got %v, want Thinking", p.Name, p.Sessions[0].State)
		}
	}
}

func TestStateManagerUpdateFromScanError(t *testing.T) {
	// エラーあり ScanResult は前回スナップショットを保持することを確認する。
	manager := NewStateManager(nil)

	// 初回: 正常スキャンでプロジェクトを登録する。
	if err := manager.UpdateFromScan(newScanResult(newProc(100, ToolCodex, "/home/user/proj"))); err != nil {
		t.Fatalf("UpdateFromScan (initial): %v", err)
	}

	before := manager.Projects()
	if len(before) != 1 {
		t.Fatalf("unexpected project count before error scan: %d", len(before))
	}

	// 2回目: エラーあり — スナップショットは変わらない。
	errResult := ScanResult{Err: errDummy}
	if err := manager.UpdateFromScan(errResult); err != nil {
		t.Fatalf("UpdateFromScan (error): %v", err)
	}

	after := manager.Projects()
	if len(after) != 1 {
		t.Errorf("snapshot should be preserved on error: got %d projects, want 1", len(after))
	}
}

// errDummy はテスト用のダミーエラー。
var errDummy = &dummyError{}

type dummyError struct{}

func (e *dummyError) Error() string { return "dummy error" }

func TestStateManagerUpdateFromScanRemoval(t *testing.T) {
	// プロセスが消えた場合にセッションが削除されることを確認する。
	manager := NewStateManager(nil)

	// 2プロセスを登録する。
	if err := manager.UpdateFromScan(newScanResult(
		newProc(100, ToolCodex, "/proj-a"),
		newProc(200, ToolGemini, "/proj-b"),
	)); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	if len(manager.Projects()) != 2 {
		t.Fatalf("want 2 projects after initial scan")
	}

	// PID=100 のみ残して再スキャンする。
	if err := manager.UpdateFromScan(newScanResult(newProc(100, ToolCodex, "/proj-a"))); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	projects := manager.Projects()
	if len(projects) != 1 {
		t.Fatalf("want 1 project after removal, got %d", len(projects))
	}
	if projects[0].Path != "/proj-a" {
		t.Errorf("remaining project should be /proj-a, got %q", projects[0].Path)
	}
}

func TestStateManagerUpdateFromScanGroupingByCWD(t *testing.T) {
	// 同一 CWD の複数プロセスが同一プロジェクトにグルーピングされることを確認する。
	manager := NewStateManager(nil)

	if err := manager.UpdateFromScan(newScanResult(
		newProc(100, ToolCodex, "/shared"),
		newProc(200, ToolGemini, "/shared"),
	)); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	projects := manager.Projects()
	if len(projects) != 1 {
		t.Fatalf("same CWD should be grouped into 1 project, got %d", len(projects))
	}
	if len(projects[0].Sessions) != 2 {
		t.Errorf("want 2 sessions in grouped project, got %d", len(projects[0].Sessions))
	}
}

func TestStateManagerUpdateFromScanWorkspaceGrouping(t *testing.T) {
	// Workspace が設定されたペインに紐づくプロセスはワークスペース優先でグルーピングされる。
	manager := NewStateManager(nil)

	panes := []terminal.Pane{
		{ID: "1", SessionName: "my-workspace"},
		{ID: "2", SessionName: "my-workspace"},
	}
	procs := []DetectedProcess{
		{PID: 100, ToolType: ToolCodex, PaneID: "1", CWD: "/proj-a"},
		{PID: 200, ToolType: ToolGemini, PaneID: "2", CWD: "/proj-b"},
	}
	result := ScanResult{
		Processes: procs,
		Panes:     panes,
		Timestamp: time.Now(),
	}

	if err := manager.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	projects := manager.Projects()
	if len(projects) != 1 {
		t.Fatalf("workspace grouping should yield 1 project, got %d", len(projects))
	}
	if projects[0].Workspace != "my-workspace" {
		t.Errorf("project workspace = %q, want my-workspace", projects[0].Workspace)
	}
	if len(projects[0].Sessions) != 2 {
		t.Errorf("want 2 sessions, got %d", len(projects[0].Sessions))
	}
}

func TestStateManagerUpdateFromScanDefaultWorkspace(t *testing.T) {
	// Workspace が "default" の場合は CWD でグルーピングされることを確認する。
	manager := NewStateManager(nil)

	panes := []terminal.Pane{
		{ID: "1", SessionName: "default"},
		{ID: "2", SessionName: "default"},
	}
	procs := []DetectedProcess{
		{PID: 100, ToolType: ToolCodex, PaneID: "1", CWD: "/proj-a"},
		{PID: 200, ToolType: ToolGemini, PaneID: "2", CWD: "/proj-b"},
	}
	result := ScanResult{
		Processes: procs,
		Panes:     panes,
		Timestamp: time.Now(),
	}

	if err := manager.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	projects := manager.Projects()
	if len(projects) != 2 {
		t.Fatalf("default workspace should fall back to CWD grouping, got %d projects", len(projects))
	}
}

func TestStateManagerProjectsSortOrder(t *testing.T) {
	// ソート規則: 状態優先度 Waiting > Error > Thinking > ToolUse > Idle を確認する。
	// resolver なし（nil）では全セッションが Thinking になるため、
	// ここでは手動でセッションポインタを構築して sortSessionPtrs を直接テストする。
	sessions := []*Session{
		{PID: 1, State: Idle},
		{PID: 2, State: Waiting},
		{PID: 3, State: ToolUse},
		{PID: 4, State: Error},
		{PID: 5, State: Thinking},
	}

	sortSessionPtrs(sessions)

	want := []SessionState{Waiting, Error, Thinking, ToolUse, Idle}
	for i, sess := range sessions {
		if sess.State != want[i] {
			t.Errorf("sessions[%d].State = %v, want %v", i, sess.State, want[i])
		}
	}
}

func TestStateManagerProjectsSortLastActivity(t *testing.T) {
	// 同一状態内は LastActivity 降順（新しいほど先頭）であることを確認する。
	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)

	sessions := []*Session{
		{PID: 1, State: Thinking, LastActivity: t1},
		{PID: 2, State: Thinking, LastActivity: t2},
	}

	sortSessionPtrs(sessions)

	if sessions[0].PID != 2 {
		t.Errorf("newer LastActivity should come first, got PID %d", sessions[0].PID)
	}
}

func TestStateManagerSummary(t *testing.T) {
	// Summary が正しく集計されることを確認する。
	manager := NewStateManager(nil)

	if err := manager.UpdateFromScan(newScanResult(
		newProc(100, ToolCodex, "/proj-a"),
		newProc(200, ToolGemini, "/proj-b"),
		newProc(300, ToolCodex, "/proj-c"),
	)); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	summary := manager.Summary()
	if summary.TotalSessions != 3 {
		t.Errorf("TotalSessions = %d, want 3", summary.TotalSessions)
	}
	// Codex/Gemini は Thinking → Active に含まれる。
	if summary.Active != 3 {
		t.Errorf("Active = %d, want 3", summary.Active)
	}
	if summary.Waiting != 0 {
		t.Errorf("Waiting = %d, want 0", summary.Waiting)
	}
	if summary.ByTool["codex"] != 2 {
		t.Errorf("ByTool[codex] = %d, want 2", summary.ByTool["codex"])
	}
	if summary.ByTool["gemini"] != 1 {
		t.Errorf("ByTool[gemini] = %d, want 1", summary.ByTool["gemini"])
	}
}

func TestStateManagerPanes(t *testing.T) {
	// Panes がスキャン結果から保存されることを確認する。
	manager := NewStateManager(nil)

	panes := []terminal.Pane{
		{ID: "1", TTYName: "/dev/ttys001"},
		{ID: "2", TTYName: "/dev/ttys002"},
	}
	result := ScanResult{
		Processes: []DetectedProcess{},
		Panes:     panes,
		Timestamp: time.Now(),
	}

	if err := manager.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	got := manager.Panes()
	if len(got) != 2 {
		t.Errorf("Panes() = %d, want 2", len(got))
	}
}

func TestStateManagerProjectsDefensiveCopy(t *testing.T) {
	// Projects() が防御的コピーを返すことを確認する（返り値を変更しても内部状態が変わらない）。
	manager := NewStateManager(nil)

	if err := manager.UpdateFromScan(newScanResult(newProc(100, ToolCodex, "/proj"))); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	projects := manager.Projects()
	if len(projects) == 0 || len(projects[0].Sessions) == 0 {
		t.Fatal("expected at least one project with one session")
	}

	// 返り値を改ざんする。
	projects[0].Name = "mutated"
	projects[0].Sessions[0].State = Error

	// 再取得して内部状態が変わっていないことを確認する。
	fresh := manager.Projects()
	if fresh[0].Name == "mutated" {
		t.Error("Projects() should return a defensive copy (Name was mutated)")
	}
	if fresh[0].Sessions[0].State == Error {
		t.Error("Projects() should return a defensive copy (State was mutated)")
	}
}

func TestStateManagerEmptyProjects(t *testing.T) {
	// プロセスが0件のとき Projects() が空スライスを返すことを確認する。
	manager := NewStateManager(nil)

	if err := manager.UpdateFromScan(newScanResult()); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	projects := manager.Projects()
	if projects == nil {
		t.Error("Projects() should return non-nil empty slice")
	}
	if len(projects) != 0 {
		t.Errorf("Projects() = %d, want 0", len(projects))
	}
}

func TestStateManagerGetProjects(t *testing.T) {
	// GetProjects が Projects と同じ結果を返すことを確認する（v1 互換）。
	manager := NewStateManager(nil)

	if err := manager.UpdateFromScan(newScanResult(newProc(100, ToolCodex, "/proj"))); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	p1 := manager.Projects()
	p2 := manager.GetProjects()

	if len(p1) != len(p2) {
		t.Errorf("GetProjects() length %d != Projects() length %d", len(p2), len(p1))
	}
}

func TestCalcSummaryWaiting(t *testing.T) {
	// Waiting 状態は Active と Waiting の両方にカウントされることを確認する。
	projects := []Project{
		{
			Sessions: []*Session{
				{State: Waiting, Tool: ToolClaude},
				{State: Idle, Tool: ToolClaude},
			},
		},
	}

	s := calcSummary(projects)
	if s.TotalSessions != 2 {
		t.Errorf("TotalSessions = %d, want 2", s.TotalSessions)
	}
	if s.Active != 1 {
		t.Errorf("Active = %d, want 1", s.Active)
	}
	if s.Waiting != 1 {
		t.Errorf("Waiting = %d, want 1", s.Waiting)
	}
}

func TestCalcSummaryNilSession(t *testing.T) {
	// nil セッションがあってもパニックしないことを確認する。
	projects := []Project{
		{
			Sessions: []*Session{
				{State: Thinking, Tool: ToolCodex},
				nil,
			},
		},
	}

	s := calcSummary(projects)
	if s.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d, want 1 (nil session should be skipped)", s.TotalSessions)
	}
}

func TestSortSessionPtrsNilSafe(t *testing.T) {
	// nil ポインタが混在してもパニックしないことを確認する。
	sessions := []*Session{
		nil,
		{PID: 1, State: Thinking},
		nil,
	}

	sortSessionPtrs(sessions) // パニックしなければ OK
}

func TestProjectNeedsAttentionNoSessions(t *testing.T) {
	// セッションなしのプロジェクトは attention 不要。
	p := Project{}
	if projectNeedsAttention(p) {
		t.Error("projectNeedsAttention(empty) should be false")
	}
}

func TestProjectNeedsAttentionWithWaiting(t *testing.T) {
	p := Project{Sessions: []*Session{{State: Waiting}}}
	if !projectNeedsAttention(p) {
		t.Error("projectNeedsAttention with Waiting session should be true")
	}
}

func TestProjectNeedsAttentionThinkingOnly(t *testing.T) {
	p := Project{Sessions: []*Session{{State: Thinking}}}
	if projectNeedsAttention(p) {
		t.Error("projectNeedsAttention with only Thinking should be false")
	}
}

func TestResolveProjectKey(t *testing.T) {
	proc := DetectedProcess{PID: 1, PaneID: "10", CWD: "/my/project"}

	tests := []struct {
		name      string
		workspace string
		wantWS    string
		wantCWD   string
	}{
		{"workspace set", "my-ws", "my-ws", ""},
		{"workspace default", "default", "", "/my/project"},
		{"workspace empty", "", "", "/my/project"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			paneMap := map[string]string{"10": tc.workspace}
			key := resolveProjectKey(proc, paneMap)
			if key.Workspace != tc.wantWS {
				t.Errorf("Workspace = %q, want %q", key.Workspace, tc.wantWS)
			}
			if key.CWD != tc.wantCWD {
				t.Errorf("CWD = %q, want %q", key.CWD, tc.wantCWD)
			}
		})
	}
}

// newExitError1 は exit code 1 の *exec.ExitError を返すヘルパー。
// pgrep が子プロセスなしのとき返すエラーを再現するために使用する。
func newExitError1(t *testing.T) error {
	t.Helper()
	cmd := exec.Command("sh", "-c", "exit 1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-nil error from 'sh -c exit 1'")
	}
	return err
}

func TestStateManagerCodexWithChildProcesses(t *testing.T) {
	// Codex プロセスに作業用子プロセスがある場合、セッション状態が Thinking になることを確認する。
	ps := NewProcessScannerWithExec(func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "pgrep" {
			return []byte("99999\n"), nil
		}
		if name == "ps" {
			return []byte("sandbox-exec\n"), nil
		}
		return nil, fmt.Errorf("unexpected command: %s", name)
	})

	manager := NewStateManager(nil)
	manager.SetProcessScanner(ps)

	result := newScanResult(newProc(100, ToolCodex, "/project"))
	if err := manager.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != Thinking {
		t.Errorf("state = %v, want Thinking (codex with child processes)", got)
	}
}

func TestStateManagerCodexWithoutChildProcesses(t *testing.T) {
	// Codex プロセスに作業用子プロセスがない場合（常駐のみ）、セッション状態が Idle になることを確認する。
	ps := NewProcessScannerWithExec(func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "pgrep" {
			return []byte("99998\n99999\n"), nil
		}
		if name == "ps" {
			return []byte("node\ncaffeinate\n"), nil
		}
		return nil, fmt.Errorf("unexpected command: %s", name)
	})

	manager := NewStateManager(nil)
	manager.SetProcessScanner(ps)

	result := newScanResult(newProc(200, ToolCodex, "/project"))
	if err := manager.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != Idle {
		t.Errorf("state = %v, want Idle (codex without child processes)", got)
	}
}

func TestStateManagerCodexWithNilProcessScanner(t *testing.T) {
	// ProcessScanner が nil の場合、Codex セッションはデフォルトの Thinking になることを確認する。
	manager := NewStateManager(nil)
	// SetProcessScanner を呼ばない（nil のまま）

	result := newScanResult(newProc(300, ToolCodex, "/project"))
	if err := manager.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != Thinking {
		t.Errorf("state = %v, want Thinking (nil processScanner fallback)", got)
	}
}

func TestStateManagerGeminiIgnoresChildProcesses(t *testing.T) {
	// Gemini プロセスは子プロセスの有無に関わらず常に Thinking になることを確認する。
	// pgrep が呼ばれた場合はテスト失敗とすることで、Gemini が HasChildProcesses を呼ばないことも検証する。
	ps := NewProcessScannerWithExec(func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "pgrep" {
			t.Error("HasChildProcesses should NOT be called for Gemini process")
			return []byte("99999\n"), nil
		}
		return nil, fmt.Errorf("unexpected command: %s", name)
	})

	manager := NewStateManager(nil)
	manager.SetProcessScanner(ps)

	result := newScanResult(newProc(400, ToolGemini, "/project"))
	if err := manager.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != Thinking {
		t.Errorf("state = %v, want Thinking (gemini always Thinking)", got)
	}
}

// paneTextTerminal は GetPaneText の戻り値を制御できるテスト用 Terminal。
type paneTextTerminal struct {
	texts map[string]string
}

func (m *paneTextTerminal) ListPanes() ([]terminal.Pane, error) { return nil, nil }
func (m *paneTextTerminal) FocusPane(paneID string) error       { return nil }
func (m *paneTextTerminal) GetPaneText(paneID string) (string, error) {
	if text, ok := m.texts[paneID]; ok {
		return text, nil
	}
	return "", fmt.Errorf("pane not found: %s", paneID)
}
func (m *paneTextTerminal) IsAvailable() bool { return true }
func (m *paneTextTerminal) Name() string      { return "mock" }

func TestRefineGeminiThinkingToWaiting(t *testing.T) {
	// Gemini の Thinking 状態がペインテキストの承認パターンで Waiting に変わることを確認する。
	manager := NewStateManager(nil)

	result := newScanResult(newProc(500, ToolGemini, "/project"))
	if err := manager.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	// ペインに承認プロンプトを設定
	term := &paneTextTerminal{
		texts: map[string]string{
			"": "Some output...\nAllow? [y/N]\n",
		},
	}

	manager.RefineToolUseState(term)

	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != Waiting {
		t.Errorf("state = %v, want Waiting (gemini approval prompt detected)", got)
	}
}

func TestRefineGeminiThinkingToIdle(t *testing.T) {
	// Gemini のペインに "> " プロンプトがあれば Idle に変わることを確認する。
	manager := NewStateManager(nil)

	result := newScanResult(newProc(500, ToolGemini, "/project"))
	if err := manager.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	term := &paneTextTerminal{
		texts: map[string]string{
			"": "Previous output...\n > baton\n workspace (/directory)                  branch      sandbox\n ~/ghq/github.com/yoshihiko555/baton     main        no sandbox\n",
		},
	}

	manager.RefineToolUseState(term)

	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != Idle {
		t.Errorf("state = %v, want Idle (gemini input prompt detected)", got)
	}
}

func TestRefineGeminiThinkingStaysThinking(t *testing.T) {
	// Gemini のペインに承認パターンがなければ Thinking のまま。
	manager := NewStateManager(nil)

	result := newScanResult(newProc(500, ToolGemini, "/project"))
	if err := manager.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	term := &paneTextTerminal{
		texts: map[string]string{
			"": "Thinking...\nGenerating response...\n",
		},
	}

	manager.RefineToolUseState(term)

	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != Thinking {
		t.Errorf("state = %v, want Thinking (no approval prompt)", got)
	}
}

func TestGeminiIdlePatternVariants(t *testing.T) {
	// geminiIdlePattern が各種 Gemini ステータスバー形式に正しくマッチすることを確認する。
	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		{
			name:      "empty prompt",
			input:     " >   Type your message or @path/to/file\n workspace (/directory)                  branch      sandbox\n ~/ghq/github.com/yoshihiko555/baton     main        no sandbox\n",
			wantMatch: true,
		},
		{
			name:      "with input text",
			input:     " > some user input\n workspace (/directory)                  branch      sandbox\n ~/path     main        no sandbox\n",
			wantMatch: true,
		},
		{
			name:      "with sandbox enabled",
			input:     " > hello\n workspace (/directory)                  branch      sandbox\n ~/path     main        safe sandbox\n",
			wantMatch: true,
		},
		{
			name:      "processing (no status bar)",
			input:     "Thinking...\nGenerating response...\n",
			wantMatch: false,
		},
		{
			name:      "approval without status bar",
			input:     "Allow? [y/N]\n",
			wantMatch: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := geminiIdlePattern.MatchString(tc.input)
			if got != tc.wantMatch {
				t.Errorf("geminiIdlePattern.MatchString(%q) = %v, want %v", tc.input, got, tc.wantMatch)
			}
		})
	}
}

func TestRefineGeminiWaitingPriority(t *testing.T) {
	// ペインテキストに承認パターンとアイドルステータスバーが両方あるとき、
	// Waiting が Idle より優先されることを確認する。
	manager := NewStateManager(nil)

	result := newScanResult(newProc(500, ToolGemini, "/project"))
	if err := manager.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	term := &paneTextTerminal{
		texts: map[string]string{
			"": "Allow? [y/N]\n workspace (/directory)                  branch      sandbox\n ~/path     main        no sandbox\n",
		},
	}

	manager.RefineToolUseState(term)

	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != Waiting {
		t.Errorf("state = %v, want Waiting (approval prompt takes priority over idle status bar)", got)
	}
}

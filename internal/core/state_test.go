package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/yoshihiko555/baton/internal/hook"
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

func TestStateManagerUpdateFromScanPinsClaudeTranscript(t *testing.T) {
	projectDir := t.TempDir()
	cwd := "/workspace/shared"
	slugDir := filepath.Join(projectDir, cwdToSlug(cwd))
	pinnedPath := filepath.Join(slugDir, "session-a.jsonl")
	unpinnedPath := filepath.Join(slugDir, "session-b.jsonl")
	writeTestJSONL(t, pinnedPath, idleJSONL)
	writeTestJSONL(t, unpinnedPath, waitingJSONL)

	resolver := NewStateResolver(NewIncrementalReader(), projectDir, projectDir, time.Second)
	manager := NewStateManager(resolver)
	store := hook.NewStore(3)
	store.Apply(hook.Event{
		PaneID:         "%1",
		HookEventName:  "SessionStart",
		TranscriptPath: pinnedPath,
		SessionID:      "sess-a",
	})
	manager.SetHookStore(store)

	procs := []DetectedProcess{
		{PID: 100, ToolType: ToolClaude, PaneID: "%1", CWD: cwd},
		{PID: 200, ToolType: ToolClaude, PaneID: "%2", CWD: cwd},
	}
	if err := manager.UpdateFromScan(newScanResult(procs...)); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	byPane := sessionsByPaneID(t, manager.Projects())
	pinned := byPane["%1"]
	if pinned.TranscriptPath != pinnedPath {
		t.Errorf("pinned TranscriptPath = %q, want %q", pinned.TranscriptPath, pinnedPath)
	}
	if pinned.SessionID != "sess-a" {
		t.Errorf("pinned SessionID = %q, want sess-a", pinned.SessionID)
	}
	if pinned.State != Idle {
		t.Errorf("pinned State = %v, want %v", pinned.State, Idle)
	}
	if got := byPane["%2"].State; got != Waiting {
		t.Errorf("unpinned State = %v, want %v", got, Waiting)
	}
}

func TestStateManagerUpdateFromScanPinnedMissingFallsBack(t *testing.T) {
	projectDir := t.TempDir()
	cwd := "/workspace/shared"
	slugDir := filepath.Join(projectDir, cwdToSlug(cwd))
	writeTestJSONL(t, filepath.Join(slugDir, "session-a.jsonl"), idleJSONL)
	writeTestJSONL(t, filepath.Join(slugDir, "session-b.jsonl"), waitingJSONL)

	resolver := NewStateResolver(NewIncrementalReader(), projectDir, projectDir, time.Second)
	manager := NewStateManager(resolver)
	store := hook.NewStore(3)
	store.Apply(hook.Event{
		PaneID:         "%1",
		HookEventName:  "SessionStart",
		TranscriptPath: filepath.Join(projectDir, "missing.jsonl"),
		SessionID:      "sess-missing",
	})
	manager.SetHookStore(store)

	procs := []DetectedProcess{
		{PID: 100, ToolType: ToolClaude, PaneID: "%1", CWD: cwd},
		{PID: 200, ToolType: ToolClaude, PaneID: "%2", CWD: cwd},
	}
	if err := manager.UpdateFromScan(newScanResult(procs...)); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	pinned := sessionsByPaneID(t, manager.Projects())["%1"]
	if pinned.State == Thinking {
		t.Errorf("missing pin fallback State = %v, want a state resolved from the CWD bundle", pinned.State)
	}
}

func TestStateManagerUpdateFromScanPinsClaudeTranscriptFromOverlay(t *testing.T) {
	projectDir := t.TempDir()
	cwd := "/workspace/shared"
	bundledPath := filepath.Join(projectDir, cwdToSlug(cwd), "bundled.jsonl")
	pinnedPath := filepath.Join(projectDir, "work-profile", "pinned.jsonl")
	writeTestJSONL(t, bundledPath, waitingJSONL)
	writeTestJSONL(t, pinnedPath, idleJSONL)

	resolver := NewStateResolver(NewIncrementalReader(), projectDir, projectDir, time.Second)
	manager := NewStateManager(resolver)
	statusPath := filepath.Join(t.TempDir(), "status.json")
	writeHookStatusOverlay(t, statusPath, StatusOutput{
		Version:      2,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HookListener: true,
		Projects: []ProjectOutput{
			{Sessions: []SessionOutput{{
				PaneID:         "%1",
				SessionID:      "sess-overlay",
				TranscriptPath: pinnedPath,
			}}},
		},
	})
	manager.SetHookStatusOverlay(statusPath, time.Minute)

	proc := DetectedProcess{PID: 100, ToolType: ToolClaude, PaneID: "%1", CWD: cwd}
	if err := manager.UpdateFromScan(newScanResult(proc)); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}

	session := sessionsByPaneID(t, manager.Projects())["%1"]
	if session.TranscriptPath != pinnedPath {
		t.Errorf("TranscriptPath = %q, want %q", session.TranscriptPath, pinnedPath)
	}
	if session.SessionID != "sess-overlay" {
		t.Errorf("SessionID = %q, want sess-overlay", session.SessionID)
	}
	if session.State != Idle {
		t.Errorf("State = %v, want %v", session.State, Idle)
	}
}

func sessionsByPaneID(t *testing.T, projects []Project) map[string]*Session {
	t.Helper()
	byPane := make(map[string]*Session)
	for _, project := range projects {
		for _, session := range project.Sessions {
			if session != nil {
				byPane[session.PaneID] = session
			}
		}
	}
	return byPane
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
func (m *paneTextTerminal) SendKeys(paneID string, keys ...string) error { return nil }
func (m *paneTextTerminal) IsAvailable() bool                            { return true }
func (m *paneTextTerminal) Name() string                                 { return "mock" }

func TestRefineCodexThinkingToWaiting(t *testing.T) {
	// Codex が子プロセスありで Thinking 判定されても、承認UIがあれば Waiting に補正されることを確認する。
	manager := NewStateManager(nil)
	manager.projects = []Project{
		{
			Name: "proj",
			Path: "/project",
			Sessions: []*Session{
				{PID: 100, Tool: ToolCodex, State: Thinking, PaneID: "%1", WorkingDir: "/project"},
			},
		},
	}
	manager.summary = calcSummary(manager.projects)

	term := &paneTextTerminal{
		texts: map[string]string{
			"%1": "Run this command?\n› 1. Yes, allow once\n  2. No, tell Codex what to do differently\n",
		},
	}

	manager.RefineToolUseState(term)

	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != Waiting {
		t.Errorf("state = %v, want Waiting (codex approval prompt detected from Thinking)", got)
	}
	if got := manager.Summary().Waiting; got != 1 {
		t.Errorf("summary.Waiting = %d, want 1", got)
	}
}

func TestCodexApprovalPatternVariants(t *testing.T) {
	// Codex の選択肢UIがプロンプト記号や文言の差分を含んでも検出できることを確認する。
	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		{
			name:      "current yes choice",
			input:     "Apply changes?\n› 1. Yes, apply all changes\n  2. No, discard changes\n",
			wantMatch: true,
		},
		{
			name:      "heavy prompt symbol",
			input:     "Run command?\n❯ 1. Yes, allow once\n  2. No\n",
			wantMatch: true,
		},
		{
			name:      "allow first choice",
			input:     "Approve command?\n  1. Allow once\n  2. Deny\n",
			wantMatch: true,
		},
		{
			name:      "single choice only",
			input:     "1. Yes, proceed\n",
			wantMatch: false,
		},
		{
			name:      "unrelated numbered list",
			input:     "1. Install dependencies\n2. Run tests\n",
			wantMatch: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexApprovalPattern.MatchString(tc.input); got != tc.wantMatch {
				t.Errorf("codexApprovalPattern.MatchString(%q) = %v, want %v", tc.input, got, tc.wantMatch)
			}
		})
	}
}

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

func TestContainsApprovalPrompt(t *testing.T) {
	// containsApprovalPrompt が claudeApprovalPattern の正規表現で正しく動作することを確認する。
	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		// --- マッチすべきケース ---
		{
			name:      "standard ToolUse approval",
			input:     "Allow Bash? (y)",
			wantMatch: true,
		},
		{
			name:      "MCP tool approval",
			input:     "Allow mcp__tool_name? (y)",
			wantMatch: true,
		},
		{
			name:      "file read approval",
			input:     "Allow Read? (y)",
			wantMatch: true,
		},
		{
			name:      "do you want to allow this action",
			input:     "Do you want to allow this action?",
			wantMatch: true,
		},
		{
			name:      "do you want to run this command",
			input:     "Do you want to run this command?",
			wantMatch: true,
		},
		{
			name:      "allow once list option",
			input:     "Allow once",
			wantMatch: true,
		},
		{
			name:      "allow always list option",
			input:     "allow always",
			wantMatch: true,
		},
		{
			name:      "yes allow confirmation",
			input:     "Yes, allow",
			wantMatch: true,
		},
		{
			name:      "y/n generic marker",
			input:     "(y/n)",
			wantMatch: true,
		},
		{
			name:      "bracket y/n marker",
			input:     "[y/n]",
			wantMatch: true,
		},
		{
			name:      "bracket n/y marker",
			input:     "[n/y]",
			wantMatch: true,
		},
		{
			name:      "yes/no generic marker",
			input:     "yes/no",
			wantMatch: true,
		},
		// --- マッチすべきでないケース ---
		{
			name:      "variable name allowance",
			input:     "allowance = 100",
			wantMatch: false,
		},
		{
			name:      "different word disallow",
			input:     "disallow",
			wantMatch: false,
		},
		{
			name:      "field name approved_at",
			input:     "approved_at = time.Now()",
			wantMatch: false,
		},
		{
			name:      "allow in sentence",
			input:     "The file is allowed",
			wantMatch: false,
		},
		{
			name:      "unrelated text permission denied",
			input:     "permission denied",
			wantMatch: false,
		},
		{
			name:      "empty string",
			input:     "",
			wantMatch: false,
		},
		{
			name:      "normal multiline output",
			input:     "This is normal output\nwith multiple lines",
			wantMatch: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := containsApprovalPrompt(tc.input)
			if got != tc.wantMatch {
				t.Errorf("containsApprovalPrompt(%q) = %v, want %v", tc.input, got, tc.wantMatch)
			}
		})
	}
}

func TestRefineClaudeThinkingToWaiting(t *testing.T) {
	// Claude Thinking state is promoted to Waiting when pane text contains approval prompt.
	manager := NewStateManager(nil)
	manager.projects = []Project{
		{
			Name: "proj",
			Path: "/project",
			Sessions: []*Session{
				{PID: 100, Tool: ToolClaude, State: Thinking, PaneID: "%1", WorkingDir: "/project"},
			},
		},
	}
	manager.summary = calcSummary(manager.projects)

	term := &paneTextTerminal{
		texts: map[string]string{
			"%1": "Allow Bash? (y)\n",
		},
	}

	manager.RefineToolUseState(term)
	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != Waiting {
		t.Errorf("state = %v, want Waiting (approval prompt in Thinking state)", got)
	}
}

func TestRefineClaudeIdleToWaiting(t *testing.T) {
	// Claude Idle state is promoted to Waiting when pane text contains approval prompt.
	manager := NewStateManager(nil)
	manager.projects = []Project{
		{
			Name: "proj",
			Path: "/project",
			Sessions: []*Session{
				{PID: 100, Tool: ToolClaude, State: Idle, PaneID: "%1", WorkingDir: "/project"},
			},
		},
	}
	manager.summary = calcSummary(manager.projects)

	term := &paneTextTerminal{
		texts: map[string]string{
			"%1": "Allow Read? (y)\n",
		},
	}

	manager.RefineToolUseState(term)
	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != Waiting {
		t.Errorf("state = %v, want Waiting (approval prompt in Idle state)", got)
	}
}

func TestRefineClaudeWaitingToToolUse(t *testing.T) {
	// Claude Waiting state (JSONL-assigned) is demoted to ToolUse when pane text has no approval prompt.
	manager := NewStateManager(nil)
	manager.projects = []Project{
		{
			Name: "proj",
			Path: "/project",
			Sessions: []*Session{
				{PID: 100, Tool: ToolClaude, State: Waiting, PaneID: "%1", WorkingDir: "/project"},
			},
		},
	}
	manager.summary = calcSummary(manager.projects)

	term := &paneTextTerminal{
		texts: map[string]string{
			"%1": "Running some command...\n",
		},
	}

	manager.RefineToolUseState(term)
	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != ToolUse {
		t.Errorf("state = %v, want ToolUse (no approval prompt, demote from Waiting)", got)
	}
}

func TestRefineClaudeDiagnosticDeduplication(t *testing.T) {
	manager := NewStateManager(nil)
	manager.projects = []Project{
		{
			Name: "proj",
			Path: "/project",
			Sessions: []*Session{
				{PID: 100, Tool: ToolClaude, State: Waiting, PaneID: "%1", WorkingDir: "/project"},
			},
		},
	}
	manager.summary = calcSummary(manager.projects)

	term := &paneTextTerminal{
		texts: map[string]string{
			"%1": "Running some command...\n",
		},
	}

	manager.RefineToolUseState(term)
	firstKey := manager.lastDiagKey["%1"]
	if firstKey != "waiting|tool_use|false" {
		t.Fatalf("lastDiagKey = %q, want %q", firstKey, "waiting|tool_use|false")
	}
	if len(manager.lastDiagKey) != 1 {
		t.Fatalf("lastDiagKey length = %d, want 1", len(manager.lastDiagKey))
	}

	// 次の JSONL スキャンで Waiting が再び割り当てられた状況を再現する。
	manager.projects[0].Sessions[0].State = Waiting
	manager.RefineToolUseState(term)
	if got := manager.lastDiagKey["%1"]; got != firstKey {
		t.Fatalf("same diagnostic changed key: got %q, want %q", got, firstKey)
	}
	if len(manager.lastDiagKey) != 1 {
		t.Fatalf("lastDiagKey length after duplicate = %d, want 1", len(manager.lastDiagKey))
	}

	manager.projects[0].Sessions[0].State = Waiting
	term.texts["%1"] = "Done.\n──────────\n❯\n──────────\n"
	manager.RefineToolUseState(term)
	if got := manager.lastDiagKey["%1"]; got != "waiting|idle|true" {
		t.Fatalf("changed diagnostic key = %q, want %q", got, "waiting|idle|true")
	}

	manager.projects[0].Sessions[0].State = Idle
	manager.RefineToolUseState(term)
	if _, ok := manager.lastDiagKey["%1"]; ok {
		t.Fatal("lastDiagKey still contains pane %1 after returning to a classified non-Waiting state")
	}
}

func TestRefineClaudeToolUseStaysToolUse(t *testing.T) {
	// Claude ToolUse state remains ToolUse when pane text has no approval prompt.
	manager := NewStateManager(nil)
	manager.projects = []Project{
		{
			Name: "proj",
			Path: "/project",
			Sessions: []*Session{
				{PID: 100, Tool: ToolClaude, State: ToolUse, PaneID: "%1", WorkingDir: "/project"},
			},
		},
	}
	manager.summary = calcSummary(manager.projects)

	term := &paneTextTerminal{
		texts: map[string]string{
			"%1": "Executing tool...\n",
		},
	}

	manager.RefineToolUseState(term)
	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != ToolUse {
		t.Errorf("state = %v, want ToolUse (no approval prompt, stays ToolUse)", got)
	}
}

func TestRefineClaudeMultiSessionPaneTextAuthority(t *testing.T) {
	// Two Claude sessions with same CWD: JSONL assigned Waiting+Idle.
	// Only the session with actual approval prompt becomes Waiting.
	// The JSONL-Waiting session (no approval prompt) is demoted to ToolUse.
	manager := NewStateManager(nil)
	manager.projects = []Project{
		{
			Name: "proj",
			Path: "/project",
			Sessions: []*Session{
				{PID: 100, Tool: ToolClaude, State: Waiting, PaneID: "%1", WorkingDir: "/project"},
				{PID: 200, Tool: ToolClaude, State: Idle, PaneID: "%2", WorkingDir: "/project"},
			},
		},
	}
	manager.summary = calcSummary(manager.projects)

	term := &paneTextTerminal{
		texts: map[string]string{
			"%1": "Running command...\n",
			"%2": "Allow Bash? (y)\n",
		},
	}

	manager.RefineToolUseState(term)
	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 2 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}

	states := map[string]SessionState{}
	for _, sess := range projects[0].Sessions {
		states[sess.PaneID] = sess.State
	}

	if got := states["%1"]; got != ToolUse {
		t.Errorf("pane %%1 state = %v, want ToolUse (JSONL Waiting demoted, no approval prompt)", got)
	}
	if got := states["%2"]; got != Waiting {
		t.Errorf("pane %%2 state = %v, want Waiting (approval prompt detected)", got)
	}
}

func TestRefineClaudeThinkingToIdleByPaneText(t *testing.T) {
	// Claude の Thinking 状態がペインテキストの Idle プロンプト（❯ + 区切り線）で Idle に降格することを確認。
	// JSONL が別プロセスの Thinking を誤割り当てしている場合を補正する。
	manager := NewStateManager(nil)
	manager.projects = []Project{
		{
			Name: "proj",
			Path: "/project",
			Sessions: []*Session{
				{PID: 100, Tool: ToolClaude, State: Thinking, PaneID: "%1", WorkingDir: "/project"},
			},
		},
	}
	manager.summary = calcSummary(manager.projects)

	term := &paneTextTerminal{
		texts: map[string]string{
			"%1": "Previous output...\n────────────────────────────────\n❯ \n────────────────────────────────\n  📁 project │ 🌿 main │ 🔧 PID:100\n",
		},
	}

	manager.RefineToolUseState(term)
	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 1 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}
	if got := projects[0].Sessions[0].State; got != Idle {
		t.Errorf("state = %v, want Idle (Claude idle prompt detected)", got)
	}
}

func TestRefineClaudeIdlePatternVariants(t *testing.T) {
	// classifyClaudePane が (Idle, true) を返すかどうかで idle 判定を検証する。
	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		{
			name:      "standard idle prompt",
			input:     "output\n────────────────────────────\n❯ \n────────────────────────────\n  📁 proj\n",
			wantMatch: true,
		},
		{
			name:      "idle prompt with no trailing space",
			input:     "output\n──────────\n❯\n──────────\n  📁 proj\n",
			wantMatch: true,
		},
		{
			name:      "working output no prompt",
			input:     "✢ Enchanting… (3m 0s)\nSome output...\n",
			wantMatch: false,
		},
		{
			name:      "approval prompt not idle",
			input:     "Allow Bash? (y)\n❯ 1. Yes\n  2. No\n",
			wantMatch: false,
		},
		{
			name:      "short separator still matches",
			input:     "text\n────\n❯\n────\n  📁 proj\n",
			wantMatch: true,
		},
		{
			name:      "non-breaking space after prompt",
			input:     "output\n────────────────────\n❯\u00a0\n────────────────────\n  📁 proj\n",
			wantMatch: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state, ok := classifyClaudePane(tc.input)
			got := ok && state == Idle
			if got != tc.wantMatch {
				t.Errorf("classifyClaudePane(%q) idle = %v (state=%v, ok=%v), want %v", tc.input, got, state, ok, tc.wantMatch)
			}
		})
	}
}

func TestRefineClaudeMultiSessionIdleCorrection(t *testing.T) {
	// 同一 CWD に 2 つの Claude セッション。JSONL が Thinking+Idle を割り当てたが、
	// 実際は Thinking のペインが Idle（❯ プロンプト表示）で、Idle のペインが Working。
	// ペインテキストに基づいて状態が補正されることを確認。
	manager := NewStateManager(nil)
	manager.projects = []Project{
		{
			Name: "proj",
			Path: "/project",
			Sessions: []*Session{
				{PID: 100, Tool: ToolClaude, State: Thinking, PaneID: "%1", WorkingDir: "/project"},
				{PID: 200, Tool: ToolClaude, State: Idle, PaneID: "%2", WorkingDir: "/project"},
			},
		},
	}
	manager.summary = calcSummary(manager.projects)

	term := &paneTextTerminal{
		texts: map[string]string{
			// %1 は実際には Idle（❯ プロンプト表示）
			"%1": "Done.\n────────────────────────\n❯\n────────────────────────\n  📁 project\n",
			// %2 は Working（通常出力）
			"%2": "⏺ Thinking about the problem...\nGenerating code...\n",
		},
	}

	manager.RefineToolUseState(term)
	projects := manager.Projects()

	states := map[string]SessionState{}
	for _, sess := range projects[0].Sessions {
		states[sess.PaneID] = sess.State
	}

	if got := states["%1"]; got != Idle {
		t.Errorf("pane %%1 state = %v, want Idle (idle prompt detected, override JSONL Thinking)", got)
	}
	// %2 は Idle のまま（pane text に承認プロンプトも idle プロンプトもないが、JSONL が Idle なので維持）
	if got := states["%2"]; got != Idle {
		t.Errorf("pane %%2 state = %v, want Idle (JSONL Idle, no override needed)", got)
	}
}

func TestRefineClaudeMultiSessionPromotesPromptPaneToWaiting(t *testing.T) {
	// 同一 CWD の Claude で、承認プロンプトのあるペインは Waiting に昇格することを確認する。
	// 元々 Waiting でなかったセッションでも承認プロンプトがあれば Waiting になる。
	manager := NewStateManager(nil)
	manager.projects = []Project{
		{
			Name: "proj",
			Path: "/project",
			Sessions: []*Session{
				{PID: 100, Tool: ToolClaude, State: Thinking, PaneID: "%1", WorkingDir: "/project"},
				{PID: 200, Tool: ToolClaude, State: Idle, PaneID: "%2", WorkingDir: "/project"},
			},
		},
	}
	manager.summary = calcSummary(manager.projects)

	term := &paneTextTerminal{
		texts: map[string]string{
			"%1": "thinking...\n",
			"%2": "Do you want to continue? [y/N]\n",
		},
	}

	manager.RefineToolUseState(term)
	projects := manager.Projects()
	if len(projects) != 1 || len(projects[0].Sessions) != 2 {
		t.Fatalf("unexpected projects/sessions: %v", projects)
	}

	states := map[string]SessionState{}
	for _, sess := range projects[0].Sessions {
		states[sess.PaneID] = sess.State
	}

	if got := states["%1"]; got != Thinking {
		t.Errorf("pane %%1 state = %v, want Thinking", got)
	}
	if got := states["%2"]; got != Waiting {
		t.Errorf("pane %%2 state = %v, want Waiting", got)
	}
}

func TestClassifyClaudePane(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantState SessionState
		wantOK    bool
	}{
		// Idle: ❯ + 区切り線、上に Working シグナルなし
		{
			name:      "idle with completed message",
			input:     "output\n✻ Worked for 4m 12s\n\n────────────────────────\n❯ \n────────────────────────\n  📁 project │ 🌿 main\n  Opus 4.6\n",
			wantState: Idle,
			wantOK:    true,
		},
		{
			name:      "idle with queued messages",
			input:     "output\n────────────────────────\n❯ Press up to edit queued messages\n────────────────────────\n  📁 proj\n",
			wantState: Idle,
			wantOK:    true,
		},
		{
			name:      "idle with non-breaking space",
			input:     "output\n────────────────────────\n❯\u00a0\n────────────────────────\n  📁 proj\n",
			wantState: Idle,
			wantOK:    true,
		},
		// Working: ❯ + 区切り線あり、上に Working シグナル
		{
			name:      "working with streaming indicator",
			input:     "⏺ researcher(task)\n  Running…\n· Symbioting… (5m)\n\n────────────────────────\n❯\n────────────────────────\n  📁 proj\n",
			wantState: Thinking,
			wantOK:    true,
		},
		{
			name:      "working with running tool",
			input:     "⏺ Bash(some command)\n  ⎿  Running…\n\n────────────────────────\n❯\n────────────────────────\n  📁 proj\n",
			wantState: Thinking,
			wantOK:    true,
		},
		{
			name:      "working with checkmark indicator",
			input:     "previous\n✢ Enchanting… (3m 0s)\n\n────────────────────────\n❯\n────────────────────────\n  📁 proj\n",
			wantState: Thinking,
			wantOK:    true,
		},
		{
			name:      "working with eight-spoked asterisk indicator",
			input:     "previous\n✻ Perambulating… (37s · ↓ 933 tokens)\n\n────────────────────────\n❯\n────────────────────────\n  📁 proj\n",
			wantState: Thinking,
			wantOK:    true,
		},
		{
			name:      "working with heavy teardrop asterisk indicator",
			input:     "previous\n✽ Pondering… (1m 2s)\n\n────────────────────────\n❯\n────────────────────────\n  📁 proj\n",
			wantState: Thinking,
			wantOK:    true,
		},
		{
			name:      "working with eight-pointed asterisk indicator",
			input:     "previous\n✳ Composing… (4s)\n\n────────────────────────\n❯\n────────────────────────\n  📁 proj\n",
			wantState: Thinking,
			wantOK:    true,
		},
		{
			name:      "idle with completed message using teardrop glyph",
			input:     "output\n✽ Worked for 12s\n\n────────────────────────\n❯ \n────────────────────────\n  📁 proj\n",
			wantState: Idle,
			wantOK:    true,
		},
		{
			name:      "idle with spinner glyph inside text is not working",
			input:     "⏺ done ✻ decorated\n\n────────────────────────\n❯\n────────────────────────\n  📁 proj\n",
			wantState: Idle,
			wantOK:    true,
		},
		{
			name:      "working with six-pointed star indicator",
			input:     "previous\n✶ Envisioning… (2m 10s)\n\n────────────────────────\n❯\n────────────────────────\n  📁 proj\n",
			wantState: Thinking,
			wantOK:    true,
		},
		// Waiting: 選択肢 UI
		{
			name:      "waiting with choice UI",
			input:     "Do you want to proceed?\n❯ 1. Yes\n  2. No\n\nEsc to cancel\n",
			wantState: Waiting,
			wantOK:    true,
		},
		{
			name:      "waiting with edit approval",
			input:     "Do you want to make this edit?\n❯ 1. Yes\n  2. Yes, allow all\n  3. No\n\nEsc to cancel\n",
			wantState: Waiting,
			wantOK:    true,
		},
		{
			name:      "waiting with allow prompt",
			input:     "Allow Bash? (y)\n",
			wantState: Waiting,
			wantOK:    true,
		},
		// Fallback: 判定不能
		{
			name:      "empty text",
			input:     "",
			wantState: 0,
			wantOK:    false,
		},
		{
			name:      "no prompt structure",
			input:     "just some text\nwithout any prompt\n",
			wantState: 0,
			wantOK:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotState, gotOK := classifyClaudePane(tc.input)
			if gotOK != tc.wantOK {
				t.Errorf("classifyClaudePane ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotOK && gotState != tc.wantState {
				t.Errorf("classifyClaudePane state = %v, want %v", gotState, tc.wantState)
			}
		})
	}
}

func TestTailLines(t *testing.T) {
	tests := []struct {
		name string
		text string
		n    int
		want string
	}{
		{name: "empty string", text: "", n: 3, want: ""},
		{name: "fewer lines than n", text: "one\ntwo", n: 3, want: "one\ntwo"},
		{name: "exactly n lines", text: "one\ntwo\nthree", n: 3, want: "one\ntwo\nthree"},
		{name: "more lines than n", text: "one\ntwo\nthree\nfour", n: 2, want: "three\nfour"},
		{name: "zero lines requested", text: "one\ntwo", n: 0, want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tailLines(tc.text, tc.n); got != tc.want {
				t.Fatalf("tailLines(%q, %d) = %q, want %q", tc.text, tc.n, got, tc.want)
			}
		})
	}
}

func TestApplyHookStatesSetsWaitingAndTranscript(t *testing.T) {
	manager := NewStateManager(nil)
	session := &Session{Tool: ToolClaude, State: Thinking, PaneID: "%1"}
	manager.projects = []Project{{Sessions: []*Session{session}}}
	manager.panes = []terminal.Pane{{ID: "%1"}}

	store := hook.NewStore(3)
	store.Apply(hook.Event{
		PaneID:         "%1",
		HookEventName:  "PermissionRequest",
		SessionID:      "sess-1",
		TranscriptPath: "/path/to/transcript.jsonl",
	})
	manager.SetHookStore(store)

	manager.ApplyHookStates()

	if session.State != Waiting {
		t.Errorf("State = %v, want Waiting", session.State)
	}
	if !session.HookWaiting {
		t.Error("HookWaiting = false, want true")
	}
	if session.StateSource != SourceHook {
		t.Errorf("StateSource = %q, want %q", session.StateSource, SourceHook)
	}
	if session.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", session.SessionID, "sess-1")
	}
	if session.TranscriptPath != "/path/to/transcript.jsonl" {
		t.Errorf("TranscriptPath = %q, want %q", session.TranscriptPath, "/path/to/transcript.jsonl")
	}
}

func TestApplyHookStatesNoHookMatchSetsJSONLSource(t *testing.T) {
	manager := NewStateManager(nil)
	session := &Session{Tool: ToolClaude, State: Thinking, PaneID: "%9"}
	manager.projects = []Project{{Sessions: []*Session{session}}}
	manager.panes = []terminal.Pane{{ID: "%9"}}
	manager.SetHookStore(hook.NewStore(3))

	manager.ApplyHookStates()

	if session.StateSource != SourceJSONL {
		t.Errorf("StateSource = %q, want %q", session.StateSource, SourceJSONL)
	}
	if session.HookWaiting {
		t.Error("HookWaiting = true, want false")
	}
	if session.State != Thinking {
		t.Errorf("State = %v, want Thinking", session.State)
	}
}

func newHookStatusOverlayTestManager(t *testing.T, paneID string, state SessionState) (*StateManager, *Session) {
	t.Helper()
	resolverDir := t.TempDir()
	resolver := NewStateResolver(NewIncrementalReader(), resolverDir, resolverDir, time.Second)
	manager := NewStateManager(resolver)
	session := &Session{Tool: ToolClaude, State: state, PaneID: paneID}
	manager.projects = []Project{{Sessions: []*Session{session}}}
	manager.summary = calcSummary(manager.projects)
	return manager, session
}

func writeHookStatusOverlay(t *testing.T, path string, status StatusOutput) {
	t.Helper()
	if err := writeAtomicJSON(status, path); err != nil {
		t.Fatalf("writeAtomicJSON: %v", err)
	}
}

func applyHookStatesWithScanOverlay(t *testing.T, manager *StateManager) {
	t.Helper()
	if err := manager.UpdateFromScan(ScanResult{Err: errDummy}); err != nil {
		t.Fatalf("UpdateFromScan: %v", err)
	}
	manager.ApplyHookStates()
}

func TestApplyHookStatesStatusOverlayWaiting(t *testing.T) {
	manager, session := newHookStatusOverlayTestManager(t, "%1", Thinking)
	statusPath := filepath.Join(t.TempDir(), "status.json")
	writeHookStatusOverlay(t, statusPath, StatusOutput{
		Version:      2,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HookListener: true,
		Projects: []ProjectOutput{
			{Sessions: []SessionOutput{
				{
					PaneID:         "%1",
					SessionID:      "session-from-overlay",
					TranscriptPath: "/tmp/from-overlay.jsonl",
					StateSource:    SourceHook,
				},
			}},
		},
	})
	manager.SetHookStatusOverlay(statusPath, time.Minute)

	applyHookStatesWithScanOverlay(t, manager)

	if session.State != Waiting {
		t.Errorf("State = %v, want Waiting", session.State)
	}
	if !session.HookWaiting {
		t.Error("HookWaiting = false, want true")
	}
	if session.StateSource != SourceHook {
		t.Errorf("StateSource = %q, want %q", session.StateSource, SourceHook)
	}
	if session.SessionID != "session-from-overlay" {
		t.Errorf("SessionID = %q, want session-from-overlay", session.SessionID)
	}
	if session.TranscriptPath != "/tmp/from-overlay.jsonl" {
		t.Errorf("TranscriptPath = %q, want /tmp/from-overlay.jsonl", session.TranscriptPath)
	}
}

func TestApplyHookStatesStatusOverlayStale(t *testing.T) {
	manager, session := newHookStatusOverlayTestManager(t, "%1", Thinking)
	statusPath := filepath.Join(t.TempDir(), "status.json")
	writeHookStatusOverlay(t, statusPath, StatusOutput{
		Version:      2,
		Timestamp:    time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
		HookListener: true,
		Projects: []ProjectOutput{
			{Sessions: []SessionOutput{{PaneID: "%1", StateSource: SourceHook}}},
		},
	})
	manager.SetHookStatusOverlay(statusPath, 10*time.Second)

	applyHookStatesWithScanOverlay(t, manager)

	if session.State != Thinking {
		t.Errorf("State = %v, want Thinking", session.State)
	}
	if session.HookWaiting {
		t.Error("HookWaiting = true, want false")
	}
	if session.StateSource != SourceJSONL {
		t.Errorf("StateSource = %q, want %q", session.StateSource, SourceJSONL)
	}
}

func TestApplyHookStatesStatusOverlayCorruptedJSON(t *testing.T) {
	manager, session := newHookStatusOverlayTestManager(t, "%1", Thinking)
	statusPath := filepath.Join(t.TempDir(), "status.json")
	if err := os.WriteFile(statusPath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	manager.SetHookStatusOverlay(statusPath, time.Minute)

	applyHookStatesWithScanOverlay(t, manager)

	if session.State != Thinking {
		t.Errorf("State = %v, want Thinking", session.State)
	}
	if session.HookWaiting {
		t.Error("HookWaiting = true, want false")
	}
	if session.StateSource != SourceJSONL {
		t.Errorf("StateSource = %q, want %q", session.StateSource, SourceJSONL)
	}
}

func TestApplyHookStatesStatusOverlayWrongVersion(t *testing.T) {
	manager, session := newHookStatusOverlayTestManager(t, "%1", Thinking)
	statusPath := filepath.Join(t.TempDir(), "status.json")
	writeHookStatusOverlay(t, statusPath, StatusOutput{
		Version:      1,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HookListener: true,
		Projects: []ProjectOutput{
			{Sessions: []SessionOutput{{PaneID: "%1", StateSource: SourceHook}}},
		},
	})
	manager.SetHookStatusOverlay(statusPath, time.Minute)

	applyHookStatesWithScanOverlay(t, manager)

	if session.State != Thinking {
		t.Errorf("State = %v, want Thinking", session.State)
	}
	if session.HookWaiting {
		t.Error("HookWaiting = true, want false")
	}
	if session.StateSource != SourceJSONL {
		t.Errorf("StateSource = %q, want %q", session.StateSource, SourceJSONL)
	}
}

func TestApplyHookStatesStatusOverlayUnparsableTimestamp(t *testing.T) {
	manager, session := newHookStatusOverlayTestManager(t, "%1", Thinking)
	statusPath := filepath.Join(t.TempDir(), "status.json")
	writeHookStatusOverlay(t, statusPath, StatusOutput{
		Version:      2,
		Timestamp:    "not-a-timestamp",
		HookListener: true,
		Projects: []ProjectOutput{
			{Sessions: []SessionOutput{{PaneID: "%1", StateSource: SourceHook}}},
		},
	})
	manager.SetHookStatusOverlay(statusPath, time.Minute)

	applyHookStatesWithScanOverlay(t, manager)

	if session.State != Thinking {
		t.Errorf("State = %v, want Thinking", session.State)
	}
	if session.HookWaiting {
		t.Error("HookWaiting = true, want false")
	}
	if session.StateSource != SourceJSONL {
		t.Errorf("StateSource = %q, want %q", session.StateSource, SourceJSONL)
	}
}

func TestApplyHookStatesStatusOverlayRejectsNonListener(t *testing.T) {
	manager, session := newHookStatusOverlayTestManager(t, "%1", Thinking)
	statusPath := filepath.Join(t.TempDir(), "status.json")
	residentState := NewStateManager(nil)
	residentState.projects = []Project{
		{Sessions: []*Session{{
			Tool:        ToolClaude,
			State:       Waiting,
			PaneID:      "%1",
			StateSource: SourceHook,
			HookWaiting: true,
		}}},
	}
	residentState.summary = calcSummary(residentState.projects)
	if err := NewExporter(statusPath, ExporterConfig{}).Write(residentState); err != nil {
		t.Fatalf("Exporter.Write: %v", err)
	}
	manager.SetHookStatusOverlay(statusPath, time.Minute)

	applyHookStatesWithScanOverlay(t, manager)

	if session.State != Thinking {
		t.Errorf("State = %v, want Thinking", session.State)
	}
	if session.HookWaiting {
		t.Error("HookWaiting = true, want false")
	}
	if session.StateSource != SourceJSONL {
		t.Errorf("StateSource = %q, want %q", session.StateSource, SourceJSONL)
	}
}

func TestApplyHookStatesStatusOverlayIgnoresUnmatchedPane(t *testing.T) {
	manager, session := newHookStatusOverlayTestManager(t, "%1", Thinking)
	statusPath := filepath.Join(t.TempDir(), "status.json")
	writeHookStatusOverlay(t, statusPath, StatusOutput{
		Version:      2,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HookListener: true,
		Projects: []ProjectOutput{
			{Sessions: []SessionOutput{{PaneID: "%9", StateSource: SourceHook}}},
		},
	})
	manager.SetHookStatusOverlay(statusPath, time.Minute)

	applyHookStatesWithScanOverlay(t, manager)

	if session.State != Thinking || session.HookWaiting || session.StateSource != SourceJSONL {
		t.Errorf("session = %#v, want unchanged state with JSONL source", session)
	}
}

func TestApplyHookStatesStatusOverlayCopiesCorrelationWithoutHookState(t *testing.T) {
	manager, session := newHookStatusOverlayTestManager(t, "%1", Thinking)
	statusPath := filepath.Join(t.TempDir(), "status.json")
	writeHookStatusOverlay(t, statusPath, StatusOutput{
		Version:      2,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		HookListener: true,
		Projects: []ProjectOutput{
			{Sessions: []SessionOutput{
				{
					PaneID:         "%1",
					SessionID:      "session-from-pane-source",
					TranscriptPath: "/tmp/from-pane-source.jsonl",
					StateSource:    SourcePane,
				},
			}},
		},
	})
	manager.SetHookStatusOverlay(statusPath, time.Minute)

	applyHookStatesWithScanOverlay(t, manager)

	if session.SessionID != "session-from-pane-source" {
		t.Errorf("SessionID = %q, want session-from-pane-source", session.SessionID)
	}
	if session.TranscriptPath != "/tmp/from-pane-source.jsonl" {
		t.Errorf("TranscriptPath = %q, want /tmp/from-pane-source.jsonl", session.TranscriptPath)
	}
	if session.State != Thinking {
		t.Errorf("State = %v, want Thinking", session.State)
	}
	if session.HookWaiting {
		t.Error("HookWaiting = true, want false")
	}
	if session.StateSource != SourceJSONL {
		t.Errorf("StateSource = %q, want %q", session.StateSource, SourceJSONL)
	}
}

func TestApplyHookStatesRetainPanesRemovesStalePane(t *testing.T) {
	store := hook.NewStore(3)
	store.Apply(hook.Event{PaneID: "%1", HookEventName: "PermissionRequest"})

	manager := NewStateManager(nil)
	manager.SetHookStore(store)
	manager.panes = []terminal.Pane{{ID: "%2"}}
	manager.projects = []Project{
		{Sessions: []*Session{{Tool: ToolClaude, State: Thinking, PaneID: "%2"}}},
	}

	manager.ApplyHookStates()

	if _, ok := store.Get("%1"); ok {
		t.Error("stale pane %1 remains in hook store")
	}
}

func TestRefineToolUseStateHookWaitingBlocksOverwrite(t *testing.T) {
	manager := NewStateManager(nil)
	session := &Session{Tool: ToolClaude, State: Waiting, HookWaiting: true, PaneID: "%1"}
	manager.projects = []Project{{Sessions: []*Session{session}}}
	manager.summary = calcSummary(manager.projects)

	store := hook.NewStore(3)
	store.Apply(hook.Event{PaneID: "%1", HookEventName: "PermissionRequest"})
	manager.SetHookStore(store)

	term := &paneTextTerminal{texts: map[string]string{
		"%1": "Running some command...\n",
	}}
	manager.RefineToolUseState(term)

	if session.State != Waiting {
		t.Errorf("State = %v, want Waiting", session.State)
	}
}

func TestHookWaitingIdleStreakClearsAfterThreshold(t *testing.T) {
	store := hook.NewStore(2)
	store.Apply(hook.Event{
		PaneID:        "%1",
		HookEventName: "PermissionRequest",
		SessionID:     "s1",
	})

	manager := NewStateManager(nil)
	manager.SetHookStore(store)
	term := &paneTextTerminal{texts: map[string]string{
		"%1": "some output\n────────────────────\n❯ \n",
	}}
	wantStates := []SessionState{Waiting, Waiting, Idle}

	for round, want := range wantStates {
		session := &Session{Tool: ToolClaude, PaneID: "%1", State: Idle}
		manager.projects = []Project{{Sessions: []*Session{session}}}
		manager.panes = []terminal.Pane{{ID: "%1"}}

		manager.ApplyHookStates()
		manager.RefineToolUseState(term)

		if session.State != want {
			t.Errorf("round %d: State = %v, want %v", round+1, session.State, want)
		}
	}
}

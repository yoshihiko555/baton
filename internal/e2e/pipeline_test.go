package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/yoshihiko555/baton/internal/core"
	"github.com/yoshihiko555/baton/internal/terminal"
)

// --- mock terminal ---

type mockTerminal struct {
	panes    []terminal.Pane
	paneText map[string]string // paneID → text
}

func (m *mockTerminal) ListPanes() ([]terminal.Pane, error) { return m.panes, nil }
func (m *mockTerminal) FocusPane(paneID string) error       { return nil }
func (m *mockTerminal) GetPaneText(paneID string) (string, error) {
	if m.paneText != nil {
		if text, ok := m.paneText[paneID]; ok {
			return text, nil
		}
	}
	return "", nil
}
func (m *mockTerminal) IsAvailable() bool { return true }
func (m *mockTerminal) Name() string      { return "mock" }

// --- mock execFn builder ---

// exitCode1Cache caches a real exec.ExitError with exit code 1.
// Avoids forking sh on every call.
var exitCode1Cache struct {
	once sync.Once
	err  error
}

// makeExitCode1Error returns a real exec.ExitError with exit code 1.
// pgrep returns exit code 1 when no processes match;
// HasChildProcesses checks errors.As(*exec.ExitError) && ExitCode() == 1.
func makeExitCode1Error() error {
	exitCode1Cache.once.Do(func() {
		_, exitCode1Cache.err = exec.Command("sh", "-c", "exit 1").Output()
	})
	return exitCode1Cache.err
}

// psLine builds a single line of `ps -t <tty> -o pid,ppid,comm,args` output.
func psLine(pid, ppid int, comm, args string) string {
	return fmt.Sprintf("%d %d %s %s", pid, ppid, comm, args)
}

// buildExecFn creates a mock execFn that returns canned output for ps and pgrep commands.
// psOutputByTTY: normalized TTY → ps output lines (excluding header).
// pgrepOutputByPID: parent PID → child PID list (one per line).
// childCommByPID: child PID → COMM name.
func buildExecFn(
	psOutputByTTY map[string]string,
	pgrepOutputByPID map[int]string,
	childCommByPID map[int]string,
) func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch name {
		case "ps":
			// ps -t <tty> -o pid,ppid,comm,args
			for i, a := range args {
				if a == "-t" && i+1 < len(args) {
					tty := args[i+1]
					if output, ok := psOutputByTTY[tty]; ok {
						return []byte("PID PPID COMM ARGS\n" + output), nil
					}
					return []byte("PID PPID COMM ARGS\n"), nil
				}
			}
			// ps -p <pids> -o comm=
			for i, a := range args {
				if a == "-p" && i+1 < len(args) {
					pids := strings.Split(args[i+1], ",")
					var lines []string
					for _, pidStr := range pids {
						pid, _ := strconv.Atoi(strings.TrimSpace(pidStr))
						if comm, ok := childCommByPID[pid]; ok {
							lines = append(lines, comm)
						}
					}
					return []byte(strings.Join(lines, "\n") + "\n"), nil
				}
			}
			return []byte(""), nil

		case "pgrep":
			// pgrep -P <pid>
			for i, a := range args {
				if a == "-P" && i+1 < len(args) {
					pidStr := args[i+1]
					pid, _ := strconv.Atoi(pidStr)
					if output, ok := pgrepOutputByPID[pid]; ok {
						return []byte(output), nil
					}
					// No children → exit code 1 (real ExitError needed for errors.As)
					return nil, makeExitCode1Error()
				}
			}
			return nil, makeExitCode1Error()

		default:
			return nil, fmt.Errorf("unexpected command: %s", name)
		}
	}
}

// --- Pipeline E2E Tests ---

// TestPipelineClaude_ThinkingState tests that a Claude process detected via ps
// results in Thinking state when no JSONL resolution is available.
func TestPipelineClaude_ThinkingState(t *testing.T) {
	term := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "%1", TTYName: "/dev/ttys001", WorkingDir: "/project-a", CurrentCommand: "claude"},
		},
	}

	ps := core.NewProcessScannerWithExec(buildExecFn(
		map[string]string{
			"ttys001": psLine(1001, 500, "claude", "claude --resume"),
		},
		nil, nil,
	))

	scanner := core.NewDefaultScanner(term, ps)

	// StateManager with nil resolver → Claude defaults to Thinking
	sm := core.NewStateManager(nil)
	sm.SetProcessScanner(ps)

	result := scanner.Scan(context.Background())
	if result.Err != nil {
		t.Fatalf("Scan error: %v", result.Err)
	}
	if len(result.Processes) != 1 {
		t.Fatalf("expected 1 process, got %d", len(result.Processes))
	}
	if result.Processes[0].ToolType != core.ToolClaude {
		t.Errorf("expected ToolClaude, got %v", result.Processes[0].ToolType)
	}

	if err := sm.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan error: %v", err)
	}

	projects := sm.Projects()
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if len(projects[0].Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(projects[0].Sessions))
	}

	sess := projects[0].Sessions[0]
	if sess.State != core.Thinking {
		t.Errorf("expected Thinking state, got %v", sess.State)
	}
	if sess.Tool != core.ToolClaude {
		t.Errorf("expected ToolClaude, got %v", sess.Tool)
	}
	if sess.PID != 1001 {
		t.Errorf("expected PID 1001, got %d", sess.PID)
	}
}

// TestPipelineCodex_IdleAndThinking tests Codex state detection via child process inspection.
func TestPipelineCodex_IdleAndThinking(t *testing.T) {
	term := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "%1", TTYName: "/dev/ttys001", WorkingDir: "/project-a", CurrentCommand: "codex"},
			{ID: "%2", TTYName: "/dev/ttys002", WorkingDir: "/project-b", CurrentCommand: "codex"},
		},
	}

	ps := core.NewProcessScannerWithExec(buildExecFn(
		map[string]string{
			// Codex with active child (sandbox) → Thinking
			"ttys001": psLine(2001, 500, "codex", "codex --full-auto"),
			// Codex with no active children → Idle
			"ttys002": psLine(2002, 500, "codex", "codex --full-auto"),
		},
		map[int]string{
			// PID 2001 has a child (sandbox process)
			2001: "3001\n",
			// PID 2002 has only a background process (node/MCP)
			2002: "3002\n",
		},
		map[int]string{
			3001: "sandbox-exec", // active child → Thinking
			3002: "node",         // background → filtered out → Idle
		},
	))

	scanner := core.NewDefaultScanner(term, ps)
	sm := core.NewStateManager(nil)
	sm.SetProcessScanner(ps)

	result := scanner.Scan(context.Background())
	if err := sm.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan error: %v", err)
	}

	projects := sm.Projects()

	// Find sessions by PID
	var thinkingFound, idleFound bool
	for _, p := range projects {
		for _, s := range p.Sessions {
			if s == nil {
				continue
			}
			switch s.PID {
			case 2001:
				if s.State != core.Thinking {
					t.Errorf("PID 2001 (active child): expected Thinking, got %v", s.State)
				}
				thinkingFound = true
			case 2002:
				if s.State != core.Idle {
					t.Errorf("PID 2002 (bg-only child): expected Idle, got %v", s.State)
				}
				idleFound = true
			}
		}
	}
	if !thinkingFound {
		t.Error("PID 2001 (Thinking) not found in projects")
	}
	if !idleFound {
		t.Error("PID 2002 (Idle) not found in projects")
	}
}

// TestPipelineGemini_DetectedViaArgs tests Gemini detection via ARGS fallback
// when COMM is "node".
func TestPipelineGemini_DetectedViaArgs(t *testing.T) {
	term := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "%1", TTYName: "/dev/ttys001", WorkingDir: "/project-a", CurrentCommand: "node"},
		},
	}

	ps := core.NewProcessScannerWithExec(buildExecFn(
		map[string]string{
			// COMM=node but ARGS contains gemini → detected as Gemini
			"ttys001": psLine(4001, 500, "node", "/usr/local/bin/gemini -m gemini-2.5-pro"),
		},
		nil, nil,
	))

	scanner := core.NewDefaultScanner(term, ps)
	sm := core.NewStateManager(nil)
	sm.SetProcessScanner(ps)

	result := scanner.Scan(context.Background())
	if err := sm.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan error: %v", err)
	}

	projects := sm.Projects()
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}

	sess := projects[0].Sessions[0]
	if sess.Tool != core.ToolGemini {
		t.Errorf("expected ToolGemini, got %v", sess.Tool)
	}
	if sess.State != core.Thinking {
		t.Errorf("expected Thinking (default for Gemini), got %v", sess.State)
	}
}

// TestPipelineGemini_RefineToIdle tests that Gemini Thinking → Idle
// when pane text contains the idle prompt pattern.
func TestPipelineGemini_RefineToIdle(t *testing.T) {
	term := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "%1", TTYName: "/dev/ttys001", WorkingDir: "/project-a", CurrentCommand: "node"},
		},
		paneText: map[string]string{
			"%1": "workspace (default)   sandbox on",
		},
	}

	ps := core.NewProcessScannerWithExec(buildExecFn(
		map[string]string{
			"ttys001": psLine(4001, 500, "node", "/usr/local/bin/gemini -m gemini-2.5-pro"),
		},
		nil, nil,
	))

	scanner := core.NewDefaultScanner(term, ps)
	sm := core.NewStateManager(nil)
	sm.SetProcessScanner(ps)

	result := scanner.Scan(context.Background())
	if err := sm.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan error: %v", err)
	}
	sm.RefineToolUseState(term)

	projects := sm.Projects()
	sess := projects[0].Sessions[0]
	if sess.State != core.Idle {
		t.Errorf("expected Idle after refine (idle prompt detected), got %v", sess.State)
	}
}

// TestPipelineGemini_RefineToWaiting tests that Gemini Thinking → Waiting
// when pane text contains an approval prompt.
func TestPipelineGemini_RefineToWaiting(t *testing.T) {
	term := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "%1", TTYName: "/dev/ttys001", WorkingDir: "/project-a", CurrentCommand: "node"},
		},
		paneText: map[string]string{
			"%1": "Do you want to allow this tool execution? (y/n)",
		},
	}

	ps := core.NewProcessScannerWithExec(buildExecFn(
		map[string]string{
			"ttys001": psLine(4001, 500, "node", "/usr/local/bin/gemini -m gemini-2.5-pro"),
		},
		nil, nil,
	))

	scanner := core.NewDefaultScanner(term, ps)
	sm := core.NewStateManager(nil)
	sm.SetProcessScanner(ps)

	result := scanner.Scan(context.Background())
	if err := sm.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan error: %v", err)
	}
	sm.RefineToolUseState(term)

	projects := sm.Projects()
	sess := projects[0].Sessions[0]
	if sess.State != core.Waiting {
		t.Errorf("expected Waiting after refine (approval prompt detected), got %v", sess.State)
	}
}

// TestPipelineCodex_RefineToWaiting tests that Codex Idle → Waiting
// when pane text contains the numbered approval pattern.
func TestPipelineCodex_RefineToWaiting(t *testing.T) {
	term := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "%1", TTYName: "/dev/ttys001", WorkingDir: "/project-a", CurrentCommand: "codex"},
		},
		paneText: map[string]string{
			"%1": "Apply the changes?\n  1. Yes, apply all changes\n  2. No, discard changes\n",
		},
	}

	ps := core.NewProcessScannerWithExec(buildExecFn(
		map[string]string{
			"ttys001": psLine(2001, 500, "codex", "codex --full-auto"),
		},
		nil, // no children → Idle
		nil,
	))

	scanner := core.NewDefaultScanner(term, ps)
	sm := core.NewStateManager(nil)
	sm.SetProcessScanner(ps)

	result := scanner.Scan(context.Background())
	if err := sm.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan error: %v", err)
	}
	sm.RefineToolUseState(term)

	projects := sm.Projects()
	sess := projects[0].Sessions[0]
	if sess.State != core.Waiting {
		t.Errorf("expected Waiting after refine (Codex approval pattern detected), got %v", sess.State)
	}
}

// TestPipelineMixedTools tests a multi-tool scenario with Claude, Codex, and Gemini
// across different panes, verifying summary aggregation.
func TestPipelineMixedTools(t *testing.T) {
	term := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "%1", TTYName: "/dev/ttys001", WorkingDir: "/project-a", CurrentCommand: "claude"},
			{ID: "%2", TTYName: "/dev/ttys002", WorkingDir: "/project-b", CurrentCommand: "codex"},
			{ID: "%3", TTYName: "/dev/ttys003", WorkingDir: "/project-c", CurrentCommand: "node"},
		},
	}

	ps := core.NewProcessScannerWithExec(buildExecFn(
		map[string]string{
			"ttys001": psLine(1001, 500, "claude", "claude"),
			"ttys002": psLine(2001, 500, "codex", "codex"),
			"ttys003": psLine(4001, 500, "node", "/usr/local/bin/gemini"),
		},
		nil, nil,
	))

	scanner := core.NewDefaultScanner(term, ps)
	sm := core.NewStateManager(nil)
	sm.SetProcessScanner(ps)

	result := scanner.Scan(context.Background())
	if err := sm.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan error: %v", err)
	}

	summary := sm.Summary()
	if summary.TotalSessions != 3 {
		t.Errorf("expected 3 total sessions, got %d", summary.TotalSessions)
	}
	if summary.ByTool["claude"] != 1 {
		t.Errorf("expected 1 claude session, got %d", summary.ByTool["claude"])
	}
	if summary.ByTool["codex"] != 1 {
		t.Errorf("expected 1 codex session, got %d", summary.ByTool["codex"])
	}
	if summary.ByTool["gemini"] != 1 {
		t.Errorf("expected 1 gemini session, got %d", summary.ByTool["gemini"])
	}
}

// TestPipelineExporterJSON tests the full pipeline through to JSON export.
func TestPipelineExporterJSON(t *testing.T) {
	term := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "%1", TTYName: "/dev/ttys001", WorkingDir: "/project-a", CurrentCommand: "claude"},
			{ID: "%2", TTYName: "/dev/ttys002", WorkingDir: "/project-b", CurrentCommand: "codex"},
		},
	}

	ps := core.NewProcessScannerWithExec(buildExecFn(
		map[string]string{
			"ttys001": psLine(1001, 500, "claude", "claude"),
			"ttys002": psLine(2001, 500, "codex", "codex"),
		},
		map[int]string{
			2001: "3001\n",
		},
		map[int]string{
			3001: "sandbox-exec",
		},
	))

	scanner := core.NewDefaultScanner(term, ps)
	sm := core.NewStateManager(nil)
	sm.SetProcessScanner(ps)

	result := scanner.Scan(context.Background())
	if err := sm.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan error: %v", err)
	}

	// Export to temp file
	outPath := filepath.Join(t.TempDir(), "baton-status.json")
	exporter := core.NewExporter(outPath, core.ExporterConfig{})
	if err := exporter.Write(sm); err != nil {
		t.Fatalf("Exporter.Write error: %v", err)
	}

	// Read and verify JSON
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read exported JSON: %v", err)
	}

	var status core.StatusOutput
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if status.Version != 2 {
		t.Errorf("expected version 2, got %d", status.Version)
	}
	if status.Summary.TotalSessions != 2 {
		t.Errorf("expected 2 total sessions, got %d", status.Summary.TotalSessions)
	}
	if len(status.Projects) < 1 {
		t.Fatal("expected at least 1 project in output")
	}

	// Verify each session appears in the output
	sessionsByTool := make(map[string]bool)
	for _, p := range status.Projects {
		for _, s := range p.Sessions {
			sessionsByTool[s.Tool] = true
		}
	}
	if !sessionsByTool["claude"] {
		t.Error("expected claude session in exported JSON")
	}
	if !sessionsByTool["codex"] {
		t.Error("expected codex session in exported JSON")
	}
}

// TestPipelineParentChildDedup tests that parent-child process deduplication works
// correctly in the full pipeline.
func TestPipelineParentChildDedup(t *testing.T) {
	term := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "%1", TTYName: "/dev/ttys001", WorkingDir: "/project-a", CurrentCommand: "node"},
		},
	}

	// node (parent, PID=4000) spawns gemini (child, PID=4001)
	// Only the parent should be kept after dedup.
	ps := core.NewProcessScannerWithExec(buildExecFn(
		map[string]string{
			"ttys001": psLine(4000, 500, "node", "/usr/local/bin/gemini") + "\n" +
				psLine(4001, 4000, "node", "/usr/local/bin/gemini worker"),
		},
		nil, nil,
	))

	scanner := core.NewDefaultScanner(term, ps)
	sm := core.NewStateManager(nil)
	sm.SetProcessScanner(ps)

	result := scanner.Scan(context.Background())
	if err := sm.UpdateFromScan(result); err != nil {
		t.Fatalf("UpdateFromScan error: %v", err)
	}

	projects := sm.Projects()
	totalSessions := 0
	for _, p := range projects {
		totalSessions += len(p.Sessions)
	}
	if totalSessions != 1 {
		t.Errorf("expected 1 session after parent-child dedup, got %d", totalSessions)
	}
}

// TestPipelineNonAIPaneSkipped tests that panes running non-AI commands
// are skipped by the scanner (tmux CurrentCommand optimization).
func TestPipelineNonAIPaneSkipped(t *testing.T) {
	psCalled := false
	execFn := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ps" {
			psCalled = true
		}
		return []byte("PID PPID COMM ARGS\n"), nil
	}

	term := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "%1", TTYName: "/dev/ttys001", WorkingDir: "/project-a", CurrentCommand: "bash"},
			{ID: "%2", TTYName: "/dev/ttys002", WorkingDir: "/project-b", CurrentCommand: "vim"},
		},
	}

	ps := core.NewProcessScannerWithExec(execFn)
	scanner := core.NewDefaultScanner(term, ps)

	result := scanner.Scan(context.Background())
	if result.Err != nil {
		t.Fatalf("Scan error: %v", result.Err)
	}
	if psCalled {
		t.Error("ps should not be called for non-AI panes (bash, vim)")
	}
	if len(result.Processes) != 0 {
		t.Errorf("expected 0 processes from non-AI panes, got %d", len(result.Processes))
	}
}

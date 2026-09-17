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
func (m *mockTerminal) SendKeys(paneID string, keys ...string) error {
	return nil
}
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

// TestPipelineMixedTools tests a multi-tool scenario with Claude, Codex, agy, and
// OpenCode across different panes, verifying summary aggregation.
func TestPipelineMixedTools(t *testing.T) {
	term := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "%1", TTYName: "/dev/ttys001", WorkingDir: "/project-a", CurrentCommand: "claude"},
			{ID: "%2", TTYName: "/dev/ttys002", WorkingDir: "/project-b", CurrentCommand: "codex"},
			{ID: "%3", TTYName: "/dev/ttys003", WorkingDir: "/project-c", CurrentCommand: "agy"},
			{ID: "%4", TTYName: "/dev/ttys004", WorkingDir: "/project-d", CurrentCommand: ".opencode-wrapp"},
		},
	}

	ps := core.NewProcessScannerWithExec(buildExecFn(
		map[string]string{
			"ttys001": psLine(1001, 500, "claude", "claude"),
			"ttys002": psLine(2001, 500, "codex", "codex"),
			"ttys003": psLine(4001, 500, "agy", "agy"),
			"ttys004": psLine(5001, 500, "opencode", "opencode"),
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
	if summary.TotalSessions != 4 {
		t.Errorf("expected 4 total sessions, got %d", summary.TotalSessions)
	}
	if summary.ByTool["claude"] != 1 {
		t.Errorf("expected 1 claude session, got %d", summary.ByTool["claude"])
	}
	if summary.ByTool["codex"] != 1 {
		t.Errorf("expected 1 codex session, got %d", summary.ByTool["codex"])
	}
	if summary.ByTool["agy"] != 1 {
		t.Errorf("expected 1 agy session, got %d", summary.ByTool["agy"])
	}
	if summary.ByTool["opencode"] != 1 {
		t.Errorf("expected 1 opencode session, got %d", summary.ByTool["opencode"])
	}
}

// TestPipelineTaktLabel tests that a claude -p spawned by takt (stdio=pipe, ancestry via
// node_modules/takt/) is labelled Via=takt, does not consume the CWD-bundled state slot of a
// sibling interactive claude session in the same CWD, and that pane refinement is skipped for
// it (screen text that would otherwise be misclassified as Waiting has no effect). It also
// verifies that a genuinely interactive claude session in the same CWD is still refined
// normally, and that "via" appears in the exported status JSON only for the takt session
// (ADR-0016 Decision 3).
func TestPipelineTaktLabel(t *testing.T) {
	term := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "%1", TTYName: "/dev/ttys001", WorkingDir: "/project-a", CurrentCommand: "claude"},
			// takt 自身の stdout が pane に映る（claude の stdio は pipe のため見えない）。
			{ID: "%2", TTYName: "/dev/ttys002", WorkingDir: "/project-a", CurrentCommand: "node"},
		},
		paneText: map[string]string{
			"%1": "Done.\n──────────\n❯\n──────────\n",
			// takt 配下セッションで pane 精緻化がスキップされないと誤って Waiting 判定されるテキスト。
			"%2": "❯ 1. Yes, proceed\n  2. No\n",
		},
	}

	ps := core.NewProcessScannerWithExec(buildExecFn(
		map[string]string{
			"ttys001": psLine(1001, 900, "claude", "claude"),
			"ttys002": psLine(1000, 1, "zsh", "-zsh") + "\n" +
				psLine(2000, 1000, "node", "node /Users/user/project-a/node_modules/takt/dist/app/cli/index.js run") + "\n" +
				psLine(3000, 2000, "claude", "claude -p --verbose --output-format stream-json"),
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

	byPID := make(map[int]*core.Session)
	for _, p := range sm.Projects() {
		for _, s := range p.Sessions {
			byPID[s.PID] = s
		}
	}

	interactive, ok := byPID[1001]
	if !ok {
		t.Fatalf("interactive session (PID 1001) not found")
	}
	if interactive.Via != "" {
		t.Errorf("interactive session Via = %q, want empty", interactive.Via)
	}
	if interactive.State != core.Idle {
		t.Errorf("interactive session State = %v, want Idle (pane refinement must still run)", interactive.State)
	}

	taktSess, ok := byPID[3000]
	if !ok {
		t.Fatalf("takt-spawned claude session (PID 3000) not found")
	}
	if taktSess.Via != "takt" {
		t.Errorf("takt session Via = %q, want %q", taktSess.Via, "takt")
	}
	if taktSess.State == core.Waiting {
		t.Errorf("takt session State = %v, want not Waiting (pane refinement must be skipped, screen text must not leak in)", taktSess.State)
	}

	// Export と JSON 出力の確認
	outPath := filepath.Join(t.TempDir(), "baton-status.json")
	exporter := core.NewExporter(outPath, core.ExporterConfig{})
	if err := exporter.Write(sm); err != nil {
		t.Fatalf("Exporter.Write error: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read exported JSON: %v", err)
	}
	if !strings.Contains(string(data), `"via": "takt"`) {
		t.Errorf(`exported JSON does not contain "via": "takt":\n%s`, data)
	}

	var status core.StatusOutput
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	for _, p := range status.Projects {
		for _, s := range p.Sessions {
			switch s.PID {
			case 1001:
				if s.Via != "" {
					t.Errorf("exported interactive session via = %q, want empty", s.Via)
				}
			case 3000:
				if s.Via != "takt" {
					t.Errorf("exported takt session via = %q, want %q", s.Via, "takt")
				}
			}
		}
	}
}

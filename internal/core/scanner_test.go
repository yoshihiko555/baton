package core

import (
	"context"
	"testing"

	"github.com/yoshihiko555/baton/internal/terminal"
)

// mockTerminal is a test double for terminal.Terminal.
type mockTerminal struct {
	panes []terminal.Pane
}

func (m *mockTerminal) ListPanes() ([]terminal.Pane, error) { return m.panes, nil }
func (m *mockTerminal) FocusPane(paneID string) error       { return nil }
func (m *mockTerminal) GetPaneText(paneID string) (string, error) {
	return "", nil
}
func (m *mockTerminal) SendKeys(paneID string, keys ...string) error { return nil }
func (m *mockTerminal) IsAvailable() bool                            { return true }
func (m *mockTerminal) Name() string                                 { return "mock" }

// newTrackingScanner returns a ProcessScanner whose execFn records normalized TTY names called.
func newTrackingScanner(called map[string]bool) *ProcessScanner {
	return NewProcessScannerWithExec(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// args: ["-t", "<tty>", "-o", "pid,ppid,comm"]
		for i, a := range args {
			if a == "-t" && i+1 < len(args) {
				called[args[i+1]] = true
			}
		}
		// Return header-only output so no processes are detected.
		return []byte("PID PPID COMM\n"), nil
	})
}

func TestScanCurrentCommandClaude(t *testing.T) {
	called := map[string]bool{}
	mt := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "1", TTYName: "/dev/ttys001", CurrentCommand: "claude"},
		},
	}
	ps := newTrackingScanner(called)
	sc := NewDefaultScanner(mt, ps)

	sc.Scan(context.Background())

	if !called["ttys001"] {
		t.Error("expected FindAIProcesses to be called for ttys001 (claude is an AI command), but it was not")
	}
}

func TestScanCurrentCommandBash(t *testing.T) {
	called := map[string]bool{}
	mt := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "2", TTYName: "/dev/ttys002", CurrentCommand: "bash"},
		},
	}
	ps := newTrackingScanner(called)
	sc := NewDefaultScanner(mt, ps)

	sc.Scan(context.Background())

	if called["ttys002"] {
		t.Error("expected FindAIProcesses to be skipped for ttys002 (bash is not an AI command), but it was called")
	}
}

func TestScanCurrentCommandNode(t *testing.T) {
	called := map[string]bool{}
	mt := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "5", TTYName: "/dev/ttys005", CurrentCommand: "node"},
		},
	}
	ps := newTrackingScanner(called)
	sc := NewDefaultScanner(mt, ps)

	sc.Scan(context.Background())

	if !called["ttys005"] {
		t.Error("expected FindAIProcesses to be called for ttys005 (node is an AI runtime for Gemini CLI), but it was not")
	}
}

func TestScanCurrentCommandEmpty(t *testing.T) {
	called := map[string]bool{}
	mt := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "3", TTYName: "/dev/ttys003", CurrentCommand: ""},
		},
	}
	ps := newTrackingScanner(called)
	sc := NewDefaultScanner(mt, ps)

	sc.Scan(context.Background())

	if !called["ttys003"] {
		t.Error("expected FindAIProcesses to be called for ttys003 (empty CurrentCommand = WezTerm compat), but it was not")
	}
}

func TestScanCurrentCommandCaseInsensitive(t *testing.T) {
	called := map[string]bool{}
	mt := &mockTerminal{
		panes: []terminal.Pane{
			{ID: "4", TTYName: "/dev/ttys004", CurrentCommand: "Codex"},
		},
	}
	ps := newTrackingScanner(called)
	sc := NewDefaultScanner(mt, ps)

	sc.Scan(context.Background())

	if !called["ttys004"] {
		t.Error("expected FindAIProcesses to be called for ttys004 (Codex is AI command, case-insensitive), but it was not")
	}
}

func TestIsAICommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"claude", true},
		{"codex", true},
		{"gemini", true},
		{"Claude", true},
		{"bash", false},
		{"node", true},
		{"zsh", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.cmd, func(t *testing.T) {
			got := isAICommand(tc.cmd)
			if got != tc.want {
				t.Errorf("isAICommand(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

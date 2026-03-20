package terminal

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestTmuxListPanes(t *testing.T) {
	sampleOutput := strings.Join([]string{
		"main\t1\t0\t0\t%0\teditor\tclaude\t/home/user/project\t/dev/ttys001",
		"main\t1\t0\t1\t%1\tlogs\tbash\t/home/user/project\t/dev/ttys002",
		"work\t1\t1\t0\t%2\tserver\tnode\t/home/user/app\t/dev/ttys003",
		"", // trailing newline
	}, "\n")

	tmx := &TmuxTerminal{
		execFn: func(args ...string) ([]byte, error) {
			if len(args) < 2 || args[0] != "list-panes" {
				t.Fatalf("unexpected command: %v", args)
			}
			return []byte(sampleOutput), nil
		},
	}

	got, err := tmx.ListPanes()
	if err != nil {
		t.Fatalf("ListPanes returned error: %v", err)
	}

	want := []Pane{
		{
			ID:              "%0",
			Title:           "editor",
			WorkingDir:      "/home/user/project",
			TTYName:         "/dev/ttys001",
			CurrentCommand:  "claude",
			SessionName:     "main",
			SessionAttached: true,
			WindowIndex:     0,
			PaneIndex:       0,
		},
		{
			ID:              "%1",
			Title:           "logs",
			WorkingDir:      "/home/user/project",
			TTYName:         "/dev/ttys002",
			CurrentCommand:  "bash",
			SessionName:     "main",
			SessionAttached: true,
			WindowIndex:     0,
			PaneIndex:       1,
		},
		{
			ID:              "%2",
			Title:           "server",
			WorkingDir:      "/home/user/app",
			TTYName:         "/dev/ttys003",
			CurrentCommand:  "node",
			SessionName:     "work",
			SessionAttached: true,
			WindowIndex:     1,
			PaneIndex:       0,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected panes:\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestTmuxListPanesHookSessionExcluded(t *testing.T) {
	// claude-xxx-12345 形式の unattached セッションは除外される。
	sampleOutput := strings.Join([]string{
		"main\t1\t0\t0\t%0\teditor\tclaude\t/home/user/project\t/dev/ttys001",
		"claude-hook-12345\t0\t0\t0\t%1\thook\tbash\t/tmp\t/dev/ttys002",
		"claude-test-9999\t0\t1\t0\t%2\thook2\tsh\t/tmp\t/dev/ttys003",
		"",
	}, "\n")

	tmx := &TmuxTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return []byte(sampleOutput), nil
		},
	}

	got, err := tmx.ListPanes()
	if err != nil {
		t.Fatalf("ListPanes returned error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 pane (hook sessions excluded), got %d", len(got))
	}
	if got[0].ID != "%0" {
		t.Errorf("expected pane %%0, got %s", got[0].ID)
	}
}

func TestTmuxListPanesHookSessionAttachedNotExcluded(t *testing.T) {
	// hook パターンでも attached ならば除外しない。
	sampleOutput := "claude-hook-12345\t1\t0\t0\t%0\thook\tbash\t/tmp\t/dev/ttys001\n"

	tmx := &TmuxTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return []byte(sampleOutput), nil
		},
	}

	got, err := tmx.ListPanes()
	if err != nil {
		t.Fatalf("ListPanes returned error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 pane (attached hook session), got %d", len(got))
	}
}

func TestTmuxListPanesNilExecFn(t *testing.T) {
	tmx := &TmuxTerminal{execFn: nil}
	_, err := tmx.ListPanes()
	if err == nil {
		t.Fatal("expected error for nil execFn")
	}
}

func TestTmuxListPanesNilReceiver(t *testing.T) {
	var tmx *TmuxTerminal
	_, err := tmx.ListPanes()
	if err == nil {
		t.Fatal("expected error for nil receiver")
	}
}

func TestTmuxListPanesExecError(t *testing.T) {
	tmx := &TmuxTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return nil, errors.New("tmux not running")
		},
	}
	_, err := tmx.ListPanes()
	if err == nil {
		t.Fatal("expected error from exec failure")
	}
}

func TestTmuxFocusPane(t *testing.T) {
	var calls []string

	sampleOutput := "main\t1\t0\t0\t%0\teditor\tclaude\t/home/user/project\t/dev/ttys001\n"

	tmx := &TmuxTerminal{
		execFn: func(args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(args, " "))
			if args[0] == "list-panes" {
				return []byte(sampleOutput), nil
			}
			return []byte(""), nil
		},
	}

	if err := tmx.FocusPane("%0"); err != nil {
		t.Fatalf("FocusPane returned error: %v", err)
	}

	// list-panes, switch-client, select-window, select-pane の4コマンドが呼ばれる。
	if len(calls) != 4 {
		t.Fatalf("expected 4 tmux commands, got %d: %v", len(calls), calls)
	}

	if !strings.Contains(calls[1], "switch-client") {
		t.Errorf("expected switch-client, got %q", calls[1])
	}
	if !strings.Contains(calls[2], "select-window") {
		t.Errorf("expected select-window, got %q", calls[2])
	}
	if !strings.Contains(calls[3], "select-pane") {
		t.Errorf("expected select-pane, got %q", calls[3])
	}
}

func TestTmuxFocusPaneNotFound(t *testing.T) {
	tmx := &TmuxTerminal{
		execFn: func(args ...string) ([]byte, error) {
			if args[0] == "list-panes" {
				return []byte("main\t1\t0\t0\t%0\teditor\tclaude\t/tmp\t/dev/ttys001\n"), nil
			}
			return []byte(""), nil
		},
	}

	err := tmx.FocusPane("%999")
	if err == nil {
		t.Fatal("expected error for non-existent pane")
	}
	if !errors.Is(err, ErrPaneNotFound) {
		t.Errorf("expected ErrPaneNotFound, got %v", err)
	}
}

func TestTmuxFocusPaneNilExecFn(t *testing.T) {
	tmx := &TmuxTerminal{execFn: nil}
	err := tmx.FocusPane("%0")
	if err == nil {
		t.Fatal("expected error for nil execFn")
	}
}

func TestTmuxGetPaneText(t *testing.T) {
	sampleText := "line1\nline2\nline3\nAllow tool call? (y/n)\n"

	tmx := &TmuxTerminal{
		execFn: func(args ...string) ([]byte, error) {
			if args[0] != "capture-pane" {
				t.Fatalf("expected capture-pane, got %v", args)
			}
			// -t, paneID, -p, -J を確認
			if len(args) < 5 || args[2] != "%0" {
				t.Fatalf("unexpected args: %v", args)
			}
			return []byte(sampleText), nil
		},
	}

	got, err := tmx.GetPaneText("%0")
	if err != nil {
		t.Fatalf("GetPaneText returned error: %v", err)
	}

	if !strings.Contains(got, "Allow tool call") {
		t.Errorf("expected approval prompt in output, got %q", got)
	}
}

func TestTmuxGetPaneTextNilExecFn(t *testing.T) {
	tmx := &TmuxTerminal{execFn: nil}
	_, err := tmx.GetPaneText("%0")
	if err == nil {
		t.Fatal("expected error for nil execFn")
	}
}

func TestTmuxName(t *testing.T) {
	tmx := NewTmuxTerminal()
	if got, want := tmx.Name(), "tmux"; got != want {
		t.Fatalf("unexpected terminal name: got=%q want=%q", got, want)
	}
}

func TestTmuxNewTerminalExecFn(t *testing.T) {
	tmx := NewTmuxTerminal()
	if tmx == nil {
		t.Fatal("NewTmuxTerminal returned nil")
	}
	if tmx.execFn == nil {
		t.Fatal("execFn should be set")
	}
}

func TestTmuxGetPaneText80LineLimit(t *testing.T) {
	// Arrange: generate 100 lines "line 1" ... "line 100"
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		sb.WriteString(fmt.Sprintf("line %d\n", i))
	}
	input := sb.String()

	tmx := &TmuxTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return []byte(input), nil
		},
	}

	// Act
	got, err := tmx.GetPaneText("%0")
	if err != nil {
		t.Fatalf("GetPaneText returned error: %v", err)
	}

	// strings.Split("line 1\n...line 100\n", "\n") produces 101 elements
	// (trailing empty string after the final newline), so:
	//   len(lines) = 101, start = 101 - 80 = 21 (0-indexed)
	//   lines[21] = "line 22"  ← first line in the 80-line window
	// Therefore "line 22" must be present and "line 21" must NOT appear.
	if !strings.Contains(got, "line 22") {
		t.Errorf("expected output to contain 'line 22' (first of last-80 window), got: %q", got[:min(200, len(got))])
	}
	// line 21 is trimmed off — it falls outside the 80-line window
	if strings.Contains(got, "line 21\n") {
		t.Errorf("expected 'line 21' to be excluded from 80-line window output")
	}
	// line 100 should be present
	if !strings.Contains(got, "line 100") {
		t.Errorf("expected output to contain 'line 100'")
	}
}

func TestTmuxFocusPaneSwitchClientError(t *testing.T) {
	sampleOutput := "main\t1\t0\t0\t%0\teditor\tclaude\t/home/user/project\t/dev/ttys001\n"

	tmx := &TmuxTerminal{
		execFn: func(args ...string) ([]byte, error) {
			switch args[0] {
			case "list-panes":
				return []byte(sampleOutput), nil
			case "switch-client":
				return nil, errors.New("no clients")
			default:
				return []byte(""), nil
			}
		},
	}

	err := tmx.FocusPane("%0")
	if err == nil {
		t.Fatal("expected error when switch-client fails")
	}
	if !strings.Contains(err.Error(), "switch-client") {
		t.Errorf("expected error to contain 'switch-client', got: %v", err)
	}
}

func TestTmuxFocusPaneSelectWindowError(t *testing.T) {
	sampleOutput := "main\t1\t0\t0\t%0\teditor\tclaude\t/home/user/project\t/dev/ttys001\n"

	tmx := &TmuxTerminal{
		execFn: func(args ...string) ([]byte, error) {
			switch args[0] {
			case "list-panes":
				return []byte(sampleOutput), nil
			case "switch-client":
				return []byte(""), nil
			case "select-window":
				return nil, errors.New("no such window")
			default:
				return []byte(""), nil
			}
		},
	}

	err := tmx.FocusPane("%0")
	if err == nil {
		t.Fatal("expected error when select-window fails")
	}
	if !strings.Contains(err.Error(), "select-window") {
		t.Errorf("expected error to contain 'select-window', got: %v", err)
	}
}

func TestTmuxListPanesShortLine(t *testing.T) {
	// A line with fewer than 9 tab-separated fields should be skipped.
	sampleOutput := strings.Join([]string{
		"main\t1\t0\t0\t%0\teditor\tclaude", // only 7 fields — should be skipped
		"main\t1\t0\t1\t%1\tlogs\tbash\t/home/user/project\t/dev/ttys002",
		"",
	}, "\n")

	tmx := &TmuxTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return []byte(sampleOutput), nil
		},
	}

	got, err := tmx.ListPanes()
	if err != nil {
		t.Fatalf("ListPanes returned error: %v", err)
	}

	// Only the valid line with 9 fields should be returned.
	if len(got) != 1 {
		t.Fatalf("expected 1 pane (short line skipped), got %d", len(got))
	}
	if got[0].ID != "%1" {
		t.Errorf("expected pane %%1, got %s", got[0].ID)
	}
}

func TestTmuxListPanesEmptyOutput(t *testing.T) {
	tmx := &TmuxTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return []byte(""), nil
		},
	}

	got, err := tmx.ListPanes()
	if err != nil {
		t.Fatalf("ListPanes returned unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d panes", len(got))
	}
}

func TestHookSessionPattern(t *testing.T) {
	tests := []struct {
		name  string
		input string
		match bool
	}{
		{"typical hook session", "claude-hook-12345", true},
		{"short digits", "claude-test-999", false},       // < 4 digits
		{"four digits", "claude-test-1234", true},
		{"normal session", "main", false},
		{"work session", "dev-server", false},
		{"claude prefix only", "claude", false},
		{"claude with dash", "claude-session", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hookSessionPattern.MatchString(tc.input)
			if got != tc.match {
				t.Errorf("hookSessionPattern.MatchString(%q) = %v, want %v", tc.input, got, tc.match)
			}
		})
	}
}


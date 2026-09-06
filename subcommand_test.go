package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yoshihiko555/baton/internal/config"
	"github.com/yoshihiko555/baton/internal/core"
	"github.com/yoshihiko555/baton/internal/terminal"
)

// --- fakes ---

type fakeSubTerminal struct {
	sendKeysCalls int
	lastPaneID    string
	lastKeys      []string
}

func (f *fakeSubTerminal) ListPanes() ([]terminal.Pane, error) { return nil, nil }
func (f *fakeSubTerminal) FocusPane(paneID string) error       { return nil }
func (f *fakeSubTerminal) GetPaneText(paneID string) (string, error) {
	return "", nil
}
func (f *fakeSubTerminal) IsAvailable() bool { return true }
func (f *fakeSubTerminal) SendKeys(paneID string, keys ...string) error {
	f.sendKeysCalls++
	f.lastPaneID = paneID
	f.lastKeys = keys
	return nil
}
func (f *fakeSubTerminal) Name() string { return "fake" }

type fakeSubScanner struct {
	err error
}

func (f *fakeSubScanner) Scan(ctx context.Context) core.ScanResult {
	return core.ScanResult{Err: f.err}
}

// fakeSubState implements both core.StateUpdater and core.StateReader so
// tests can directly control what Projects() returns without going through
// a real scan pipeline.
type fakeSubState struct {
	projects []core.Project
}

func (f *fakeSubState) UpdateFromScan(result core.ScanResult) error { return nil }
func (f *fakeSubState) ApplyHookStates()                            {}
func (f *fakeSubState) RefineToolUseState(term terminal.Terminal)   {}

func (f *fakeSubState) Projects() []core.Project    { return f.projects }
func (f *fakeSubState) GetProjects() []core.Project { return f.projects }
func (f *fakeSubState) Summary() core.Summary       { return core.Summary{} }
func (f *fakeSubState) Panes() []terminal.Pane      { return nil }

func newFakeDeps(projects []core.Project) (subcommandDeps, *fakeSubTerminal) {
	return newFakeDepsWithScanErr(projects, nil)
}

// newFakeDepsWithScanErr is like newFakeDeps but lets tests force
// core.ScanResult.Err, so they can exercise scanOnce's fail-fast path
// (see TestRunListWithScanError / TestRunApproveWithScanError).
func newFakeDepsWithScanErr(projects []core.Project, scanErr error) (subcommandDeps, *fakeSubTerminal) {
	term := &fakeSubTerminal{}
	state := &fakeSubState{projects: projects}
	return subcommandDeps{
		cfg:     config.Default(),
		term:    term,
		scanner: &fakeSubScanner{err: scanErr},
		updater: state,
		reader:  state,
	}, term
}

func waitingSession(pane string) *core.Session {
	return &core.Session{
		State:      core.Waiting,
		Tool:       core.ToolClaude,
		PaneID:     pane,
		WorkingDir: "/home/user/proj",
	}
}

func idleSession(pane string) *core.Session {
	return &core.Session{
		State:      core.Idle,
		Tool:       core.ToolClaude,
		PaneID:     pane,
		WorkingDir: "/home/user/proj",
	}
}

// --- argument parsing errors (exit 2) ---

func TestRunApproveOrDenyArgErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"too many args", []string{"%5", "%6"}},
		{"unknown flag", []string{"--bogus", "%5"}},
		{"empty pane", []string{""}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := runSubcommand("approve", tc.args, &out, &errOut)
			if code != exitUsageError {
				t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitUsageError, errOut.String())
			}
		})
	}
}

// TestRunApproveOrDenyHelp verifies that -h/--help exits 0 (not exitUsageError)
// and does not print the usage text twice: flag.Parse itself already prints
// the error/usage via fs.Usage() when it returns flag.ErrHelp, so
// runApproveOrDeny must not print anything a second time.
func TestRunApproveOrDenyHelp(t *testing.T) {
	for _, name := range []string{"approve", "deny"} {
		t.Run(name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := runSubcommand(name, []string{"-h"}, &out, &errOut)
			if code != exitOK {
				t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitOK, errOut.String())
			}
			if got := strings.Count(errOut.String(), "usage:"); got != 1 {
				t.Errorf("usage text printed %d times, want exactly 1 (stderr: %q)", got, errOut.String())
			}
		})
	}
}

func TestRunListHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runSubcommand("list", []string{"-h"}, &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitOK, errOut.String())
	}
}

func TestParsePaneAndFlagsOrderIndependence(t *testing.T) {
	// parsePaneAndFlags only needs a --config *value* to route through
	// fs.String/fs.Parse; it never opens or reads the path, so an arbitrary
	// placeholder is enough here (no config.yaml fixture needed).
	const cfgPath = "/nonexistent/config.yaml"

	fs := flag.NewFlagSet("approve", flag.ContinueOnError)
	configFlag := fs.String("config", "", "path to config file")
	pane, err := parsePaneAndFlags(fs, []string{"%5", "--config", cfgPath})
	if err != nil {
		t.Fatalf("parsePaneAndFlags() error = %v", err)
	}
	if pane != "%5" {
		t.Errorf("pane = %q, want %%5", pane)
	}
	if *configFlag != cfgPath {
		t.Errorf("--config = %q, want %q", *configFlag, cfgPath)
	}

	fs2 := flag.NewFlagSet("approve", flag.ContinueOnError)
	configFlag2 := fs2.String("config", "", "path to config file")
	pane2, err := parsePaneAndFlags(fs2, []string{"--config", cfgPath, "%5"})
	if err != nil {
		t.Fatalf("parsePaneAndFlags() error = %v", err)
	}
	if pane2 != "%5" {
		t.Errorf("pane = %q, want %%5", pane2)
	}
	if *configFlag2 != cfgPath {
		t.Errorf("--config = %q, want %q", *configFlag2, cfgPath)
	}
}

// --- approve/deny gate behavior ---

func TestRunApproveWithPaneNotFound(t *testing.T) {
	deps, term := newFakeDeps(nil)
	var out, errOut bytes.Buffer

	code := runApproveWith(deps, "%99", core.ApprovalApprove, &out, &errOut)
	if code != exitPaneNotFound {
		t.Fatalf("exit code = %d, want %d", code, exitPaneNotFound)
	}
	if !strings.Contains(errOut.String(), "%99") {
		t.Errorf("stderr = %q, want mention of pane id", errOut.String())
	}
	if term.sendKeysCalls != 0 {
		t.Errorf("SendKeys called %d times, want 0", term.sendKeysCalls)
	}
}

// TestRunApproveOrDenyWithNonWaitingSession covers both approve and deny
// against a non-Waiting session: neither action should ever reach SendKeys.
func TestRunApproveOrDenyWithNonWaitingSession(t *testing.T) {
	cases := []struct {
		name   string
		action core.ApprovalAction
	}{
		{"approve", core.ApprovalApprove},
		{"deny", core.ApprovalDeny},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projects := []core.Project{
				{Path: "/home/user/proj", Sessions: []*core.Session{idleSession("%5")}},
			}
			deps, term := newFakeDeps(projects)
			var out, errOut bytes.Buffer

			code := runApproveWith(deps, "%5", tc.action, &out, &errOut)
			if code != exitNotApprovable {
				t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitNotApprovable, errOut.String())
			}
			if term.sendKeysCalls != 0 {
				t.Errorf("SendKeys called %d times, want 0", term.sendKeysCalls)
			}
		})
	}
}

// TestRunListWithScanError and TestRunApproveWithScanError cover item 1:
// a scan failure (core.ScanResult.Err != nil) must fail the subcommand with
// exitInternalError instead of silently proceeding with an empty/stale scan.

func TestRunListWithScanError(t *testing.T) {
	deps, _ := newFakeDepsWithScanErr(nil, errors.New("boom"))
	var out, errOut bytes.Buffer

	code := runListWith(deps, false, "json", &out, &errOut)
	if code != exitInternalError {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitInternalError, errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty on scan failure", out.String())
	}
	if !strings.Contains(errOut.String(), "boom") {
		t.Errorf("stderr = %q, want mention of scan error", errOut.String())
	}
}

func TestRunApproveWithScanError(t *testing.T) {
	deps, term := newFakeDepsWithScanErr(nil, errors.New("boom"))
	var out, errOut bytes.Buffer

	code := runApproveWith(deps, "%5", core.ApprovalApprove, &out, &errOut)
	// Scan failure must be reported as an internal error (1), not misread as
	// "pane not found" (3): the scan never happened, so we cannot know
	// whether the pane exists.
	if code != exitInternalError {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitInternalError, errOut.String())
	}
	if term.sendKeysCalls != 0 {
		t.Errorf("SendKeys called %d times, want 0", term.sendKeysCalls)
	}
}

// TestFindSessionByPaneDuplicates covers item 6: multiple sessions sharing
// the same PaneID must not silently pick an arbitrary one.
func TestFindSessionByPaneDuplicates(t *testing.T) {
	t.Run("exactly one waiting among duplicates is unambiguous", func(t *testing.T) {
		projects := []core.Project{
			{Path: "/home/user/proj-a", Sessions: []*core.Session{idleSession("%5")}},
			{Path: "/home/user/proj-b", Sessions: []*core.Session{waitingSession("%5")}},
		}
		session, err := findSessionByPane(projects, "%5")
		if err != nil {
			t.Fatalf("findSessionByPane() error = %v, want nil", err)
		}
		if session == nil || session.State != core.Waiting {
			t.Fatalf("findSessionByPane() = %+v, want the waiting duplicate", session)
		}
	})

	t.Run("zero waiting among duplicates falls back to not-approvable", func(t *testing.T) {
		projects := []core.Project{
			{Path: "/home/user/proj-a", Sessions: []*core.Session{idleSession("%5")}},
			{Path: "/home/user/proj-b", Sessions: []*core.Session{idleSession("%5")}},
		}
		session, err := findSessionByPane(projects, "%5")
		if err != nil {
			t.Fatalf("findSessionByPane() error = %v, want nil", err)
		}
		if session == nil {
			t.Fatalf("findSessionByPane() = nil, want a representative duplicate for the not-approvable error path")
		}
	})

	t.Run("two or more waiting duplicates is a genuine ambiguity", func(t *testing.T) {
		projects := []core.Project{
			{Path: "/home/user/proj-a", Sessions: []*core.Session{waitingSession("%5")}},
			{Path: "/home/user/proj-b", Sessions: []*core.Session{waitingSession("%5")}},
		}
		session, err := findSessionByPane(projects, "%5")
		if err == nil {
			t.Fatalf("findSessionByPane() error = nil, want errAmbiguousPane")
		}
		if !errors.Is(err, errAmbiguousPane) {
			t.Errorf("findSessionByPane() error = %v, want wrapping errAmbiguousPane", err)
		}
		if session != nil {
			t.Errorf("findSessionByPane() session = %+v, want nil", session)
		}
	})
}

// TestRunApproveWithAmbiguousDuplicatePane covers the CLI-level effect of the
// two-or-more-waiting-duplicates case: exit 4 with a message identifying the
// pane, and no SendKeys call (we must not guess which session to signal).
func TestRunApproveWithAmbiguousDuplicatePane(t *testing.T) {
	projects := []core.Project{
		{Path: "/home/user/proj-a", Sessions: []*core.Session{waitingSession("%5")}},
		{Path: "/home/user/proj-b", Sessions: []*core.Session{waitingSession("%5")}},
	}
	deps, term := newFakeDeps(projects)
	var out, errOut bytes.Buffer

	code := runApproveWith(deps, "%5", core.ApprovalApprove, &out, &errOut)
	if code != exitNotApprovable {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitNotApprovable, errOut.String())
	}
	if !strings.Contains(errOut.String(), "%5") {
		t.Errorf("stderr = %q, want mention of pane id", errOut.String())
	}
	if term.sendKeysCalls != 0 {
		t.Errorf("SendKeys called %d times, want 0", term.sendKeysCalls)
	}
}

func TestRunApproveWithWaitingSessionSendsEnter(t *testing.T) {
	projects := []core.Project{
		{Path: "/home/user/proj", Sessions: []*core.Session{waitingSession("%5")}},
	}
	deps, term := newFakeDeps(projects)
	var out, errOut bytes.Buffer

	code := runApproveWith(deps, "%5", core.ApprovalApprove, &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitOK, errOut.String())
	}
	if term.sendKeysCalls != 1 || len(term.lastKeys) != 1 || term.lastKeys[0] != "Enter" {
		t.Errorf("SendKeys calls = %d keys = %v, want 1 call with [Enter]", term.sendKeysCalls, term.lastKeys)
	}
	if !strings.Contains(out.String(), "approved") || !strings.Contains(out.String(), "%5") {
		t.Errorf("stdout = %q, want mention of approved and pane id", out.String())
	}
}

func TestRunApproveWithWaitingSessionSendsEscapeOnDeny(t *testing.T) {
	projects := []core.Project{
		{Path: "/home/user/proj", Sessions: []*core.Session{waitingSession("%5")}},
	}
	deps, term := newFakeDeps(projects)
	var out, errOut bytes.Buffer

	code := runApproveWith(deps, "%5", core.ApprovalDeny, &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitOK, errOut.String())
	}
	if term.sendKeysCalls != 1 || len(term.lastKeys) != 1 || term.lastKeys[0] != "Escape" {
		t.Errorf("SendKeys calls = %d keys = %v, want 1 call with [Escape]", term.sendKeysCalls, term.lastKeys)
	}
	if !strings.Contains(out.String(), "denied") || !strings.Contains(out.String(), "%5") {
		t.Errorf("stdout = %q, want mention of denied and pane id", out.String())
	}
}

// --- list ---

func TestRunListWithJSONFormat(t *testing.T) {
	projects := []core.Project{
		{
			Path: "/home/user/proj",
			Name: "proj",
			Sessions: []*core.Session{
				waitingSession("%5"),
				idleSession("%6"),
			},
		},
	}
	deps, _ := newFakeDeps(projects)
	var out, errOut bytes.Buffer

	code := runListWith(deps, false, "json", &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitOK, errOut.String())
	}

	var status core.StatusOutput
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("json.Unmarshal() error = %v (out: %s)", err, out.String())
	}
	total := 0
	for _, p := range status.Projects {
		total += len(p.Sessions)
	}
	if total != 2 {
		t.Errorf("total sessions = %d, want 2", total)
	}
}

func TestRunListWithJSONFormatWaitingOnly(t *testing.T) {
	projects := []core.Project{
		{
			Path: "/home/user/proj-a",
			Name: "proj-a",
			Sessions: []*core.Session{
				waitingSession("%5"),
				idleSession("%6"),
			},
		},
		{
			Path:     "/home/user/proj-b",
			Name:     "proj-b",
			Sessions: []*core.Session{idleSession("%7")},
		},
	}
	deps, _ := newFakeDeps(projects)
	var out, errOut bytes.Buffer

	code := runListWith(deps, true, "json", &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitOK, errOut.String())
	}

	var status core.StatusOutput
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("json.Unmarshal() error = %v (out: %s)", err, out.String())
	}
	if len(status.Projects) != 1 {
		t.Fatalf("projects = %d, want 1 (only proj-a should remain)", len(status.Projects))
	}
	if len(status.Projects[0].Sessions) != 1 || status.Projects[0].Sessions[0].State != "waiting" {
		t.Errorf("sessions = %+v, want exactly 1 waiting session", status.Projects[0].Sessions)
	}
}

func TestRunListWithTableFormat(t *testing.T) {
	projects := []core.Project{
		{
			Path:     "/home/user/proj",
			Name:     "proj",
			Sessions: []*core.Session{waitingSession("%5")},
		},
	}
	deps, _ := newFakeDeps(projects)
	var out, errOut bytes.Buffer

	code := runListWith(deps, false, "table", &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitOK, errOut.String())
	}
	if !strings.Contains(out.String(), "PANE") {
		t.Errorf("stdout missing header: %q", out.String())
	}
	if !strings.Contains(out.String(), "%5") || !strings.Contains(out.String(), "claude") {
		t.Errorf("stdout = %q, want pane id and tool name", out.String())
	}
}

// --- no status file write guarantee ---

func TestSubcommandsNeverWriteStatusFile(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "status.json")
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "terminal: tmux\nstatus_output_path: " + statusPath + "\nhook:\n  enabled: false\n"
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var setupErrOut bytes.Buffer
	deps, err := newSubcommandDeps(cfgPath, &setupErrOut)
	if err != nil {
		t.Fatalf("newSubcommandDeps() error = %v", err)
	}

	var out, errOut bytes.Buffer
	// Use the real scanner/state pipeline against whatever the machine's
	// actual tmux/process state happens to be; we only assert the status
	// file is never created, regardless of scan contents.
	_ = runListWith(deps, false, "json", &out, &errOut)

	if _, err := os.Stat(statusPath); !os.IsNotExist(err) {
		t.Fatalf("status file %q exists after baton list (err=%v), want it to remain absent", statusPath, err)
	}
}

// TestRunListWithJSONFormatWaitingSummary covers item 2: --waiting must
// recompute Summary from the filtered []core.Project (via
// core.BuildStatusOutputFromProjects), not by re-deriving counts from
// string-compared DTO state.
func TestRunListWithJSONFormatWaitingSummary(t *testing.T) {
	projects := []core.Project{
		{
			Path: "/home/user/proj-a",
			Name: "proj-a",
			Sessions: []*core.Session{
				waitingSession("%5"),
				idleSession("%6"),
			},
		},
		{
			Path:     "/home/user/proj-b",
			Name:     "proj-b",
			Sessions: []*core.Session{idleSession("%7")},
		},
	}
	deps, _ := newFakeDeps(projects)
	var out, errOut bytes.Buffer

	code := runListWith(deps, true, "json", &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitOK, errOut.String())
	}

	var status core.StatusOutput
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("json.Unmarshal() error = %v (out: %s)", err, out.String())
	}
	if status.Summary.TotalSessions != 1 {
		t.Errorf("Summary.TotalSessions = %d, want 1", status.Summary.TotalSessions)
	}
	if status.Summary.Active != 1 {
		t.Errorf("Summary.Active = %d, want 1", status.Summary.Active)
	}
	if status.Summary.Waiting != 1 {
		t.Errorf("Summary.Waiting = %d, want 1", status.Summary.Waiting)
	}
	if status.Summary.ByTool["claude"] != 1 {
		t.Errorf("Summary.ByTool[claude] = %d, want 1 (got %+v)", status.Summary.ByTool["claude"], status.Summary.ByTool)
	}
}

// TestRunListWithJSONFormatUsesStatusbarFormat covers item 3:
// formatted_status must reflect cfg.Statusbar.Format, not always the default
// "{{.Active}}/{{.TotalSessions}}" template.
func TestRunListWithJSONFormatUsesStatusbarFormat(t *testing.T) {
	projects := []core.Project{
		{
			Path:     "/home/user/proj",
			Name:     "proj",
			Sessions: []*core.Session{waitingSession("%5"), idleSession("%6")},
		},
	}
	deps, _ := newFakeDeps(projects)
	deps.cfg.Statusbar.Format = "waiting={{.Waiting}} total={{.TotalSessions}}"
	var out, errOut bytes.Buffer

	code := runListWith(deps, false, "json", &out, &errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitOK, errOut.String())
	}

	var status core.StatusOutput
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatalf("json.Unmarshal() error = %v (out: %s)", err, out.String())
	}
	if want := "waiting=1 total=2"; status.FormattedStatus != want {
		t.Errorf("FormattedStatus = %q, want %q", status.FormattedStatus, want)
	}
}

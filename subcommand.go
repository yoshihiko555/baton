package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/yoshihiko555/baton/internal/config"
	"github.com/yoshihiko555/baton/internal/core"
	"github.com/yoshihiko555/baton/internal/terminal"
)

// Exit codes for the list/approve/deny subcommands.
const (
	exitOK            = 0
	exitInternalError = 1
	exitUsageError    = 2
	exitPaneNotFound  = 3
	exitNotApprovable = 4
)

// subcommandScanTimeout bounds how long a single list/approve/deny scan may
// run, so a hung terminal/process call fails the CLI invocation instead of
// hanging it indefinitely.
const subcommandScanTimeout = 15 * time.Second

// errMissingPaneArg is returned by parsePaneAndFlags when no positional
// <pane> argument was supplied. It has not yet been printed anywhere, so
// callers must print it themselves before returning exitUsageError.
var errMissingPaneArg = errors.New("missing pane argument")

// unexpectedArgError is returned by parsePaneAndFlags when extra positional
// arguments remain after <pane>. Like errMissingPaneArg, it has not yet been
// printed, so callers must print it themselves.
type unexpectedArgError struct {
	arg string
}

func (e *unexpectedArgError) Error() string {
	return fmt.Sprintf("unexpected argument %q", e.arg)
}

// errAmbiguousPane is returned by findSessionByPane when more than one
// session sharing the same PaneID is in the Waiting state, making it unsafe
// to guess which one the caller intended to approve/deny.
var errAmbiguousPane = errors.New("multiple sessions share pane")

// subcommandDeps groups the dependencies used by the list/approve/deny
// subcommands. It mirrors the components run() wires up for a one-shot scan
// (like --once/--exit), but is never allowed to start a hook server listener
// or write the shared status JSON file.
type subcommandDeps struct {
	cfg     config.Config
	term    terminal.Terminal
	scanner core.Scanner
	updater core.StateUpdater
	reader  core.StateReader
}

// newSubcommandDeps loads config and builds the scan pipeline the same way
// run() does for its one-shot paths (--once / --exit): a hook status overlay
// is attached when hooks are enabled, but no hook socket is ever opened and
// the status file is never written from here.
func newSubcommandDeps(configPath string, errOut io.Writer) (subcommandDeps, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return subcommandDeps{}, fmt.Errorf("load config: %w", err)
	}
	core.SetDebugLogging(cfg.LogLevel == "debug")

	term, err := initTerminal(cfg.Terminal)
	if err != nil {
		return subcommandDeps{}, fmt.Errorf("init terminal: %w", err)
	}
	// term.IsAvailable() の false は致命的エラーにしない（run() と同様、警告のみ）。
	if !term.IsAvailable() {
		fmt.Fprintf(errOut, "warning: terminal %q is not available\n", term.Name())
	}

	processScanner := core.NewProcessScanner()
	scanner := core.NewDefaultScanner(term, processScanner)
	reader := core.NewIncrementalReader()
	resolver := core.NewStateResolver(reader, cfg.ClaudeProjectsDir, cfg.SessionMetaDir, cfg.ScanInterval)
	stateManager := core.NewStateManager(resolver)
	stateManager.SetProcessScanner(processScanner)

	if cfg.Hook.Enabled {
		stateManager.SetHookStatusOverlay(cfg.StatusOutputPath, cfg.Hook.StatusMaxAge)
	}

	return subcommandDeps{
		cfg:     cfg,
		term:    term,
		scanner: scanner,
		updater: stateManager,
		reader:  stateManager,
	}, nil
}

// scanOnce runs a single scan → update → refine cycle shared by all
// subcommands. It never writes the status JSON file.
//
// Unlike the TUI/headless scan loop (which treats a transient ScanResult.Err
// as "keep the previous snapshot" inside UpdateFromScan), a one-shot CLI
// invocation has no previous snapshot to fall back to: silently continuing
// with an empty scan would make `list` print nothing and `approve`/`deny`
// report a misleading "pane not found". So scanOnce fails fast on
// result.Err instead of calling UpdateFromScan at all.
func scanOnce(ctx context.Context, deps subcommandDeps) error {
	result := deps.scanner.Scan(ctx)
	if result.Err != nil {
		return result.Err
	}
	if err := deps.updater.UpdateFromScan(result); err != nil {
		return err
	}
	deps.updater.ApplyHookStates()
	deps.updater.RefineToolUseState(deps.term)
	return nil
}

// runSubcommand dispatches to the list/approve/deny subcommand implementations.
func runSubcommand(name string, args []string, out, errOut io.Writer) int {
	switch name {
	case "list":
		return runList(args, out, errOut)
	case "approve":
		return runApproveOrDeny("approve", core.ApprovalApprove, args, out, errOut)
	case "deny":
		return runApproveOrDeny("deny", core.ApprovalDeny, args, out, errOut)
	default:
		fmt.Fprintf(errOut, "baton: unknown subcommand %q\n", name)
		return exitUsageError
	}
}

// --- list ---

func runList(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(errOut)
	configPath := fs.String("config", "", "path to config file")
	waiting := fs.Bool("waiting", false, "show only sessions waiting for approval")
	format := fs.String("format", "table", "output format: table or json")
	fs.Usage = func() {
		fmt.Fprintln(errOut, "usage: baton list [--waiting] [--format table|json] [--config <path>]")
	}
	if err := fs.Parse(args); err != nil {
		// flag.Parse already printed the error (or, for -h/--help, the usage
		// text) via fs.Usage(); do not print anything a second time here.
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsageError
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(errOut, "baton list: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		return exitUsageError
	}

	normalizedFormat := strings.ToLower(strings.TrimSpace(*format))
	if normalizedFormat != "table" && normalizedFormat != "json" {
		fmt.Fprintf(errOut, "baton list: unsupported --format %q: expected table or json\n", *format)
		return exitUsageError
	}

	deps, err := newSubcommandDeps(*configPath, errOut)
	if err != nil {
		fmt.Fprintf(errOut, "baton list: %v\n", err)
		return exitInternalError
	}
	return runListWith(deps, *waiting, normalizedFormat, out, errOut)
}

// runListWith performs the scan and renders the result. It is separated from
// runList so tests can inject fake subcommandDeps.
func runListWith(deps subcommandDeps, waitingOnly bool, format string, out, errOut io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), subcommandScanTimeout)
	defer cancel()
	if err := scanOnce(ctx, deps); err != nil {
		fmt.Fprintf(errOut, "baton list: scan failed: %v\n", err)
		return exitInternalError
	}

	// Filter at the domain ([]core.Project) level before either renderer runs,
	// so table and JSON output are guaranteed to agree on which sessions are
	// included and Summary is computed by core.calcSummary (via
	// BuildStatusOutputFromProjects) rather than re-derived from string state
	// comparisons on the DTO layer.
	projects := deps.reader.Projects()
	if waitingOnly {
		projects = core.FilterWaitingSessions(projects)
	}

	switch format {
	case "json":
		return renderListJSON(deps.cfg, projects, out, errOut)
	default:
		return renderListTable(projects, out, errOut)
	}
}

func renderListJSON(cfg config.Config, projects []core.Project, out, errOut io.Writer) int {
	status := core.BuildStatusOutputFromProjects(projects, false)
	status.FormattedStatus = core.FormatStatus(cfg.Statusbar.Format, status.Summary)

	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		fmt.Fprintf(errOut, "baton list: encode status: %v\n", err)
		return exitInternalError
	}
	fmt.Fprintln(out, string(data))
	return exitOK
}

func renderListTable(projects []core.Project, out, errOut io.Writer) int {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PANE\tSTATE\tTOOL\tVIA\tDIR")
	for _, p := range projects {
		for _, s := range p.Sessions {
			if s == nil {
				continue
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				paneColumn(*s), s.State.String(), s.Tool.String(), viaColumn(s.Via), shortenHome(s.WorkingDir))
		}
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintf(errOut, "baton list: render table: %v\n", err)
		return exitInternalError
	}
	return exitOK
}

func paneColumn(s core.Session) string {
	if s.PaneID != "" && !s.Ambiguous {
		return s.PaneID
	}
	if len(s.CandidatePaneIDs) > 0 {
		return "?(" + strings.Join(s.CandidatePaneIDs, ",") + ")"
	}
	return "?"
}

func viaColumn(via string) string {
	if via == "" {
		return "-"
	}
	return via
}

// shortenHome replaces the user's home directory prefix with "~".
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
}

// --- approve / deny ---

// parsePaneAndFlags parses a subcommand's FlagSet against args where a single
// positional <pane> argument may appear either before or after the flags
// (e.g. both "approve %5 --config x" and "approve --config x %5" must work,
// since Go's flag.Parse stops at the first non-flag argument).
func parsePaneAndFlags(fs *flag.FlagSet, args []string) (string, error) {
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if fs.NArg() < 1 {
		return "", errMissingPaneArg
	}
	pane := fs.Arg(0)
	if err := fs.Parse(fs.Args()[1:]); err != nil {
		return "", err
	}
	if fs.NArg() != 0 {
		return "", &unexpectedArgError{arg: fs.Arg(0)}
	}
	return pane, nil
}

// isUnprintedParseError reports whether err is one of parsePaneAndFlags' own
// sentinel errors (errMissingPaneArg / *unexpectedArgError), which have not
// been printed anywhere yet. Any other error returned by fs.Parse has
// already been printed to errOut by the flag package itself (it respects
// fs.Usage, which is always set before parsePaneAndFlags runs).
func isUnprintedParseError(err error) bool {
	if errors.Is(err, errMissingPaneArg) {
		return true
	}
	var uae *unexpectedArgError
	return errors.As(err, &uae)
}

func runApproveOrDeny(name string, action core.ApprovalAction, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(errOut)
	configPath := fs.String("config", "", "path to config file")
	fs.Usage = func() {
		fmt.Fprintf(errOut, "usage: baton %s <pane> [--config <path>]\n", name)
	}

	pane, err := parsePaneAndFlags(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// flag.Parse already printed the usage text via fs.Usage().
			return exitOK
		}
		if isUnprintedParseError(err) {
			fmt.Fprintf(errOut, "baton %s: %v\n", name, err)
			fs.Usage()
		}
		// Otherwise flag.Parse already printed its own error + usage.
		return exitUsageError
	}
	if strings.TrimSpace(pane) == "" {
		fmt.Fprintf(errOut, "baton %s: pane argument must not be empty\n", name)
		fs.Usage()
		return exitUsageError
	}

	deps, err := newSubcommandDeps(*configPath, errOut)
	if err != nil {
		fmt.Fprintf(errOut, "baton %s: %v\n", name, err)
		return exitInternalError
	}
	return runApproveWith(deps, pane, action, out, errOut)
}

// runApproveWith scans once, locates the session bound to pane, and sends the
// action's key to it. It is used for both "approve" and "deny" (the action
// parameter selects the key), and is separated from runApproveOrDeny so tests
// can inject fake subcommandDeps.
func runApproveWith(deps subcommandDeps, pane string, action core.ApprovalAction, out, errOut io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), subcommandScanTimeout)
	defer cancel()
	if err := scanOnce(ctx, deps); err != nil {
		fmt.Fprintf(errOut, "baton %s: scan failed: %v\n", action, err)
		return exitInternalError
	}

	session, err := findSessionByPane(deps.reader.Projects(), pane)
	if err != nil {
		fmt.Fprintf(errOut, "baton %s: %v\n", action, err)
		return exitNotApprovable
	}
	if session == nil {
		fmt.Fprintf(errOut, "baton %s: pane %q not found in current scan\n", action, pane)
		return exitPaneNotFound
	}

	if err := core.SendApproval(deps.term, *session, action); err != nil {
		if errors.Is(err, core.ErrNotApprovable) {
			fmt.Fprintf(errOut, "baton %s: pane %s is not approvable: state=%s tool=%s ambiguous=%v\n",
				action, pane, session.State, session.Tool, session.Ambiguous)
			return exitNotApprovable
		}
		fmt.Fprintf(errOut, "baton %s: send keys failed: %v\n", action, err)
		return exitInternalError
	}

	verb := "approved"
	if action == core.ApprovalDeny {
		verb = "denied"
	}
	fmt.Fprintf(out, "%s %s (%s, %s)\n", verb, pane, session.Tool.String(), shortenHome(session.WorkingDir))
	return exitOK
}

// findSessionByPane locates the session bound to pane. If more than one
// session shares the same PaneID (which normally should not happen, but
// scanner/state edge cases could produce it), it disambiguates by Waiting
// state: exactly one Waiting session among the duplicates is unambiguous and
// is returned; zero Waiting sessions falls back to the first duplicate (so
// the caller's CanRespondToApproval/SendApproval gate reports the ordinary
// "not approvable" error); two or more Waiting sessions is a genuine
// ambiguity that is reported as an error rather than guessed at.
func findSessionByPane(projects []core.Project, pane string) (*core.Session, error) {
	var matches []*core.Session
	for _, p := range projects {
		for _, s := range p.Sessions {
			if s != nil && s.PaneID == pane {
				matches = append(matches, s)
			}
		}
	}

	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return matches[0], nil
	}

	var waiting []*core.Session
	for _, m := range matches {
		if m.State == core.Waiting {
			waiting = append(waiting, m)
		}
	}
	switch len(waiting) {
	case 0:
		return matches[0], nil
	case 1:
		return waiting[0], nil
	default:
		return nil, fmt.Errorf("%w %q", errAmbiguousPane, pane)
	}
}

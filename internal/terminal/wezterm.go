package terminal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
)

// WezTerminal implements Terminal for wezterm.
type WezTerminal struct {
	execFn func(args ...string) ([]byte, error)
}

// NewWezTerminal creates a wezterm-backed terminal implementation.
func NewWezTerminal() *WezTerminal {
	return &WezTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return exec.Command("wezterm", args...).Output()
		},
	}
}

// ListPanes lists wezterm panes via wezterm cli.
func (w *WezTerminal) ListPanes() ([]Pane, error) {
	if w == nil || w.execFn == nil {
		return nil, fmt.Errorf("wezterm exec function is not configured")
	}

	out, err := w.execFn("cli", "list", "--format", "json")
	if err != nil {
		return nil, mapWeztermExecError(err)
	}

	var rawPanes []struct {
		ID         json.RawMessage `json:"pane_id"`
		Title      string          `json:"title"`
		TabID      json.RawMessage `json:"tab_id"`
		WorkingDir string          `json:"cwd"`
	}
	if err := json.Unmarshal(out, &rawPanes); err != nil {
		return nil, err
	}

	panes := make([]Pane, 0, len(rawPanes))
	for _, rawPane := range rawPanes {
		paneID, err := jsonValueToString(rawPane.ID)
		if err != nil {
			return nil, fmt.Errorf("invalid pane_id: %w", err)
		}

		tabID, err := jsonValueToString(rawPane.TabID)
		if err != nil {
			return nil, fmt.Errorf("invalid tab_id: %w", err)
		}

		panes = append(panes, Pane{
			ID:         paneID,
			Title:      rawPane.Title,
			TabID:      tabID,
			WorkingDir: rawPane.WorkingDir,
		})
	}

	return panes, nil
}

// FocusPane activates the given pane in wezterm.
func (w *WezTerminal) FocusPane(paneID string) error {
	if w == nil || w.execFn == nil {
		return fmt.Errorf("wezterm exec function is not configured")
	}

	_, err := w.execFn("cli", "activate-pane", "--pane-id", paneID)
	if err != nil {
		return mapWeztermExecError(err)
	}

	return nil
}

// IsAvailable reports whether wezterm exists on PATH.
func (w *WezTerminal) IsAvailable() bool {
	_, err := exec.LookPath("wezterm")
	return err == nil
}

// Name returns terminal identifier.
func (w *WezTerminal) Name() string {
	return "wezterm"
}

func mapWeztermExecError(err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrTerminalNotFound, err)
	}

	return err
}

func jsonValueToString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("empty value")
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}

	var i int64
	if err := json.Unmarshal(raw, &i); err == nil {
		return strconv.FormatInt(i, 10), nil
	}

	return "", fmt.Errorf("unsupported value: %s", string(raw))
}

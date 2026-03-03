package terminal

import "errors"

// Terminal defines the interface for terminal integrations.
type Terminal interface {
	// ListPanes returns all available panes in the terminal.
	ListPanes() ([]Pane, error)
	// FocusPane focuses the pane identified by paneID.
	FocusPane(paneID string) error
	// IsAvailable reports whether the terminal is accessible.
	IsAvailable() bool
	// Name returns the terminal identifier (e.g., "wezterm").
	Name() string
}

// Pane represents a terminal pane or tab.
type Pane struct {
	ID         string `json:"pane_id"`
	Title      string `json:"title"`
	TabID      string `json:"tab_id"`
	WorkingDir string `json:"cwd"`
}

// Sentinel errors returned by Terminal implementations.
var (
	ErrTerminalNotFound = errors.New("terminal not found")
	ErrPaneNotFound     = errors.New("pane not found")
)

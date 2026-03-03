package core

import (
	"encoding/json"
	"time"
)

// SessionState represents the current state of a session.
type SessionState int

const (
	// Idle means the session is not doing any work.
	Idle SessionState = iota
	// Thinking means the session is currently reasoning.
	Thinking
	// ToolUse means the session is currently using a tool.
	ToolUse
	// Error means the session is in an error state.
	Error
)

// String returns the string representation of the session state.
func (s SessionState) String() string {
	switch s {
	case Idle:
		return "idle"
	case Thinking:
		return "thinking"
	case ToolUse:
		return "tool_use"
	case Error:
		return "error"
	default:
		return "unknown"
	}
}

// MarshalJSON marshals the session state as a quoted string.
func (s SessionState) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// Entry represents a raw session entry from the stream.
type Entry struct {
	Type      string          `json:"type"`
	Role      string          `json:"role,omitempty"`
	CreatedAt time.Time       `json:"created_at,omitempty"`
	Raw       json.RawMessage `json:"-"`
}

// Session represents an AI monitor session.
type Session struct {
	ID           string       `json:"id"`
	ProjectPath  string       `json:"project_path"`
	State        SessionState `json:"state"`
	LastActivity time.Time    `json:"last_activity"`
	PaneID       string       `json:"pane_id,omitempty"`
	FilePath     string       `json:"-"`
}

// Project represents sessions grouped by project.
type Project struct {
	Path        string     `json:"path"`
	DisplayName string     `json:"display_name"`
	Sessions    []*Session `json:"sessions"`
	ActiveCount int        `json:"active_count"`
}

// StatusOutput represents the status response payload.
type StatusOutput struct {
	Projects  []Project `json:"projects"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WatchEventType represents the type of file watch event.
type WatchEventType int

const (
	// Created indicates a new session file was created.
	Created WatchEventType = iota
	// Modified indicates a session file was modified.
	Modified
	// Removed indicates a session file was removed.
	Removed
)

// String returns the string representation of a watch event type.
func (t WatchEventType) String() string {
	switch t {
	case Created:
		return "created"
	case Modified:
		return "modified"
	case Removed:
		return "removed"
	default:
		return "unknown"
	}
}

// WatchEvent represents a session file watch event.
type WatchEvent struct {
	Type        WatchEventType
	Path        string
	ProjectPath string
	SessionID   string
}

// StateReader provides read-only access to aggregated state.
type StateReader interface {
	GetProjects() []Project
	GetStatus() StatusOutput
}

// StateWriter provides write access to aggregated state.
type StateWriter interface {
	HandleEvent(event WatchEvent) error
}

// EventSource provides a read-only channel of watch events.
type EventSource interface {
	Events() <-chan WatchEvent
}

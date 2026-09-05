package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

const maxFieldLength = 4096

var paneIDPattern = regexp.MustCompile(`^%[0-9]+$`)

// Event は Claude Code hook から受信するイベントを表す。
type Event struct {
	PaneID         string `json:"pane_id"`
	HookEventName  string `json:"hook_event_name"`
	SessionID      string `json:"session_id,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	CWD            string `json:"cwd,omitempty"`
	ToolName       string `json:"tool_name,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
}

// ParseEvent は JSON 形式の hook イベントを検証して返す。
func ParseEvent(line []byte) (Event, error) {
	var ev Event
	if err := json.Unmarshal(line, &ev); err != nil {
		return Event{}, fmt.Errorf("decode hook event: %w", err)
	}
	if ev.PaneID == "" {
		return Event{}, errors.New("pane_id is required")
	}
	if !paneIDPattern.MatchString(ev.PaneID) {
		return Event{}, fmt.Errorf("pane_id has invalid format: %q", ev.PaneID)
	}
	if ev.HookEventName == "" {
		return Event{}, errors.New("hook_event_name is required")
	}
	if len(ev.SessionID) > maxFieldLength {
		return Event{}, fmt.Errorf("session_id exceeds maximum length of %d bytes", maxFieldLength)
	}
	if len(ev.TranscriptPath) > maxFieldLength {
		return Event{}, fmt.Errorf("transcript_path exceeds maximum length of %d bytes", maxFieldLength)
	}
	if len(ev.ToolName) > maxFieldLength {
		return Event{}, fmt.Errorf("tool_name exceeds maximum length of %d bytes", maxFieldLength)
	}
	return ev, nil
}

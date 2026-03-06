package core

import (
	"encoding/json"
	"testing"
)

func TestSessionStateString(t *testing.T) {
	tests := []struct {
		state SessionState
		want  string
	}{
		{Idle, "idle"},
		{Thinking, "thinking"},
		{ToolUse, "tool_use"},
		{Error, "error"},
		{SessionState(999), "unknown"},
	}

	for _, tc := range tests {
		got := tc.state.String()
		if got != tc.want {
			t.Errorf("SessionState(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestSessionStateMarshalJSON(t *testing.T) {
	tests := []struct {
		state SessionState
		want  string
	}{
		{Idle, `"idle"`},
		{Thinking, `"thinking"`},
		{ToolUse, `"tool_use"`},
		{Error, `"error"`},
		{SessionState(999), `"unknown"`},
	}

	for _, tc := range tests {
		got, err := json.Marshal(tc.state)
		if err != nil {
			t.Fatalf("MarshalJSON(%d) error: %v", tc.state, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalJSON(%d) = %s, want %s", tc.state, got, tc.want)
		}
	}
}

func TestWatchEventTypeString(t *testing.T) {
	tests := []struct {
		eventType WatchEventType
		want      string
	}{
		{Created, "created"},
		{Modified, "modified"},
		{Removed, "removed"},
		{WatchEventType(999), "unknown"},
	}

	for _, tc := range tests {
		got := tc.eventType.String()
		if got != tc.want {
			t.Errorf("WatchEventType(%d).String() = %q, want %q", tc.eventType, got, tc.want)
		}
	}
}

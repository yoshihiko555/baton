package core

import (
	"encoding/json"
	"testing"
)

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

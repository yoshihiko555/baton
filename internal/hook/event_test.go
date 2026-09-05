package hook

import (
	"strings"
	"testing"
)

func TestParseEvent(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		want        Event
		wantErr     bool
		errContains string
	}{
		{
			name:  "valid full JSON",
			input: `{"pane_id":"%3","hook_event_name":"PermissionRequest","session_id":"session-1","transcript_path":"/tmp/transcript.jsonl","cwd":"/tmp/project","tool_name":"Bash","agent_id":"agent-1"}`,
			want: Event{
				PaneID:         "%3",
				HookEventName:  "PermissionRequest",
				SessionID:      "session-1",
				TranscriptPath: "/tmp/transcript.jsonl",
				CWD:            "/tmp/project",
				ToolName:       "Bash",
				AgentID:        "agent-1",
			},
		},
		{
			name:  "minimal valid JSON",
			input: `{"pane_id":"%1","hook_event_name":"Stop"}`,
			want: Event{
				PaneID:        "%1",
				HookEventName: "Stop",
			},
		},
		{
			name:  "valid multi-digit pane ID",
			input: `{"pane_id":"%23","hook_event_name":"Stop"}`,
			want: Event{
				PaneID:        "%23",
				HookEventName: "Stop",
			},
		},
		{
			name:        "missing pane ID",
			input:       `{"hook_event_name":"PermissionRequest"}`,
			wantErr:     true,
			errContains: "pane_id is required",
		},
		{
			name:        "non-numeric pane ID",
			input:       `{"pane_id":"%abc","hook_event_name":"PermissionRequest"}`,
			wantErr:     true,
			errContains: "pane_id has invalid format",
		},
		{
			name:        "pane ID without prefix",
			input:       `{"pane_id":"foo","hook_event_name":"PermissionRequest"}`,
			wantErr:     true,
			errContains: "pane_id has invalid format",
		},
		{
			name:        "missing hook event name",
			input:       `{"pane_id":"%3"}`,
			wantErr:     true,
			errContains: "hook_event_name is required",
		},
		{
			name:        "malformed JSON",
			input:       `{"pane_id":`,
			wantErr:     true,
			errContains: "decode hook event",
		},
		{
			name:        "session ID too large",
			input:       `{"pane_id":"%1","hook_event_name":"SessionStart","session_id":"` + strings.Repeat("a", maxFieldLength+1) + `"}`,
			wantErr:     true,
			errContains: "session_id exceeds maximum length",
		},
		{
			name:        "transcript path too large",
			input:       `{"pane_id":"%1","hook_event_name":"SessionStart","transcript_path":"` + strings.Repeat("a", maxFieldLength+1) + `"}`,
			wantErr:     true,
			errContains: "transcript_path exceeds maximum length",
		},
		{
			name:        "tool name too large",
			input:       `{"pane_id":"%1","hook_event_name":"PermissionRequest","tool_name":"` + strings.Repeat("a", maxFieldLength+1) + `"}`,
			wantErr:     true,
			errContains: "tool_name exceeds maximum length",
		},
		{
			name:  "unknown fields ignored",
			input: `{"pane_id":"%4","hook_event_name":"SessionStart","unknown":{"nested":true}}`,
			want: Event{
				PaneID:        "%4",
				HookEventName: "SessionStart",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseEvent([]byte(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Fatal("ParseEvent returned no error")
				}
				if !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("ParseEvent error = %q, want it to contain %q", err, tc.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseEvent returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParseEvent = %#v, want %#v", got, tc.want)
			}
		})
	}
}

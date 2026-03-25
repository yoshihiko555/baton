package core

import "testing"

func TestBuildTMUXStatus(t *testing.T) {
	tests := []struct {
		name     string
		projects []Project
		want     string
	}{
		{
			name: "thinking waiting idle are rendered in fixed order",
			projects: []Project{
				{
					Sessions: []*Session{
						{State: Idle},
						{State: Thinking},
						{State: Waiting},
						{State: ToolUse},
					},
				},
			},
			want: "🤔2 ✋1 💤1",
		},
		{
			name: "zero states are hidden",
			projects: []Project{
				{
					Sessions: []*Session{
						{State: Waiting},
						{State: Waiting},
					},
				},
			},
			want: "✋2",
		},
		{
			name: "no sessions returns empty string",
			projects: []Project{
				{
					Sessions: []*Session{},
				},
			},
			want: "",
		},
		{
			name: "sessions with other states do not appear",
			projects: []Project{
				{
					Sessions: []*Session{
						{State: Error},
						nil,
					},
				},
			},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildTMUXStatus(tc.projects)
			if got != tc.want {
				t.Fatalf("BuildTMUXStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

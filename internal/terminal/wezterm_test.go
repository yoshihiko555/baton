package terminal

import (
	"reflect"
	"testing"
)

func TestListPanes(t *testing.T) {
	sampleJSON := `[
		{"pane_id": 1, "title": "editor", "tab_id": "2", "cwd": "file:///tmp/project"},
		{"pane_id": "3", "title": "logs", "tab_id": 4, "cwd": "file:///tmp/logs"}
	]`

	wez := &WezTerminal{
		execFn: func(args ...string) ([]byte, error) {
			wantArgs := []string{"cli", "list", "--format", "json"}
			if !reflect.DeepEqual(args, wantArgs) {
				t.Fatalf("unexpected args: got=%v want=%v", args, wantArgs)
			}
			return []byte(sampleJSON), nil
		},
	}

	got, err := wez.ListPanes()
	if err != nil {
		t.Fatalf("ListPanes returned error: %v", err)
	}

	want := []Pane{
		{
			ID:         "1",
			Title:      "editor",
			TabID:      "2",
			WorkingDir: "file:///tmp/project",
		},
		{
			ID:         "3",
			Title:      "logs",
			TabID:      "4",
			WorkingDir: "file:///tmp/logs",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected panes: got=%+v want=%+v", got, want)
	}
}

func TestFocusPane(t *testing.T) {
	var gotArgs []string

	wez := &WezTerminal{
		execFn: func(args ...string) ([]byte, error) {
			gotArgs = append([]string{}, args...)
			return []byte(""), nil
		},
	}

	if err := wez.FocusPane("42"); err != nil {
		t.Fatalf("FocusPane returned error: %v", err)
	}

	wantArgs := []string{"cli", "activate-pane", "--pane-id", "42"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected args: got=%v want=%v", gotArgs, wantArgs)
	}
}

func TestName(t *testing.T) {
	wez := NewWezTerminal()
	if got, want := wez.Name(), "wezterm"; got != want {
		t.Fatalf("unexpected terminal name: got=%q want=%q", got, want)
	}
}

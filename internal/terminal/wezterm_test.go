package terminal

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func TestListPanes(t *testing.T) {
	// list コマンド結果を Pane 構造体へ正規化できることを確認する。
	sampleJSON := `[
		{"pane_id": 1, "title": "editor", "tab_id": 2, "cwd": "file://localhost/tmp/project", "tty_name": "/dev/ttys001", "is_active": true, "workspace": "default"},
		{"pane_id": 3, "title": "logs", "tab_id": 4, "cwd": "file:///tmp/logs", "tty_name": "/dev/ttys002", "is_active": false, "workspace": "default"}
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
			ID:         1,
			Title:      "editor",
			TabID:      2,
			WorkingDir: "/tmp/project",
			TTYName:    "/dev/ttys001",
			IsActive:   true,
			Workspace:  "default",
		},
		{
			ID:         3,
			Title:      "logs",
			TabID:      4,
			WorkingDir: "/tmp/logs",
			TTYName:    "/dev/ttys002",
			IsActive:   false,
			Workspace:  "default",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected panes: got=%+v want=%+v", got, want)
	}
}

func TestFocusPane(t *testing.T) {
	// 同一ワークスペース内のペインに activate-pane を正しく呼び出すことを確認する。
	var activateArgs []string

	listJSON := `[{"pane_id":42,"title":"t","tab_id":1,"cwd":"/tmp","tty_name":"/dev/ttys001","is_active":false,"workspace":"default"},{"pane_id":99,"title":"current","tab_id":2,"cwd":"/tmp","tty_name":"/dev/ttys002","is_active":true,"workspace":"default"}]`

	wez := &WezTerminal{
		execFn: func(args ...string) ([]byte, error) {
			if len(args) >= 2 && args[1] == "list" {
				return []byte(listJSON), nil
			}
			if len(args) >= 2 && args[1] == "activate-pane" {
				activateArgs = append([]string{}, args...)
			}
			return []byte(""), nil
		},
	}

	if err := wez.FocusPane(42); err != nil {
		t.Fatalf("FocusPane returned error: %v", err)
	}

	wantArgs := []string{"cli", "activate-pane", "--pane-id", "42"}
	if !reflect.DeepEqual(activateArgs, wantArgs) {
		t.Fatalf("unexpected args: got=%v want=%v", activateArgs, wantArgs)
	}
}

func TestName(t *testing.T) {
	// 実装識別子が固定値 "wezterm" であることを確認する。
	wez := NewWezTerminal()
	if got, want := wez.Name(), "wezterm"; got != want {
		t.Fatalf("unexpected terminal name: got=%q want=%q", got, want)
	}
}

func TestNewWezTerminalExecFn(t *testing.T) {
	wez := NewWezTerminal()
	if wez == nil {
		t.Fatal("NewWezTerminal returned nil")
	}
	if wez.execFn == nil {
		t.Fatal("execFn should be set")
	}
}

func TestListPanesNilExecFn(t *testing.T) {
	wez := &WezTerminal{execFn: nil}
	_, err := wez.ListPanes()
	if err == nil {
		t.Fatal("expected error for nil execFn")
	}
}

func TestListPanesNilReceiver(t *testing.T) {
	var wez *WezTerminal
	_, err := wez.ListPanes()
	if err == nil {
		t.Fatal("expected error for nil receiver")
	}
}

func TestListPanesExecError(t *testing.T) {
	wez := &WezTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return nil, errors.New("command failed")
		},
	}
	_, err := wez.ListPanes()
	if err == nil {
		t.Fatal("expected error from exec failure")
	}
}

func TestListPanesInvalidJSON(t *testing.T) {
	wez := &WezTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return []byte("not json"), nil
		},
	}
	_, err := wez.ListPanes()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestListPanesInvalidPaneID(t *testing.T) {
	// pane_id が配列などの非対応型の場合は json.Unmarshal でエラーになることを確認する。
	wez := &WezTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return []byte(`[{"pane_id": [1,2], "title": "t", "tab_id": 1, "cwd": "/tmp"}]`), nil
		},
	}
	_, err := wez.ListPanes()
	if err == nil {
		t.Fatal("expected error for unsupported pane_id type")
	}
}

func TestListPanesInvalidTabID(t *testing.T) {
	// tab_id がオブジェクトの場合は json.Unmarshal でエラーになることを確認する。
	wez := &WezTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return []byte(`[{"pane_id": 1, "title": "t", "tab_id": {"nested": true}, "cwd": "/tmp"}]`), nil
		},
	}
	_, err := wez.ListPanes()
	if err == nil {
		t.Fatal("expected error for unsupported tab_id type")
	}
}

func TestFocusPaneNilExecFn(t *testing.T) {
	wez := &WezTerminal{execFn: nil}
	err := wez.FocusPane(1)
	if err == nil {
		t.Fatal("expected error for nil execFn")
	}
}

func TestFocusPaneNilReceiver(t *testing.T) {
	var wez *WezTerminal
	err := wez.FocusPane(1)
	if err == nil {
		t.Fatal("expected error for nil receiver")
	}
}

func TestFocusPaneExecError(t *testing.T) {
	wez := &WezTerminal{
		execFn: func(args ...string) ([]byte, error) {
			return nil, errors.New("activate failed")
		},
	}
	err := wez.FocusPane(42)
	if err == nil {
		t.Fatal("expected error from exec failure")
	}
}

func TestIsAvailable(t *testing.T) {
	wez := NewWezTerminal()
	// wezterm の有無は環境依存なので bool が返ることだけ確認する。
	_ = wez.IsAvailable()
}

func TestMapWeztermExecErrorNotFound(t *testing.T) {
	err := mapWeztermExecError(exec.ErrNotFound)
	if !errors.Is(err, ErrTerminalNotFound) {
		t.Errorf("expected ErrTerminalNotFound, got %v", err)
	}
}

func TestMapWeztermExecErrorOther(t *testing.T) {
	original := errors.New("some other error")
	err := mapWeztermExecError(original)
	if err != original {
		t.Errorf("expected original error passthrough, got %v", err)
	}
}

func TestNormalizeCWD(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"file://localhost prefix", "file://localhost/Users/foo/project", "/Users/foo/project"},
		{"file:// prefix", "file:///tmp/project", "/tmp/project"},
		{"already absolute path", "/tmp/project", "/tmp/project"},
		{"trailing slash removed", "file://localhost/Users/foo/", "/Users/foo"},
		{"root path preserved", "/", "/"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeCWD(tt.input)
			if got != tt.want {
				t.Errorf("normalizeCWD(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

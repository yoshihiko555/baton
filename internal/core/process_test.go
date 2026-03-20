package core

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
)

// makeExitError は指定した終了コードを持つ *exec.ExitError を生成する。
func makeExitError(code int) error {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code))
	err := cmd.Run()
	return err
}

func TestHasChildProcesses(t *testing.T) {
	tests := []struct {
		name    string
		handler func(name string, args ...string) ([]byte, error)
		wantHas bool
		wantErr bool
	}{
		{
			name: "with work children",
			handler: func(name string, args ...string) ([]byte, error) {
				if name == "pgrep" {
					return []byte("12345\n"), nil
				}
				// ps: 作業用プロセス
				return []byte("sandbox-exec\n"), nil
			},
			wantHas: true,
		},
		{
			name: "only background children",
			handler: func(name string, args ...string) ([]byte, error) {
				if name == "pgrep" {
					return []byte("12345\n12346\n12347\n"), nil
				}
				return []byte("node\ncaffeinate\ngopls\n"), nil
			},
			wantHas: false,
		},
		{
			name: "background plus work children",
			handler: func(name string, args ...string) ([]byte, error) {
				if name == "pgrep" {
					return []byte("12345\n12346\n"), nil
				}
				return []byte("node\nsandbox-exec\n"), nil
			},
			wantHas: true,
		},
		{
			name: "no children (pgrep exit code 1)",
			handler: func(name string, args ...string) ([]byte, error) {
				return nil, makeExitError(1)
			},
			wantHas: false,
		},
		{
			name: "pgrep exec error (exit code 2)",
			handler: func(name string, args ...string) ([]byte, error) {
				return nil, makeExitError(2)
			},
			wantHas: false,
			wantErr: true,
		},
		{
			name: "claude-tmux is background",
			handler: func(name string, args ...string) ([]byte, error) {
				if name == "pgrep" {
					return []byte("12345\n"), nil
				}
				return []byte("claude-tmux\n"), nil
			},
			wantHas: false,
		},
		{
			name: "ps failure fallback to thinking",
			handler: func(name string, args ...string) ([]byte, error) {
				if name == "pgrep" {
					return []byte("12345\n"), nil
				}
				return nil, fmt.Errorf("ps failed")
			},
			wantHas: true, // ps 失敗時はフォールバックで true
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scanner := NewProcessScannerWithExec(func(_ context.Context, name string, args ...string) ([]byte, error) {
				return tc.handler(name, args...)
			})

			got, err := scanner.HasChildProcesses(context.Background(), 12345)

			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if got != tc.wantHas {
				t.Errorf("HasChildProcesses() = %v, want %v", got, tc.wantHas)
			}
		})
	}
}

func TestHasChildProcessesUV(t *testing.T) {
	tests := []struct {
		name    string
		handler func(name string, args ...string) ([]byte, error)
		wantHas bool
		wantErr bool
	}{
		{
			name: "only uv child is background",
			handler: func(name string, args ...string) ([]byte, error) {
				if name == "pgrep" {
					return []byte("12345\n"), nil
				}
				// ps: uv のみ（バックグラウンドプロセス）
				return []byte("uv\n"), nil
			},
			wantHas: false,
		},
		{
			name: "uv plus work process",
			handler: func(name string, args ...string) ([]byte, error) {
				if name == "pgrep" {
					return []byte("12345\n12346\n"), nil
				}
				// ps: uv（バックグラウンド）＋ sandbox-exec（作業用）
				return []byte("uv\nsandbox-exec\n"), nil
			},
			wantHas: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scanner := NewProcessScannerWithExec(func(_ context.Context, name string, args ...string) ([]byte, error) {
				return tc.handler(name, args...)
			})

			got, err := scanner.HasChildProcesses(context.Background(), 12345)

			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if got != tc.wantHas {
				t.Errorf("HasChildProcesses() = %v, want %v", got, tc.wantHas)
			}
		})
	}
}

func TestParseARGSFallback(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		wantLen  int
		wantTool ToolType
		wantName string
	}{
		{
			name: "gemini detected via ARGS",
			output: "  PID  PPID COMM                                       ARGS\n" +
				" 10235  7338 node                                       node --no-warnings=DEP0040 /opt/homebrew/bin/gemini\n",
			wantLen:  1,
			wantTool: ToolGemini,
			wantName: "gemini",
		},
		{
			name: "claude detected via COMM (no fallback needed)",
			output: "  PID  PPID COMM     ARGS\n" +
				" 1234  5678 claude   claude\n",
			wantLen:  1,
			wantTool: ToolClaude,
			wantName: "claude",
		},
		{
			name: "node without AI tool in ARGS",
			output: "  PID  PPID COMM   ARGS\n" +
				" 1234  5678 node   node /usr/local/bin/serve\n",
			wantLen: 0,
		},
		{
			name: "gemini with mise path",
			output: "  PID  PPID COMM                                                                ARGS\n" +
				" 12554 10235 /Users/user/.local/share/mise/installs/node/24.14.0/bin/node   /Users/user/.local/share/mise/installs/node/24.14.0/bin/node --no-warnings=DEP0040 /opt/homebrew/bin/gemini\n",
			wantLen:  1,
			wantTool: ToolGemini,
			wantName: "gemini",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := NewProcessScannerWithExec(nil)
			got := ps.parse([]byte(tc.output))
			if len(got) != tc.wantLen {
				t.Fatalf("parse() returned %d results, want %d", len(got), tc.wantLen)
			}
			if tc.wantLen > 0 {
				if got[0].ToolType != tc.wantTool {
					t.Errorf("ToolType = %v, want %v", got[0].ToolType, tc.wantTool)
				}
				if got[0].Name != tc.wantName {
					t.Errorf("Name = %q, want %q", got[0].Name, tc.wantName)
				}
			}
		})
	}
}

func TestParseDeduplicatesParentChild(t *testing.T) {
	// 親子の node プロセスが両方 gemini を含む場合、親のみ採用されることを確認する。
	output := "  PID  PPID COMM   ARGS\n" +
		" 10235  7338 node   node --no-warnings=DEP0040 /opt/homebrew/bin/gemini\n" +
		" 12554 10235 node   node --no-warnings=DEP0040 /opt/homebrew/bin/gemini\n"

	ps := NewProcessScannerWithExec(nil)
	got := ps.parse([]byte(output))
	if len(got) != 1 {
		t.Fatalf("parse() returned %d results, want 1 (parent only)", len(got))
	}
	if got[0].PID != 10235 {
		t.Errorf("PID = %d, want 10235 (parent)", got[0].PID)
	}
}

func TestParseKeepsDifferentTools(t *testing.T) {
	// 異なるツールの親子は両方残ることを確認する。
	output := "  PID  PPID COMM     ARGS\n" +
		" 1000  500 claude   claude\n" +
		" 2000 1000 node     node /opt/homebrew/bin/gemini\n"

	ps := NewProcessScannerWithExec(nil)
	got := ps.parse([]byte(output))
	if len(got) != 2 {
		t.Fatalf("parse() returned %d results, want 2 (different tools)", len(got))
	}
}

func TestDetectFromArgs(t *testing.T) {
	tests := []struct {
		args     string
		wantTool ToolType
		wantOK   bool
	}{
		{"/opt/homebrew/bin/gemini", ToolGemini, true},
		{"node --no-warnings=DEP0040 /opt/homebrew/bin/gemini", ToolGemini, true},
		{"/usr/local/bin/claude", ToolClaude, true},
		{"node /usr/local/bin/serve", ToolUnknown, false},
		{"python script.py", ToolUnknown, false},
		{"/usr/local/bin/gemini-beta", ToolUnknown, false},
		{"node /opt/homebrew/bin/claude-wrapper", ToolUnknown, false},
	}

	for _, tc := range tests {
		t.Run(tc.args, func(t *testing.T) {
			got, ok := detectFromArgs(tc.args)
			if ok != tc.wantOK {
				t.Errorf("detectFromArgs(%q) ok = %v, want %v", tc.args, ok, tc.wantOK)
			}
			if got != tc.wantTool {
				t.Errorf("detectFromArgs(%q) = %v, want %v", tc.args, got, tc.wantTool)
			}
		})
	}
}

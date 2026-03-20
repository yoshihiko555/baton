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

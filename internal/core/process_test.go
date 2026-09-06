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
			name: "agy detected via ARGS",
			output: "  PID  PPID COMM                                       ARGS\n" +
				" 10235  7338 node                                       node --no-warnings=DEP0040 /opt/homebrew/bin/agy\n",
			wantLen:  1,
			wantTool: ToolAntigravity,
			wantName: "agy",
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
			name: "agy with mise path",
			output: "  PID  PPID COMM                                                                ARGS\n" +
				" 12554 10235 /Users/user/.local/share/mise/installs/node/24.14.0/bin/node   /Users/user/.local/share/mise/installs/node/24.14.0/bin/node --no-warnings=DEP0040 /opt/homebrew/bin/agy\n",
			wantLen:  1,
			wantTool: ToolAntigravity,
			wantName: "agy",
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
	// 親子の node プロセスが両方 agy を含む場合、親のみ採用されることを確認する。
	output := "  PID  PPID COMM   ARGS\n" +
		" 10235  7338 node   node --no-warnings=DEP0040 /opt/homebrew/bin/agy\n" +
		" 12554 10235 node   node --no-warnings=DEP0040 /opt/homebrew/bin/agy\n"

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
		" 2000 1000 node     node /opt/homebrew/bin/agy\n"

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
		{"/opt/homebrew/bin/agy", ToolAntigravity, true},
		{"node --no-warnings=DEP0040 /opt/homebrew/bin/agy", ToolAntigravity, true},
		{"/usr/local/bin/claude", ToolClaude, true},
		{"node /usr/local/bin/serve", ToolUnknown, false},
		{"python script.py", ToolUnknown, false},
		{"/usr/local/bin/agy-beta", ToolUnknown, false},
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

func TestParseOpenCode(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		wantLen  int
		wantTool ToolType
		wantName string
	}{
		{
			name: "opencode detected via COMM",
			output: "  PID  PPID COMM             ARGS\n" +
				"65096 64858 opencode         opencode\n",
			wantLen:  1,
			wantTool: ToolOpenCode,
			wantName: "opencode",
		},
		{
			// takt が起動する HTTP バックエンド。対話セッションではないため除外する（ADR-0016）。
			name: "opencode serve is excluded (takt HTTP backend)",
			output: "  PID  PPID COMM             ARGS\n" +
				"70000 64858 opencode         opencode serve --hostname=127.0.0.1 --port=4096\n",
			wantLen: 0,
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

func TestParseDetectsTaktViaAncestry(t *testing.T) {
	// zsh(1000) → node .../node_modules/takt/dist/app/cli/index.js run(2000, ppid 1000)
	//   → claude -p --verbose --output-format stream-json(3000, ppid 2000)
	// 同一 TTY の対話 claude(4000, ppid 1000) は takt の子孫ではないため Via は空。
	output := "  PID  PPID COMM     ARGS\n" +
		" 1000     1 zsh      -zsh\n" +
		" 2000  1000 node     node /Users/user/project/node_modules/takt/dist/app/cli/index.js run\n" +
		" 3000  2000 claude   claude -p --verbose --output-format stream-json --input-format stream-json\n" +
		" 4000  1000 claude   claude\n"

	ps := NewProcessScannerWithExec(nil)
	got := ps.parse([]byte(output))

	byPID := make(map[int]DetectedProcess, len(got))
	for _, p := range got {
		byPID[p.PID] = p
	}

	if len(got) != 2 {
		t.Fatalf("parse() returned %d results, want 2 (takt-spawned claude + interactive claude): %+v", len(got), got)
	}
	if via := byPID[3000].Via; via != ViaTakt {
		t.Errorf("PID 3000 Via = %q, want %q (spawned by takt)", via, ViaTakt)
	}
	if via := byPID[4000].Via; via != "" {
		t.Errorf("PID 4000 Via = %q, want empty (interactive session)", via)
	}
}

func TestParseDetectsTaktViaAncestryForCodex(t *testing.T) {
	// takt は codex exec も同じ方式（stdio=pipe、同一 TTY）で起動する。
	output := "  PID  PPID COMM     ARGS\n" +
		" 1000     1 zsh      -zsh\n" +
		" 2000  1000 node     node /Users/user/project/node_modules/takt/dist/app/cli/index.js run\n" +
		" 3000  2000 codex    codex exec --experimental-json\n"

	ps := NewProcessScannerWithExec(nil)
	got := ps.parse([]byte(output))
	if len(got) != 1 {
		t.Fatalf("parse() returned %d results, want 1: %+v", len(got), got)
	}
	if got[0].Via != ViaTakt {
		t.Errorf("Via = %q, want %q", got[0].Via, ViaTakt)
	}
}

func TestResolveViaHandlesCyclicPPID(t *testing.T) {
	// 循環参照や壊れた ps 出力でも無限ループせずに空文字を返すこと。
	byPID := map[int]psRow{
		100: {ppid: 200, args: "claude -p"},
		200: {ppid: 100, args: "node something-unrelated"},
	}
	if got := resolveVia(100, byPID); got != "" {
		t.Errorf("resolveVia() = %q, want empty (cycle guard)", got)
	}
}

func TestResolveViaMissingAncestorReturnsEmpty(t *testing.T) {
	byPID := map[int]psRow{
		100: {ppid: 999, args: "claude -p"},
	}
	if got := resolveVia(100, byPID); got != "" {
		t.Errorf("resolveVia() = %q, want empty (ancestor not found)", got)
	}
}

func TestIsTaktProcess(t *testing.T) {
	tests := []struct {
		args string
		want bool
	}{
		{"node /Users/user/project/node_modules/takt/dist/app/cli/index.js run", true},
		{"node /Users/user/project/node_modules/.bin/some-other-tool", false},
		{"claude -p --verbose", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.args, func(t *testing.T) {
			if got := isTaktProcess(tc.args); got != tc.want {
				t.Errorf("isTaktProcess(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestIsOpenCodeServer(t *testing.T) {
	tests := []struct {
		args string
		want bool
	}{
		{"opencode serve --hostname=127.0.0.1 --port=4096", true},
		{"opencode", false},
		{"opencode --version", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.args, func(t *testing.T) {
			if got := isOpenCodeServer(tc.args); got != tc.want {
				t.Errorf("isOpenCodeServer(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

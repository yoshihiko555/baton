package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	// デフォルト値が意図した固定値になっていることを確認する。
	got := Default()

	if got.ClaudeProjectsDir != "~/.claude/projects" {
		t.Fatalf("unexpected ClaudeProjectsDir: got %q", got.ClaudeProjectsDir)
	}
	if got.StatusOutputPath != "/tmp/baton-status.json" {
		t.Fatalf("unexpected StatusOutputPath: got %q", got.StatusOutputPath)
	}
	if got.ScanInterval != 2*time.Second {
		t.Fatalf("unexpected ScanInterval: got %v", got.ScanInterval)
	}
	if got.Terminal != "tmux" {
		t.Fatalf("unexpected Terminal: got %q", got.Terminal)
	}
	if got.LogLevel != "info" {
		t.Fatalf("unexpected LogLevel: got %q", got.LogLevel)
	}
	if got.Statusbar.Format != "{{.Active}}/{{.TotalSessions}}" {
		t.Fatalf("unexpected Statusbar.Format: got %q", got.Statusbar.Format)
	}
	if got.Statusbar.ToolIcons["default"] != "●" {
		t.Fatalf("unexpected Statusbar.ToolIcons[default]: got %q", got.Statusbar.ToolIcons["default"])
	}
}

func TestLoadValidYAML(t *testing.T) {
	// 全項目を指定した YAML から正しく読み込めることを確認する。
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `claude_projects_dir: ~/custom/projects
session_meta_dir: ~/custom/meta
status_output_path: /tmp/custom-status.json
scan_interval: 5s
terminal: tmux
log_level: debug
statusbar:
  format: "{{.Active}} sessions"
  tool_icons:
    default: "◆"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}

	if got.ClaudeProjectsDir != filepath.Join(home, "custom/projects") {
		t.Fatalf("unexpected ClaudeProjectsDir: got %q", got.ClaudeProjectsDir)
	}
	if got.SessionMetaDir != filepath.Join(home, "custom/meta") {
		t.Fatalf("unexpected SessionMetaDir: got %q", got.SessionMetaDir)
	}
	if got.StatusOutputPath != "/tmp/custom-status.json" {
		t.Fatalf("unexpected StatusOutputPath: got %q", got.StatusOutputPath)
	}
	if got.ScanInterval != 5*time.Second {
		t.Fatalf("unexpected ScanInterval: got %v", got.ScanInterval)
	}
	if got.Terminal != "tmux" {
		t.Fatalf("unexpected Terminal: got %q", got.Terminal)
	}
	if got.LogLevel != "debug" {
		t.Fatalf("unexpected LogLevel: got %q", got.LogLevel)
	}
	if got.Statusbar.Format != "{{.Active}} sessions" {
		t.Fatalf("unexpected Statusbar.Format: got %q", got.Statusbar.Format)
	}
	if got.Statusbar.ToolIcons["default"] != "◆" {
		t.Fatalf("unexpected Statusbar.ToolIcons[default]: got %q", got.Statusbar.ToolIcons["default"])
	}
}

func TestLoadPartialYAML(t *testing.T) {
	// 一部項目のみ指定した場合、未指定項目はデフォルト値が維持されることを確認する。
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `terminal: alacritty
status_output_path: ~/tmp/baton-status.json
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}

	if got.ClaudeProjectsDir != filepath.Join(home, ".claude/projects") {
		t.Fatalf("unexpected ClaudeProjectsDir: got %q", got.ClaudeProjectsDir)
	}
	if got.StatusOutputPath != filepath.Join(home, "tmp/baton-status.json") {
		t.Fatalf("unexpected StatusOutputPath: got %q", got.StatusOutputPath)
	}
	if got.ScanInterval != 2*time.Second {
		t.Fatalf("unexpected ScanInterval: got %v", got.ScanInterval)
	}
	if got.Terminal != "alacritty" {
		t.Fatalf("unexpected Terminal: got %q", got.Terminal)
	}
	if got.LogLevel != "info" {
		t.Fatalf("unexpected LogLevel: got %q", got.LogLevel)
	}
}

func TestLoadMissingYAML(t *testing.T) {
	// 設定ファイルが存在しない場合でもエラーにせずデフォルト値を返すことを確認する。
	path := filepath.Join(t.TempDir(), "not-found.yaml")

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error for missing file: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}

	if got.ClaudeProjectsDir != filepath.Join(home, ".claude/projects") {
		t.Fatalf("unexpected ClaudeProjectsDir: got %q", got.ClaudeProjectsDir)
	}
	if got.StatusOutputPath != "/tmp/baton-status.json" {
		t.Fatalf("unexpected StatusOutputPath: got %q", got.StatusOutputPath)
	}
	if got.ScanInterval != 2*time.Second {
		t.Fatalf("unexpected ScanInterval: got %v", got.ScanInterval)
	}
	if got.Terminal != "tmux" {
		t.Fatalf("unexpected Terminal: got %q", got.Terminal)
	}
	if got.LogLevel != "info" {
		t.Fatalf("unexpected LogLevel: got %q", got.LogLevel)
	}
}

func TestExpandHome(t *testing.T) {
	// "~" 展開ルールをテーブル駆動で検証する。
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "tilde only",
			in:   "~",
			want: home,
		},
		{
			name: "tilde path",
			in:   "~/foo/bar",
			want: filepath.Join(home, "foo/bar"),
		},
		{
			name: "regular path unchanged",
			in:   "/tmp/example",
			want: "/tmp/example",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := expandHome(tc.in)
			if err != nil {
				t.Fatalf("expandHome returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected path: got %q, want %q", got, tc.want)
			}
		})
	}
}

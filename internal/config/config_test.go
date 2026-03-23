package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// unmarshalConfig is a test helper that unmarshals YAML bytes into Config.
func unmarshalConfig(data []byte, cfg *Config) error {
	return yaml.Unmarshal(data, cfg)
}

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

func TestThemeConfigUnmarshalYAMLStringShorthand(t *testing.T) {
	// theme: deep-sea-glow (scalar) should set Preset field.
	content := `theme: deep-sea-glow`
	var cfg Config
	if err := unmarshalConfig([]byte(content), &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if cfg.Theme.Preset != "deep-sea-glow" {
		t.Errorf("Theme.Preset = %q, want %q", cfg.Theme.Preset, "deep-sea-glow")
	}
}

func TestThemeConfigUnmarshalYAMLMapping(t *testing.T) {
	// theme: as mapping should unmarshal individual fields.
	content := `
theme:
  preset: synthwave-peach
  active_border: "#FF0000"
  brand: "#00FF00"
  states:
    idle: "#AAAAAA"
  tools:
    claude: "#BBBBBB"
  group_headers:
    IDLE: "#CCCCCC"
`
	var cfg Config
	if err := unmarshalConfig([]byte(content), &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if cfg.Theme.Preset != "synthwave-peach" {
		t.Errorf("Theme.Preset = %q, want synthwave-peach", cfg.Theme.Preset)
	}
	if cfg.Theme.ActiveBorder != "#FF0000" {
		t.Errorf("Theme.ActiveBorder = %q, want #FF0000", cfg.Theme.ActiveBorder)
	}
	if cfg.Theme.Brand != "#00FF00" {
		t.Errorf("Theme.Brand = %q, want #00FF00", cfg.Theme.Brand)
	}
	if cfg.Theme.States["idle"] != "#AAAAAA" {
		t.Errorf("Theme.States[idle] = %q, want #AAAAAA", cfg.Theme.States["idle"])
	}
	if cfg.Theme.Tools["claude"] != "#BBBBBB" {
		t.Errorf("Theme.Tools[claude] = %q, want #BBBBBB", cfg.Theme.Tools["claude"])
	}
	if cfg.Theme.GroupHeaders["IDLE"] != "#CCCCCC" {
		t.Errorf("Theme.GroupHeaders[IDLE] = %q, want #CCCCCC", cfg.Theme.GroupHeaders["IDLE"])
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

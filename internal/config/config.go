package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// StatusbarConfig はステータスバーの表示設定を表す。
type StatusbarConfig struct {
	Format     string            `yaml:"format"`
	ToolIcons  map[string]string `yaml:"tool_icons"`
	StateIcons map[string]string `yaml:"state_icons"`
}

// ThemeConfig はテーマ/カラー設定を表す。
// プリセット名（文字列）またはカスタムカラー定義を指定できる。
type ThemeConfig struct {
	Preset         string            `yaml:"preset"`
	ActiveBorder   string            `yaml:"active_border"`
	InactiveBorder string            `yaml:"inactive_border"`
	Brand          string            `yaml:"brand"`
	States         map[string]string `yaml:"states"`
	Tools          map[string]string `yaml:"tools"`
	GroupHeaders   map[string]string `yaml:"group_headers"`
}

// UnmarshalYAML はスカラー文字列をプリセット名として扱う。
func (t *ThemeConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		t.Preset = value.Value
		return nil
	}
	// mapping の場合は通常のアンマーシャルを行う
	type rawThemeConfig ThemeConfig
	return value.Decode((*rawThemeConfig)(t))
}

// Config は YAML から読み込む baton の実行時設定を表す。
type Config struct {
	ScanInterval      time.Duration   `yaml:"scan_interval"`
	ClaudeProjectsDir string          `yaml:"claude_projects_dir"`
	SessionMetaDir    string          `yaml:"session_meta_dir"`
	StatusOutputPath  string          `yaml:"status_output_path"`
	Terminal          string          `yaml:"terminal"`
	LogLevel          string          `yaml:"log_level"`
	Statusbar         StatusbarConfig `yaml:"statusbar"`
	Theme             ThemeConfig     `yaml:"theme"`
}

// Default はデフォルト設定値を返す。
func Default() Config {
	return Config{
		ScanInterval:      2 * time.Second,
		ClaudeProjectsDir: "~/.claude/projects",
		SessionMetaDir:    "~/.claude/projects",
		StatusOutputPath:  "/tmp/baton-status.json",
		Terminal:          "tmux",
		LogLevel:          "info",
		Statusbar: StatusbarConfig{
			Format: "{{.Active}}/{{.TotalSessions}}",
			ToolIcons: map[string]string{
				"default": "●",
			},
			StateIcons: map[string]string{
				"working": "🤔",
				"waiting": "✋",
				"idle":    "~",
			},
		},
	}
}

// Load は YAML 設定ファイルを読み込み、デフォルト設定に上書きして返す。
func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return Config{}, fmt.Errorf("read config %q: %w", path, err)
			}
		} else if len(strings.TrimSpace(string(data))) > 0 {
			var loaded Config
			if err := yaml.Unmarshal(data, &loaded); err != nil {
				return Config{}, fmt.Errorf("parse config %q: %w", path, err)
			}
			mergeConfig(&cfg, loaded)
		}
	}

	var err error
	cfg.ClaudeProjectsDir, err = expandHome(cfg.ClaudeProjectsDir)
	if err != nil {
		return Config{}, fmt.Errorf("expand claude_projects_dir: %w", err)
	}
	cfg.SessionMetaDir, err = expandHome(cfg.SessionMetaDir)
	if err != nil {
		return Config{}, fmt.Errorf("expand session_meta_dir: %w", err)
	}
	cfg.StatusOutputPath, err = expandHome(cfg.StatusOutputPath)
	if err != nil {
		return Config{}, fmt.Errorf("expand status_output_path: %w", err)
	}

	return cfg, nil
}

// mergeConfig は非ゼロ値のみで base を上書きする。
func mergeConfig(base *Config, override Config) {
	if override.ScanInterval != 0 {
		base.ScanInterval = override.ScanInterval
	}
	if override.ClaudeProjectsDir != "" {
		base.ClaudeProjectsDir = override.ClaudeProjectsDir
	}
	if override.SessionMetaDir != "" {
		base.SessionMetaDir = override.SessionMetaDir
	}
	if override.StatusOutputPath != "" {
		base.StatusOutputPath = override.StatusOutputPath
	}
	if override.Terminal != "" {
		base.Terminal = override.Terminal
	}
	if override.LogLevel != "" {
		base.LogLevel = override.LogLevel
	}
	if override.Statusbar.Format != "" {
		base.Statusbar.Format = override.Statusbar.Format
	}
	if len(override.Statusbar.ToolIcons) > 0 {
		base.Statusbar.ToolIcons = override.Statusbar.ToolIcons
	}
	if len(override.Statusbar.StateIcons) > 0 {
		base.Statusbar.StateIcons = override.Statusbar.StateIcons
	}
	mergeThemeConfig(&base.Theme, override.Theme)
}

// mergeThemeConfig は非ゼロ値のみで base の Theme を上書きする。
func mergeThemeConfig(base *ThemeConfig, override ThemeConfig) {
	if override.Preset != "" {
		base.Preset = override.Preset
	}
	if override.ActiveBorder != "" {
		base.ActiveBorder = override.ActiveBorder
	}
	if override.InactiveBorder != "" {
		base.InactiveBorder = override.InactiveBorder
	}
	if override.Brand != "" {
		base.Brand = override.Brand
	}
	if len(override.States) > 0 {
		if base.States == nil {
			base.States = make(map[string]string)
		}
		for k, v := range override.States {
			base.States[k] = v
		}
	}
	if len(override.Tools) > 0 {
		if base.Tools == nil {
			base.Tools = make(map[string]string)
		}
		for k, v := range override.Tools {
			base.Tools[k] = v
		}
	}
	if len(override.GroupHeaders) > 0 {
		if base.GroupHeaders == nil {
			base.GroupHeaders = make(map[string]string)
		}
		for k, v := range override.GroupHeaders {
			base.GroupHeaders[k] = v
		}
	}
}

// expandHome は "~" プレフィックスをユーザーホームへ展開する。
func expandHome(path string) (string, error) {
	if path == "" {
		return path, nil
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

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
	Format    string            `yaml:"format"`
	ToolIcons map[string]string `yaml:"tool_icons"`
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
}

// Default はデフォルト設定値を返す。
func Default() Config {
	return Config{
		ScanInterval:      2 * time.Second,
		ClaudeProjectsDir: "~/.claude/projects",
		SessionMetaDir:    "~/.claude/projects",
		StatusOutputPath:  "/tmp/baton-status.json",
		Terminal:          "wezterm",
		LogLevel:          "info",
		Statusbar: StatusbarConfig{
			Format: "{{.Active}}/{{.TotalSessions}}",
			ToolIcons: map[string]string{
				"default": "●",
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

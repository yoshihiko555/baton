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

// Config represents baton runtime configuration loaded from YAML.
type Config struct {
	WatchPath        string        `yaml:"watch_path"`
	StatusOutputPath string        `yaml:"status_output_path"`
	RefreshInterval  time.Duration `yaml:"refresh_interval"`
	Terminal         string        `yaml:"terminal"`
	LogLevel         string        `yaml:"log_level"`
}

// Default returns the default configuration values.
func Default() Config {
	return Config{
		WatchPath:        "~/.claude/projects",
		StatusOutputPath: "/tmp/baton-status.json",
		RefreshInterval:  2 * time.Second,
		Terminal:         "wezterm",
		LogLevel:         "info",
	}
}

// Load reads the YAML configuration file and merges it with defaults.
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
	cfg.WatchPath, err = expandHome(cfg.WatchPath)
	if err != nil {
		return Config{}, fmt.Errorf("expand watch_path: %w", err)
	}
	cfg.StatusOutputPath, err = expandHome(cfg.StatusOutputPath)
	if err != nil {
		return Config{}, fmt.Errorf("expand status_output_path: %w", err)
	}

	return cfg, nil
}

func mergeConfig(base *Config, override Config) {
	if override.WatchPath != "" {
		base.WatchPath = override.WatchPath
	}
	if override.StatusOutputPath != "" {
		base.StatusOutputPath = override.StatusOutputPath
	}
	if override.RefreshInterval != 0 {
		base.RefreshInterval = override.RefreshInterval
	}
	if override.Terminal != "" {
		base.Terminal = override.Terminal
	}
	if override.LogLevel != "" {
		base.LogLevel = override.LogLevel
	}
}

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

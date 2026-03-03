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

// Config は YAML から読み込む baton の実行時設定を表す。
type Config struct {
	WatchPath        string        `yaml:"watch_path"`
	StatusOutputPath string        `yaml:"status_output_path"`
	RefreshInterval  time.Duration `yaml:"refresh_interval"`
	Terminal         string        `yaml:"terminal"`
	LogLevel         string        `yaml:"log_level"`
}

// Default はデフォルト設定値を返す。
func Default() Config {
	return Config{
		WatchPath:        "~/.claude/projects",
		StatusOutputPath: "/tmp/baton-status.json",
		RefreshInterval:  2 * time.Second,
		Terminal:         "wezterm",
		LogLevel:         "info",
	}
}

// Load は YAML 設定ファイルを読み込み、デフォルト設定に上書きして返す。
func Load(path string) (Config, error) {
	// まずデフォルト値を起点にし、指定された値だけを上書きする。
	cfg := Default()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return Config{}, fmt.Errorf("read config %q: %w", path, err)
			}
			// 設定ファイルが存在しない場合はデフォルト値を使って続行する。
		} else if len(strings.TrimSpace(string(data))) > 0 {
			var loaded Config
			if err := yaml.Unmarshal(data, &loaded); err != nil {
				return Config{}, fmt.Errorf("parse config %q: %w", path, err)
			}
			mergeConfig(&cfg, loaded)
		}
	}

	// "~" や "~/" を実際のホームディレクトリへ展開する。
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

// mergeConfig は非ゼロ値のみで base を上書きする。
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

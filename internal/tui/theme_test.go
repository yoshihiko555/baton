package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/yoshihiko555/baton/internal/config"
	"github.com/yoshihiko555/baton/internal/core"
)

func TestResolveThemeEmpty(t *testing.T) {
	// Empty config should return deep-sea-glow defaults.
	theme := ResolveTheme(config.ThemeConfig{})

	expected := deepSeaGlow()
	if theme.ActiveBorder != expected.ActiveBorder {
		t.Errorf("ActiveBorder = %q, want %q", theme.ActiveBorder, expected.ActiveBorder)
	}
	if theme.InactiveBorder != expected.InactiveBorder {
		t.Errorf("InactiveBorder = %q, want %q", theme.InactiveBorder, expected.InactiveBorder)
	}
	if theme.Brand != expected.Brand {
		t.Errorf("Brand = %q, want %q", theme.Brand, expected.Brand)
	}
	if theme.States[core.Idle] != expected.States[core.Idle] {
		t.Errorf("States[Idle] = %q, want %q", theme.States[core.Idle], expected.States[core.Idle])
	}
	if theme.Tools[core.ToolClaude] != expected.Tools[core.ToolClaude] {
		t.Errorf("Tools[ToolClaude] = %q, want %q", theme.Tools[core.ToolClaude], expected.Tools[core.ToolClaude])
	}
}

func TestResolveThemePresetSynthwavePeach(t *testing.T) {
	theme := ResolveTheme(config.ThemeConfig{Preset: "synthwave-peach"})

	expected := synthwavePeach()
	if theme.ActiveBorder != expected.ActiveBorder {
		t.Errorf("ActiveBorder = %q, want %q", theme.ActiveBorder, expected.ActiveBorder)
	}
	if theme.Brand != expected.Brand {
		t.Errorf("Brand = %q, want %q", theme.Brand, expected.Brand)
	}
	if theme.States[core.Thinking] != expected.States[core.Thinking] {
		t.Errorf("States[Thinking] = %q, want %q", theme.States[core.Thinking], expected.States[core.Thinking])
	}
	if theme.Tools[core.ToolCodex] != expected.Tools[core.ToolCodex] {
		t.Errorf("Tools[ToolCodex] = %q, want %q", theme.Tools[core.ToolCodex], expected.Tools[core.ToolCodex])
	}
}

func TestResolveThemePresetBioluminescent(t *testing.T) {
	theme := ResolveTheme(config.ThemeConfig{Preset: "bioluminescent"})

	expected := bioluminescent()
	if theme.ActiveBorder != expected.ActiveBorder {
		t.Errorf("ActiveBorder = %q, want %q", theme.ActiveBorder, expected.ActiveBorder)
	}
	if theme.Brand != expected.Brand {
		t.Errorf("Brand = %q, want %q", theme.Brand, expected.Brand)
	}
}

func TestResolveThemePresetDeepSeaGlow(t *testing.T) {
	theme := ResolveTheme(config.ThemeConfig{Preset: "deep-sea-glow"})

	expected := deepSeaGlow()
	if theme.ActiveBorder != expected.ActiveBorder {
		t.Errorf("ActiveBorder = %q, want %q", theme.ActiveBorder, expected.ActiveBorder)
	}
}

func TestResolveThemeUnknownPresetFallsBackToDeepSeaGlow(t *testing.T) {
	theme := ResolveTheme(config.ThemeConfig{Preset: "unknown-preset"})

	expected := deepSeaGlow()
	if theme.ActiveBorder != expected.ActiveBorder {
		t.Errorf("unknown preset should fall back to deep-sea-glow: ActiveBorder = %q, want %q",
			theme.ActiveBorder, expected.ActiveBorder)
	}
	if theme.Brand != expected.Brand {
		t.Errorf("unknown preset should fall back to deep-sea-glow: Brand = %q, want %q",
			theme.Brand, expected.Brand)
	}
}

func TestResolveThemeCustomOverrides(t *testing.T) {
	// Custom overrides should be applied on top of the base preset.
	cfg := config.ThemeConfig{
		Preset:       "synthwave-peach",
		ActiveBorder: "#AABBCC",
		Brand:        "#112233",
		States: map[string]string{
			"idle": "#FFFFFF",
		},
		Tools: map[string]string{
			"codex": "#123456",
		},
		GroupHeaders: map[string]string{
			"IDLE": "#FEDCBA",
		},
	}
	theme := ResolveTheme(cfg)

	if theme.ActiveBorder != lipgloss.Color("#AABBCC") {
		t.Errorf("ActiveBorder override = %q, want #AABBCC", theme.ActiveBorder)
	}
	if theme.Brand != lipgloss.Color("#112233") {
		t.Errorf("Brand override = %q, want #112233", theme.Brand)
	}
	if theme.States[core.Idle] != lipgloss.Color("#FFFFFF") {
		t.Errorf("States[Idle] override = %q, want #FFFFFF", theme.States[core.Idle])
	}
	if theme.Tools[core.ToolCodex] != lipgloss.Color("#123456") {
		t.Errorf("Tools[ToolCodex] override = %q, want #123456", theme.Tools[core.ToolCodex])
	}
	if theme.GroupHeaders["IDLE"] != lipgloss.Color("#FEDCBA") {
		t.Errorf("GroupHeaders[IDLE] override = %q, want #FEDCBA", theme.GroupHeaders["IDLE"])
	}

	// Non-overridden fields should come from the synthwave-peach preset.
	expected := synthwavePeach()
	if theme.InactiveBorder != expected.InactiveBorder {
		t.Errorf("InactiveBorder should remain from preset: got %q, want %q",
			theme.InactiveBorder, expected.InactiveBorder)
	}
}

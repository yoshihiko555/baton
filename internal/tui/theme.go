package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/yoshihiko555/baton/internal/config"
	"github.com/yoshihiko555/baton/internal/core"
)

// Theme holds all resolved color values for TUI rendering.
type Theme struct {
	ActiveBorder   lipgloss.Color
	InactiveBorder lipgloss.Color
	Brand          lipgloss.Color
	GroupHeaders   map[string]lipgloss.Color
	States         map[core.SessionState]lipgloss.Color
	Tools          map[core.ToolType]lipgloss.Color
}

// deepSeaGlow returns the default deep-sea-glow preset theme.
func deepSeaGlow() Theme {
	return Theme{
		ActiveBorder:   lipgloss.Color("#836FFF"),
		InactiveBorder: lipgloss.Color("#3D2A7A"),
		Brand:          lipgloss.Color("#836FFF"),
		GroupHeaders: map[string]lipgloss.Color{
			"WAITING": lipgloss.Color("#FF2DAA"),
			"ERROR":   lipgloss.Color("#FF4444"),
			"WORKING": lipgloss.Color("#15F5BA"),
			"IDLE":    lipgloss.Color("#836FFF"),
		},
		States: map[core.SessionState]lipgloss.Color{
			core.Idle:     lipgloss.Color("#836FFF"),
			core.Thinking: lipgloss.Color("#15F5BA"),
			core.ToolUse:  lipgloss.Color("#15F5BA"),
			core.Waiting:  lipgloss.Color("#FF2DAA"),
			core.Error:    lipgloss.Color("#FF4444"),
		},
		Tools: map[core.ToolType]lipgloss.Color{
			core.ToolClaude: lipgloss.Color("#F0F3FF"),
			core.ToolCodex:  lipgloss.Color("#15F5BA"),
			core.ToolGemini: lipgloss.Color("#836FFF"),
		},
	}
}

// synthwavePeach returns the synthwave-peach preset theme.
func synthwavePeach() Theme {
	return Theme{
		ActiveBorder:   lipgloss.Color("#FF6B9D"),
		InactiveBorder: lipgloss.Color("#4A2040"),
		Brand:          lipgloss.Color("#FF6B9D"),
		GroupHeaders: map[string]lipgloss.Color{
			"WAITING": lipgloss.Color("#FFD93D"),
			"ERROR":   lipgloss.Color("#FF4444"),
			"WORKING": lipgloss.Color("#C084FC"),
			"IDLE":    lipgloss.Color("#FF6B9D"),
		},
		States: map[core.SessionState]lipgloss.Color{
			core.Idle:     lipgloss.Color("#FF6B9D"),
			core.Thinking: lipgloss.Color("#C084FC"),
			core.ToolUse:  lipgloss.Color("#C084FC"),
			core.Waiting:  lipgloss.Color("#FFD93D"),
			core.Error:    lipgloss.Color("#FF4444"),
		},
		Tools: map[core.ToolType]lipgloss.Color{
			core.ToolClaude: lipgloss.Color("#FFF0F5"),
			core.ToolCodex:  lipgloss.Color("#C084FC"),
			core.ToolGemini: lipgloss.Color("#FF6B9D"),
		},
	}
}

// bioluminescent returns the bioluminescent preset theme.
func bioluminescent() Theme {
	return Theme{
		ActiveBorder:   lipgloss.Color("#00F5D4"),
		InactiveBorder: lipgloss.Color("#1A3A3A"),
		Brand:          lipgloss.Color("#00F5D4"),
		GroupHeaders: map[string]lipgloss.Color{
			"WAITING": lipgloss.Color("#FEE440"),
			"ERROR":   lipgloss.Color("#FF4444"),
			"WORKING": lipgloss.Color("#00BBF9"),
			"IDLE":    lipgloss.Color("#00F5D4"),
		},
		States: map[core.SessionState]lipgloss.Color{
			core.Idle:     lipgloss.Color("#00F5D4"),
			core.Thinking: lipgloss.Color("#00BBF9"),
			core.ToolUse:  lipgloss.Color("#00BBF9"),
			core.Waiting:  lipgloss.Color("#FEE440"),
			core.Error:    lipgloss.Color("#FF4444"),
		},
		Tools: map[core.ToolType]lipgloss.Color{
			core.ToolClaude: lipgloss.Color("#E0FFF0"),
			core.ToolCodex:  lipgloss.Color("#00BBF9"),
			core.ToolGemini: lipgloss.Color("#00F5D4"),
		},
	}
}

// stateKeyMap maps YAML state key strings to core.SessionState values.
var stateKeyMap = map[string]core.SessionState{
	"idle":     core.Idle,
	"thinking": core.Thinking,
	"tool_use": core.ToolUse,
	"waiting":  core.Waiting,
	"error":    core.Error,
}

// toolKeyMap maps YAML tool key strings to core.ToolType values.
var toolKeyMap = map[string]core.ToolType{
	"claude": core.ToolClaude,
	"codex":  core.ToolCodex,
	"gemini": core.ToolGemini,
}

// ResolveTheme resolves the final Theme from a ThemeConfig.
// It starts with deepSeaGlow as the base, applies the preset if specified,
// then applies any individual color overrides.
func ResolveTheme(cfg config.ThemeConfig) Theme {
	var theme Theme

	// 1. Start with deepSeaGlow as base
	theme = deepSeaGlow()

	// 2. Apply preset if specified
	if cfg.Preset != "" {
		switch cfg.Preset {
		case "synthwave-peach":
			theme = synthwavePeach()
		case "bioluminescent":
			theme = bioluminescent()
		case "deep-sea-glow":
			theme = deepSeaGlow()
		default:
			// Unknown preset: fall back to deep-sea-glow (already set)
		}
	}

	// 3. Apply individual overrides
	if cfg.ActiveBorder != "" {
		theme.ActiveBorder = lipgloss.Color(cfg.ActiveBorder)
	}
	if cfg.InactiveBorder != "" {
		theme.InactiveBorder = lipgloss.Color(cfg.InactiveBorder)
	}
	if cfg.Brand != "" {
		theme.Brand = lipgloss.Color(cfg.Brand)
	}
	if len(cfg.States) > 0 {
		if theme.States == nil {
			theme.States = make(map[core.SessionState]lipgloss.Color)
		}
		for k, v := range cfg.States {
			if state, ok := stateKeyMap[k]; ok {
				theme.States[state] = lipgloss.Color(v)
			}
		}
	}
	if len(cfg.Tools) > 0 {
		if theme.Tools == nil {
			theme.Tools = make(map[core.ToolType]lipgloss.Color)
		}
		for k, v := range cfg.Tools {
			if tool, ok := toolKeyMap[k]; ok {
				theme.Tools[tool] = lipgloss.Color(v)
			}
		}
	}
	if len(cfg.GroupHeaders) > 0 {
		if theme.GroupHeaders == nil {
			theme.GroupHeaders = make(map[string]lipgloss.Color)
		}
		for k, v := range cfg.GroupHeaders {
			theme.GroupHeaders[k] = lipgloss.Color(v)
		}
	}

	return theme
}

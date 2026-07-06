package tui

import (
	"strings"

	"github.com/yoshihiko555/baton/internal/autoreview"
	"github.com/yoshihiko555/baton/internal/config"
)

func newAutoReviewer(cfg config.AutoModeConfig) autoreview.Reviewer {
	if !cfg.Enabled {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Reviewer)) {
	case "", "codex":
		return autoreview.NewCodexReviewer(cfg.Model, cfg.Timeout)
	case "none", "off", "disabled":
		return nil
	default:
		return nil
	}
}

func autoReviewPolicy(cfg config.AutoModeConfig) autoreview.Policy {
	threshold := autoreview.NormalizeRisk(cfg.RiskThreshold)
	if threshold == autoreview.RiskUnknown {
		threshold = autoreview.RiskMedium
	}
	return autoreview.Policy{
		Enabled:       cfg.Enabled,
		RiskThreshold: threshold,
	}
}

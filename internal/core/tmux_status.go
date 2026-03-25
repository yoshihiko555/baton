package core

import (
	"fmt"
	"strings"
)

// BuildTMUXStatus は tmux status-line 向けの軽量サマリ文字列を生成する。
// Thinking + ToolUse は「作業中」として同一カウントで扱う。
func BuildTMUXStatus(projects []Project) string {
	thinking := 0
	waiting := 0
	idle := 0

	for _, project := range projects {
		for _, sess := range project.Sessions {
			if sess == nil {
				continue
			}
			switch sess.State {
			case Thinking, ToolUse:
				thinking++
			case Waiting:
				waiting++
			case Idle:
				idle++
			}
		}
	}

	if thinking == 0 && waiting == 0 && idle == 0 {
		return ""
	}

	parts := make([]string, 0, 3)
	if thinking > 0 {
		parts = append(parts, fmt.Sprintf("🤔%d", thinking))
	}
	if waiting > 0 {
		parts = append(parts, fmt.Sprintf("✋%d", waiting))
	}
	if idle > 0 {
		parts = append(parts, fmt.Sprintf("💤%d", idle))
	}

	return strings.Join(parts, " ")
}

package core

import (
	"fmt"
	"strings"
)

// BuildTMUXStatus は tmux status-line 向けの軽量サマリ文字列を生成する。
// Thinking + ToolUse は「作業中」として同一カウントで扱う。
func BuildTMUXStatus(projects []Project) string {
	return BuildTMUXStatusWithIcons(projects, nil)
}

// BuildTMUXStatusWithIcons は状態別のアイコン指定を受け取り、tmux status-line 向けの軽量サマリ文字列を生成する。
// icons は以下のキーを受け付ける: working/thinking/tool_use, waiting, idle。
func BuildTMUXStatusWithIcons(projects []Project, icons map[string]string) string {
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

	workingIcon := resolveTMUXStateIcon(icons, "🤔", "working", "thinking", "tool_use")
	waitingIcon := resolveTMUXStateIcon(icons, "✋", "waiting")
	idleIcon := resolveTMUXStateIcon(icons, "~", "idle")

	parts := make([]string, 0, 3)
	if thinking > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", workingIcon, thinking))
	}
	if waiting > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", waitingIcon, waiting))
	}
	if idle > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", idleIcon, idle))
	}

	return strings.Join(parts, " ")
}

func resolveTMUXStateIcon(icons map[string]string, fallback string, keys ...string) string {
	for _, key := range keys {
		if icon, ok := lookupTMUXStateIcon(icons, key); ok {
			return icon
		}
	}
	return fallback
}

func lookupTMUXStateIcon(icons map[string]string, key string) (string, bool) {
	if len(icons) == 0 {
		return "", false
	}
	if icon, ok := icons[key]; ok {
		return icon, true
	}
	for k, icon := range icons {
		if strings.EqualFold(k, key) {
			return icon, true
		}
	}
	return "", false
}

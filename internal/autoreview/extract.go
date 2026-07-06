package autoreview

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	bashCallPattern     = regexp.MustCompile(`(?is)\bBash\(([^)]{1,800})\)`)
	jsonCmdPattern      = regexp.MustCompile(`(?s)"cmd"\s*:\s*("(?:\\.|[^"\\]){1,1000}")`)
	commandLinePattern  = regexp.MustCompile(`(?im)^\s*(?:cmd|command|command to run|shell command|run command|run this command|コマンド)\s*:\s*(.+)$`)
	shellPromptPattern  = regexp.MustCompile(`(?m)^\s*(?:\$|❯|>|›)\s+(.+)$`)
	codeFencePattern    = regexp.MustCompile("(?s)```(?:bash|sh|shell)?\\s*(.*?)\\s*```")
	approvalChoiceLine  = regexp.MustCompile(`^\s*[›>❯]?\s*\d+\.\s+`)
	ansiEscapeSequences = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
)

func ExtractOperation(paneText string) Operation {
	text := strings.TrimSpace(ansiEscapeSequences.ReplaceAllString(paneText, ""))
	if text == "" {
		return Operation{Kind: "unknown"}
	}

	if match := bashCallPattern.FindStringSubmatch(text); len(match) == 2 {
		return operationFromCommand(match[1])
	}
	if match := jsonCmdPattern.FindStringSubmatch(text); len(match) == 2 {
		if command, err := strconv.Unquote(match[1]); err == nil {
			return operationFromCommand(command)
		}
	}
	if match := commandLinePattern.FindStringSubmatch(text); len(match) == 2 {
		return operationFromCommand(match[1])
	}
	if match := codeFencePattern.FindStringSubmatch(text); len(match) == 2 {
		command := strings.TrimSpace(match[1])
		if command != "" {
			return operationFromCommand(command)
		}
	}
	if command := lineAfterRunPrompt(text); command != "" {
		return operationFromCommand(command)
	}
	if match := shellPromptPattern.FindStringSubmatch(text); len(match) == 2 {
		return operationFromCommand(match[1])
	}

	return Operation{
		Kind:    "unknown",
		Summary: truncateForPrompt(text, 500),
	}
}

func operationFromCommand(command string) Operation {
	command = strings.TrimSpace(strings.Trim(command, "`"))
	if command == "" {
		return Operation{Kind: "unknown"}
	}
	return Operation{
		Kind:    "shell",
		Command: command,
		Summary: truncateForPrompt(command, 500),
	}
}

func lineAfterRunPrompt(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		normalized := strings.ToLower(strings.TrimSpace(line))
		if !strings.Contains(normalized, "run this command") && !strings.Contains(normalized, "run command") {
			continue
		}
		for _, candidate := range lines[i+1:] {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" || approvalChoiceLine.MatchString(candidate) {
				continue
			}
			if strings.EqualFold(candidate, "esc to cancel") {
				continue
			}
			return candidate
		}
	}
	return ""
}

func truncateForPrompt(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

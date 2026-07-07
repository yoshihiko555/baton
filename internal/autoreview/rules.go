package autoreview

import (
	"regexp"
	"strings"
)

var (
	dangerousPatterns = []struct {
		pattern *regexp.Regexp
		reason  string
		risk    Risk
	}{
		{regexp.MustCompile(`(?i)(^|\s)rm\s+`), "`rm` requires human confirmation", RiskHigh},
		{regexp.MustCompile(`(?i)\bgit\s+(reset|clean|push|branch\s+-D)\b`), "destructive or remote git operation requires human confirmation", RiskHigh},
		{regexp.MustCompile(`(?i)\b(curl|wget)\b.*(\|\s*(sh|bash)|https?://)`), "network command or pipe-to-shell requires human confirmation", RiskHigh},
		{regexp.MustCompile(`(?i)\b(npm|pnpm|yarn)\s+publish\b`), "package publishing requires human confirmation", RiskHigh},
		{regexp.MustCompile(`(?i)\b(kubectl|terraform|aws|gcloud|az)\b.*\b(apply|delete|destroy|deploy|prod|production)\b`), "infrastructure or production operation requires human confirmation", RiskHigh},
		{regexp.MustCompile(`(?i)\b(chmod\s+777|chown|sudo)\b`), "permission or privilege change requires human confirmation", RiskHigh},
		{regexp.MustCompile(`(?i)(^|[/\s])(\.env|\.ssh|\.aws)([/\s]|$)|\.(pem|key)\b|\b(secret|credential|token|cookie)\b`), "secret or credential related operation requires human confirmation", RiskCritical},
	}

	lowRiskPrefixes = []string{
		"go test",
		"go vet",
		"go build",
		"npm test",
		"npm run test",
		"npm run lint",
		"pnpm test",
		"pnpm run test",
		"pnpm run lint",
		"yarn test",
		"yarn lint",
		"cargo test",
		"swift test",
		"git status",
		"git diff",
		"git log",
		"rg ",
		"grep ",
		"ls",
		"pwd",
		"cat ",
		"sed -n",
		"awk ",
		"find . ",
	}
)

func DeterministicDecision(req Request) (Result, bool) {
	operation := req.Operation
	if operation.Kind == "" {
		operation = ExtractOperation(req.PaneText)
	}

	evidence := strings.TrimSpace(operation.Command + "\n" + operation.Summary)
	if evidence == "" {
		return Result{}, false
	}

	for _, item := range dangerousPatterns {
		if item.pattern.MatchString(evidence) {
			return StopResult(DecisionAsk, item.risk, SourceRule, item.reason), true
		}
	}

	if operation.Kind == "shell" && isLowRiskCommand(operation.Command) {
		return Result{
			Decision:   DecisionAllow,
			Risk:       RiskLow,
			Confidence: 1,
			Reason:     "routine low-risk local command",
			Source:     SourceRule,
		}, true
	}

	return Result{}, false
}

func isLowRiskCommand(command string) bool {
	normalized := normalizeCommand(command)
	for _, prefix := range lowRiskPrefixes {
		prefix = strings.TrimSpace(prefix)
		if normalized == prefix || strings.HasPrefix(normalized, prefix+" ") {
			return true
		}
	}
	return false
}

func normalizeCommand(command string) string {
	command = strings.TrimSpace(strings.Trim(command, "`"))
	command = strings.TrimPrefix(command, "$ ")
	command = strings.Join(strings.Fields(command), " ")
	return strings.ToLower(command)
}

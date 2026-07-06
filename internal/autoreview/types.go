package autoreview

import (
	"context"
	"fmt"
	"strings"
)

type Decision string

const (
	DecisionAllow   Decision = "allow"
	DecisionAsk     Decision = "ask"
	DecisionDeny    Decision = "deny"
	DecisionUnknown Decision = "unknown"
	DecisionError   Decision = "error"
)

type Risk string

const (
	RiskLow      Risk = "low"
	RiskMedium   Risk = "medium"
	RiskHigh     Risk = "high"
	RiskCritical Risk = "critical"
	RiskUnknown  Risk = "unknown"
)

type Source string

const (
	SourceRule     Source = "rule"
	SourceLLM      Source = "llm"
	SourceFallback Source = "fallback"
)

type Operation struct {
	Kind    string
	Command string
	Summary string
}

type Request struct {
	Tool        string
	PaneID      string
	ProjectName string
	ProjectPath string
	PaneText    string
	Operation   Operation
}

type Result struct {
	Decision   Decision `json:"decision"`
	Risk       Risk     `json:"risk"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
	Source     Source   `json:"source"`
}

type Reviewer interface {
	Review(ctx context.Context, req Request) (Result, error)
}

func (r Result) AllowsAutoApprove(threshold Risk) bool {
	return r.Decision == DecisionAllow && compareRisk(r.Risk, threshold) <= 0
}

func (r Result) Label() string {
	switch r.Decision {
	case DecisionAllow:
		return "ALLOW"
	case DecisionAsk:
		return "ASK"
	case DecisionDeny:
		return "DENY"
	case DecisionError:
		return "ERR"
	default:
		return "UNKNOWN"
	}
}

func (r Result) Badge() string {
	parts := []string{r.Label()}
	if r.Source != "" {
		parts = append(parts, strings.ToUpper(string(r.Source)))
	}
	if r.Risk != "" {
		parts = append(parts, strings.ToUpper(string(r.Risk)))
	}
	return strings.Join(parts, ":")
}

func (r Result) Trace() string {
	source := string(r.Source)
	if source == "" {
		source = "unknown"
	}
	risk := string(r.Risk)
	if risk == "" {
		risk = "unknown"
	}
	return source + "/" + risk
}

func (r Result) ShortReason() string {
	reason := strings.TrimSpace(r.Reason)
	if reason == "" {
		return fmt.Sprintf("%s/%s", r.Decision, r.Risk)
	}
	if len([]rune(reason)) > 100 {
		return string([]rune(reason)[:100]) + "..."
	}
	return reason
}

func StopResult(decision Decision, risk Risk, source Source, reason string) Result {
	return Result{
		Decision:   decision,
		Risk:       risk,
		Confidence: 1,
		Reason:     reason,
		Source:     source,
	}
}

func ErrorResult(err error) Result {
	return ErrorResultWithSource(err, SourceFallback)
}

func ErrorResultWithSource(err error, source Source) Result {
	reason := "auto-review failed"
	if err != nil {
		reason = err.Error()
	}
	return Result{
		Decision:   DecisionError,
		Risk:       RiskUnknown,
		Confidence: 0,
		Reason:     reason,
		Source:     source,
	}
}

func NormalizeDecision(value string) Decision {
	switch Decision(strings.ToLower(strings.TrimSpace(value))) {
	case DecisionAllow:
		return DecisionAllow
	case DecisionAsk:
		return DecisionAsk
	case DecisionDeny:
		return DecisionDeny
	case DecisionError:
		return DecisionError
	default:
		return DecisionUnknown
	}
}

func NormalizeRisk(value string) Risk {
	switch Risk(strings.ToLower(strings.TrimSpace(value))) {
	case RiskLow:
		return RiskLow
	case RiskMedium:
		return RiskMedium
	case RiskHigh:
		return RiskHigh
	case RiskCritical:
		return RiskCritical
	default:
		return RiskUnknown
	}
}

func compareRisk(left, right Risk) int {
	return riskRank(left) - riskRank(right)
}

func riskRank(risk Risk) int {
	switch risk {
	case RiskLow:
		return 0
	case RiskMedium:
		return 1
	case RiskHigh:
		return 2
	case RiskCritical:
		return 3
	default:
		return 4
	}
}

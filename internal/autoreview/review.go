package autoreview

import (
	"context"
	"fmt"
)

type Policy struct {
	Enabled       bool
	RiskThreshold Risk
}

func Evaluate(ctx context.Context, policy Policy, reviewer Reviewer, req Request) Result {
	if !policy.Enabled {
		return StopResult(DecisionAsk, RiskUnknown, SourceFallback, "auto mode is disabled")
	}

	if req.Operation.Kind == "" {
		req.Operation = ExtractOperation(req.PaneText)
	}

	if result, ok := DeterministicDecision(req); ok {
		if result.AllowsAutoApprove(policy.RiskThreshold) {
			return result
		}
		if result.Decision == DecisionAllow {
			return StopResult(DecisionAsk, result.Risk, SourceRule, fmt.Sprintf("risk %s exceeds threshold %s", result.Risk, policy.RiskThreshold))
		}
		return result
	}

	if reviewer == nil {
		return StopResult(DecisionAsk, RiskUnknown, SourceFallback, "no reviewer configured for ambiguous approval")
	}

	result, err := reviewer.Review(ctx, req)
	if err != nil {
		return ErrorResultWithSource(err, SourceLLM)
	}
	result.Decision = NormalizeDecision(string(result.Decision))
	result.Risk = NormalizeRisk(string(result.Risk))
	if result.Source == "" {
		result.Source = SourceLLM
	}
	if result.Reason == "" {
		result.Reason = "reviewer returned no reason"
	}
	if !result.AllowsAutoApprove(policy.RiskThreshold) && result.Decision == DecisionAllow {
		result.Decision = DecisionAsk
		result.Reason = fmt.Sprintf("reviewer risk %s exceeds threshold %s", result.Risk, policy.RiskThreshold)
	}
	return result
}

package autoreview

import "testing"

func TestExtractOperationFromRunCommandPrompt(t *testing.T) {
	text := "Run this command?\n\ngo test ./...\n\n› 1. Yes, allow once\n  2. No"

	got := ExtractOperation(text)

	if got.Kind != "shell" {
		t.Fatalf("Kind = %q, want shell", got.Kind)
	}
	if got.Command != "go test ./..." {
		t.Fatalf("Command = %q, want go test ./...", got.Command)
	}
}

func TestExtractOperationFromCodexToolJSON(t *testing.T) {
	text := `{"cmd":"pwd","workdir":"/repo","sandbox_permissions":"require_escalated","justification":"test"}`

	got := ExtractOperation(text)

	if got.Kind != "shell" {
		t.Fatalf("Kind = %q, want shell", got.Kind)
	}
	if got.Command != "pwd" {
		t.Fatalf("Command = %q, want pwd", got.Command)
	}
}

func TestExtractOperationFromCmdLine(t *testing.T) {
	text := "cmd: pwd\n\n› 1. Yes\n  2. No"

	got := ExtractOperation(text)

	if got.Kind != "shell" {
		t.Fatalf("Kind = %q, want shell", got.Kind)
	}
	if got.Command != "pwd" {
		t.Fatalf("Command = %q, want pwd", got.Command)
	}
}

func TestDeterministicDecisionAllowsLowRiskCommand(t *testing.T) {
	req := Request{Operation: Operation{Kind: "shell", Command: "go test ./..."}}

	got, ok := DeterministicDecision(req)

	if !ok {
		t.Fatal("expected deterministic decision")
	}
	if got.Decision != DecisionAllow {
		t.Fatalf("Decision = %q, want allow", got.Decision)
	}
	if got.Risk != RiskLow {
		t.Fatalf("Risk = %q, want low", got.Risk)
	}
}

func TestDeterministicDecisionStopsDangerousCommand(t *testing.T) {
	req := Request{Operation: Operation{Kind: "shell", Command: "rm -rf ./tmp"}}

	got, ok := DeterministicDecision(req)

	if !ok {
		t.Fatal("expected deterministic decision")
	}
	if got.Decision != DecisionAsk {
		t.Fatalf("Decision = %q, want ask", got.Decision)
	}
	if got.Risk != RiskHigh {
		t.Fatalf("Risk = %q, want high", got.Risk)
	}
}

func TestParseCodexResult(t *testing.T) {
	output := []byte("analysis...\n{\"decision\":\"allow\",\"risk\":\"medium\",\"confidence\":0.8,\"reason\":\"bounded test command\"}\n")

	got, err := parseCodexResult(output)
	if err != nil {
		t.Fatalf("parseCodexResult returned error: %v", err)
	}

	if got.Decision != DecisionAllow {
		t.Fatalf("Decision = %q, want allow", got.Decision)
	}
	if got.Risk != RiskMedium {
		t.Fatalf("Risk = %q, want medium", got.Risk)
	}
	if got.Confidence != 0.8 {
		t.Fatalf("Confidence = %v, want 0.8", got.Confidence)
	}
}

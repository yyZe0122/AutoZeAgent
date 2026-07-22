package policy

import (
	"errors"
	"testing"
)

func TestDefaultPolicyAllowsOnlyPureReadsWithoutApproval(t *testing.T) {
	evaluator := NewEvaluator(DefaultConfig())

	if err := evaluator.Authorize(RiskR0, false); err != nil {
		t.Fatalf("authorize R0: %v", err)
	}
	for _, level := range []RiskLevel{RiskR1, RiskR2, RiskR3, RiskR4} {
		if err := evaluator.Authorize(level, false); !errors.Is(err, ErrApprovalRequired) {
			t.Fatalf("authorize %s without approval = %v, want approval required", level, err)
		}
		if err := evaluator.Authorize(level, true); err != nil {
			t.Fatalf("authorize %s with approval: %v", level, err)
		}
	}
}

func TestPolicyFailsClosedForMissingInvalidAndUnknownRules(t *testing.T) {
	evaluator := NewEvaluator(Config{Rules: map[RiskLevel]Action{
		RiskR0: "unexpected",
	}})

	for _, level := range []RiskLevel{RiskR0, RiskR1, RiskLevel("R9")} {
		result := evaluator.Evaluate(level)
		if result.Allowed || result.RequiresApproval || result.Action != ActionDeny {
			t.Fatalf("evaluate %s = %#v, want fail-closed deny", level, result)
		}
		if err := evaluator.Authorize(level, true); !errors.Is(err, ErrDenied) {
			t.Fatalf("authorize %s = %v, want denied", level, err)
		}
	}
}

func TestPolicyCanBeConfiguredPerRiskLevel(t *testing.T) {
	evaluator := NewEvaluator(Config{Rules: map[RiskLevel]Action{
		RiskR0: ActionDeny,
		RiskR1: ActionAllow,
		RiskR4: ActionRequireApproval,
	}})

	if err := evaluator.Authorize(RiskR1, false); err != nil {
		t.Fatalf("custom allow: %v", err)
	}
	if err := evaluator.Authorize(RiskR0, true); !errors.Is(err, ErrDenied) {
		t.Fatalf("custom deny = %v", err)
	}
	if err := evaluator.Authorize(RiskR4, false); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("custom approval = %v", err)
	}
}

func TestAtLeastFailsClosed(t *testing.T) {
	if !AtLeast(RiskR2, RiskR1) {
		t.Fatal("R2 should satisfy R1")
	}
	if AtLeast(RiskR0, RiskR1) {
		t.Fatal("R0 should not satisfy R1")
	}
	if AtLeast("R9", RiskR0) || AtLeast(RiskR0, "R9") {
		t.Fatal("unknown risk must fail closed")
	}
}

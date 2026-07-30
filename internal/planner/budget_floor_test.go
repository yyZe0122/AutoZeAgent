package planner

import (
	"strings"
	"testing"

	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/policy"
)

func TestToPlanRejectsTinyBudget(t *testing.T) {
	p, err := New(Config{Provider: &scriptedProvider{}, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.toPlan(structuredGenerateRequest(), planOutput{
		Objective: "x",
		Budget:    budgetOutput{MaxTokens: 100, MaxDurationMillis: 1000},
		Steps: []stepOutput{{
			Title: "s", Risk: policy.RiskR0, Rollback: "none", TimeoutMillis: 500,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid plan budget") {
		t.Fatalf("error = %v, want invalid plan budget floor", err)
	}
}

func plannerWithProcessExec(t *testing.T) *Planner {
	t.Helper()
	caps := DefaultReadOnlyCapabilities()
	caps["process_exec"] = policy.RiskR2
	p, err := New(Config{
		Provider: &scriptedProvider{}, Model: "test",
		AllowedCapabilities: caps,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestToPlanRejectsProcessExecWithoutPath(t *testing.T) {
	p := plannerWithProcessExec(t)
	_, err := p.toPlan(structuredGenerateRequest(), planOutput{
		Objective: "run echo",
		Budget:    budgetOutput{MaxTokens: 4096, MaxDurationMillis: 120_000},
		Steps: []stepOutput{{
			Title: "exec", Risk: policy.RiskR2, Rollback: "none", TimeoutMillis: 30_000,
			ExpectedSideEffects: []string{"run echo"},
			Capabilities: []approval.CapabilityScope{{
				Capability: "process_exec", Command: "echo", Arguments: []string{"hello"},
				MaxDurationMillis: 30_000, MaxCalls: 1, OneTime: true,
			}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "process_exec") {
		t.Fatalf("error = %v, want process_exec path requirement", err)
	}
}

func TestToPlanAcceptsProcessExecWithAbsolutePath(t *testing.T) {
	p := plannerWithProcessExec(t)
	plan, err := p.toPlan(structuredGenerateRequest(), planOutput{
		Objective: "run echo",
		Budget:    budgetOutput{MaxTokens: 4096, MaxDurationMillis: 120_000},
		Steps: []stepOutput{{
			Title: "exec", Risk: policy.RiskR2, Rollback: "none", TimeoutMillis: 30_000,
			ExpectedSideEffects: []string{"run echo"},
			Capabilities: []approval.CapabilityScope{{
				Capability: "process_exec", Command: "echo", Paths: []string{"/tmp/ws"},
				Arguments: []string{"hello"}, MaxDurationMillis: 30_000, MaxCalls: 1, OneTime: true,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || len(plan.Steps[0].Capabilities) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Steps[0].Capabilities[0].Paths[0] != "/tmp/ws" {
		t.Fatalf("paths = %v", plan.Steps[0].Capabilities[0].Paths)
	}
}

package approvalsubmission

import (
	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/policy"
	"context"
	"fmt"
	"strings"
	"time"
)

type Action string

const (
	ActionAllowOnce      Action = "allow_once"
	ActionAllowLimited   Action = "allow_limited"
	ActionAllowPlan      Action = "allow_plan"
	ActionReject         Action = "reject"
	ActionRequestChanges Action = "request_changes"
)

type PromptRequest struct {
	PlanID kernel.PlanID
	StepID kernel.StepID
}

type Prompt struct {
	PlanID    kernel.PlanID  `json:"plan_id"`
	TaskID    kernel.TaskID  `json:"task_id"`
	Revision  uint64         `json:"plan_revision"`
	PlanHash  string         `json:"plan_hash"`
	Objective string         `json:"objective"`
	Budget    PromptBudget   `json:"budget"`
	Steps     []PromptStep   `json:"steps"`
	Actions   []ActionOption `json:"actions"`
}

type PromptBudget struct {
	MaxTokens         int64 `json:"max_tokens"`
	MaxCostMicros     int64 `json:"max_cost_micros"`
	MaxDurationMillis int64 `json:"max_duration_ms"`
}

type PromptStep struct {
	StepID              kernel.StepID      `json:"step_id"`
	Position            int                `json:"position"`
	Title               string             `json:"title"`
	Risk                policy.RiskLevel   `json:"risk"`
	ExpectedSideEffects []string           `json:"expected_side_effects"`
	Rollback            string             `json:"rollback"`
	TimeoutMillis       int64              `json:"timeout_ms"`
	Capabilities        []PromptCapability `json:"capabilities"`
}

type PromptCapability struct {
	Tool              string   `json:"tool"`
	Paths             []string `json:"paths"`
	Command           string   `json:"command,omitempty"`
	Arguments         []string `json:"arguments"`
	NetworkDomains    []string `json:"network_domains"`
	MaxDurationMillis int64    `json:"max_duration_ms"`
	MaxCalls          uint64   `json:"max_calls"`
	OneTime           bool     `json:"one_time"`
}

type ActionOption struct {
	Action      Action             `json:"action"`
	Scope       approval.ScopeType `json:"scope"`
	StepID      kernel.StepID      `json:"step_id,omitempty"`
	Description string             `json:"description"`
}

type ActionRequest struct {
	ApprovalID approval.ApprovalID
	PlanID     kernel.PlanID
	Revision   uint64
	PlanHash   string
	StepID     kernel.StepID
	Action     Action
	DecidedBy  string
	Reason     string
	ExpiresAt  *time.Time
}

func (s *Service) Prompt(ctx context.Context, request PromptRequest) (Prompt, error) {
	if ctx == nil {
		return Prompt{}, fmt.Errorf("approval prompt context is required")
	}
	request.PlanID = kernel.PlanID(strings.TrimSpace(string(request.PlanID)))
	request.StepID = kernel.StepID(strings.TrimSpace(string(request.StepID)))
	if request.PlanID == "" {
		return Prompt{}, invalidInteraction("plan ID is required")
	}
	stored, err := s.plans.LoadPlanDocument(ctx, request.PlanID)
	if err != nil {
		return Prompt{}, classifyError(err)
	}
	plan, err := validateStoredPlan(request.PlanID, stored)
	if err != nil {
		return Prompt{}, err
	}
	if err := requireWaitingApproval(stored); err != nil {
		return Prompt{}, err
	}

	steps := plan.Steps
	var selected *approval.StepScope
	if request.StepID != "" {
		step, ok := findStep(plan, request.StepID)
		if !ok {
			return Prompt{}, invalidInteraction("step is not present in the canonical plan")
		}
		selected = &step
		steps = []approval.StepScope{step}
	}
	return buildPrompt(plan, stored.Hash, steps, selected), nil
}

func (s *Service) Act(ctx context.Context, request ActionRequest) (approval.Approval, error) {
	if ctx == nil {
		return approval.Approval{}, fmt.Errorf("approval submission context is required")
	}
	request.PlanID = kernel.PlanID(strings.TrimSpace(string(request.PlanID)))
	request.StepID = kernel.StepID(strings.TrimSpace(string(request.StepID)))
	if request.PlanID == "" || request.Revision == 0 || strings.TrimSpace(request.PlanHash) == "" {
		return approval.Approval{}, invalidInteraction("plan ID, revision, and hash are required")
	}
	stored, err := s.plans.LoadPlanDocument(ctx, request.PlanID)
	if err != nil {
		return approval.Approval{}, classifyError(err)
	}
	plan, err := validateStoredPlan(request.PlanID, stored)
	if err != nil {
		return approval.Approval{}, err
	}
	if err := requireWaitingApproval(stored); err != nil {
		return approval.Approval{}, err
	}
	if request.Revision != stored.Revision || strings.TrimSpace(request.PlanHash) != stored.Hash {
		return approval.Approval{}, classifyError(fmt.Errorf("%w: stored plan revision or hash differs", ErrPlanChanged))
	}

	scope, stepID, decision, err := resolveAction(plan, request.Action, request.StepID)
	if err != nil {
		return approval.Approval{}, err
	}
	return s.Decide(ctx, Request{
		ApprovalID: request.ApprovalID,
		PlanID:     request.PlanID, Revision: request.Revision, PlanHash: stored.Hash,
		Scope: scope, StepID: stepID, Decision: decision,
		DecidedBy: request.DecidedBy, Reason: request.Reason, ExpiresAt: request.ExpiresAt,
	})
}

func resolveAction(plan approval.PlanDocument, action Action, requestedStep kernel.StepID) (approval.ScopeType, kernel.StepID, approval.Decision, error) {
	var step approval.StepScope
	if requestedStep != "" {
		var ok bool
		step, ok = findStep(plan, requestedStep)
		if !ok {
			return "", "", "", invalidInteraction("step is not present in the canonical plan")
		}
	}

	switch action {
	case ActionAllowOnce:
		if requestedStep == "" || !allowsExactlyOneCall(step) {
			return "", "", "", invalidInteraction("allow_once requires one canonical one-time capability in the selected step")
		}
		return approval.ScopeStep, requestedStep, approval.DecisionApproved, nil
	case ActionAllowLimited:
		if requestedStep == "" || len(step.Capabilities) == 0 {
			return "", "", "", invalidInteraction("allow_limited requires a selected step with canonical capability limits")
		}
		return approval.ScopeStep, requestedStep, approval.DecisionApproved, nil
	case ActionAllowPlan:
		return approval.ScopePlan, "", approval.DecisionApproved, nil
	case ActionReject:
		if requestedStep != "" {
			return approval.ScopeStep, requestedStep, approval.DecisionRejected, nil
		}
		return approval.ScopePlan, "", approval.DecisionRejected, nil
	case ActionRequestChanges:
		if requestedStep != "" {
			return approval.ScopeStep, requestedStep, approval.DecisionChangesRequested, nil
		}
		return approval.ScopePlan, "", approval.DecisionChangesRequested, nil
	default:
		return "", "", "", invalidInteraction(fmt.Sprintf("unknown action %q", action))
	}
}

func buildPrompt(plan approval.PlanDocument, hash string, steps []approval.StepScope, selected *approval.StepScope) Prompt {
	promptSteps := make([]PromptStep, 0, len(steps))
	for _, step := range steps {
		capabilities := make([]PromptCapability, 0, len(step.Capabilities))
		for _, capability := range step.Capabilities {
			capabilities = append(capabilities, PromptCapability{
				Tool: capability.Capability, Paths: append([]string(nil), capability.Paths...),
				Command: capability.Command, Arguments: append([]string(nil), capability.Arguments...),
				NetworkDomains:    append([]string(nil), capability.NetworkDomains...),
				MaxDurationMillis: capability.MaxDurationMillis, MaxCalls: capability.MaxCalls,
				OneTime: capability.OneTime,
			})
		}
		promptSteps = append(promptSteps, PromptStep{
			StepID: step.StepID, Position: step.Position, Title: step.Title, Risk: step.Risk,
			ExpectedSideEffects: append([]string(nil), step.ExpectedSideEffects...),
			Rollback:            step.Rollback, TimeoutMillis: step.TimeoutMillis, Capabilities: capabilities,
		})
	}
	return Prompt{
		PlanID: plan.PlanID, TaskID: plan.TaskID, Revision: plan.Revision, PlanHash: hash,
		Objective: plan.Objective,
		Budget: PromptBudget{
			MaxTokens: plan.Budget.MaxTokens, MaxCostMicros: plan.Budget.MaxCostMicros,
			MaxDurationMillis: plan.Budget.MaxDurationMillis,
		},
		Steps: promptSteps, Actions: promptActions(selected),
	}
}

func promptActions(selected *approval.StepScope) []ActionOption {
	if selected == nil {
		return []ActionOption{
			{Action: ActionAllowPlan, Scope: approval.ScopePlan, Description: "Allow the canonical capabilities for the current Plan revision."},
			{Action: ActionReject, Scope: approval.ScopePlan, Description: "Reject the current Plan revision."},
			{Action: ActionRequestChanges, Scope: approval.ScopePlan, Description: "Request changes to the current Plan revision."},
		}
	}
	actions := make([]ActionOption, 0, 5)
	if allowsExactlyOneCall(*selected) {
		actions = append(actions, ActionOption{
			Action: ActionAllowOnce, Scope: approval.ScopeStep, StepID: selected.StepID,
			Description: "Allow the selected step's single canonical capability for one call.",
		})
	}
	if len(selected.Capabilities) > 0 {
		actions = append(actions, ActionOption{
			Action: ActionAllowLimited, Scope: approval.ScopeStep, StepID: selected.StepID,
			Description: "Allow the selected step using only the canonical per-capability call limits.",
		})
	}
	return append(actions,
		ActionOption{Action: ActionAllowPlan, Scope: approval.ScopePlan, Description: "Allow the canonical capabilities for the current Plan revision."},
		ActionOption{Action: ActionReject, Scope: approval.ScopeStep, StepID: selected.StepID, Description: "Reject the selected step."},
		ActionOption{Action: ActionRequestChanges, Scope: approval.ScopeStep, StepID: selected.StepID, Description: "Request changes to the selected step."},
	)
}

func allowsExactlyOneCall(step approval.StepScope) bool {
	return len(step.Capabilities) == 1 && step.Capabilities[0].OneTime && step.Capabilities[0].MaxCalls == 1
}

func findStep(plan approval.PlanDocument, id kernel.StepID) (approval.StepScope, bool) {
	for _, step := range plan.Steps {
		if step.StepID == id {
			return step, true
		}
	}
	return approval.StepScope{}, false
}

func invalidInteraction(message string) error {
	return classifyError(fmt.Errorf("%w: %s", ErrInvalidRequest, message))
}

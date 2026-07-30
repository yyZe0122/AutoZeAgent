package gatewayclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type ApprovalDecisionRequest struct {
	PlanID       PlanID `json:"plan_id"`
	PlanRevision uint64 `json:"plan_revision"`
	PlanHash     string `json:"plan_hash"`
	StepID       StepID `json:"step_id,omitempty"`
	Action       Action `json:"action"`
	DecidedBy    string `json:"decided_by"`
	Reason       string `json:"reason"`
}

func (c *Client) ApprovalPrompt(ctx context.Context, planID PlanID, stepID StepID) (Prompt, error) {
	path := "/v1/approvals/prompt?plan_id=" + url.QueryEscape(string(planID))
	if stepID != "" {
		path += "&step_id=" + url.QueryEscape(string(stepID))
	}
	var prompt Prompt
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &prompt); err != nil {
		return Prompt{}, fmt.Errorf("load approval prompt: %w", err)
	}
	return prompt, nil
}

func (c *Client) DecideApproval(ctx context.Context, prompt Prompt, stepID StepID, action Action, decidedBy, reason string) (Approval, error) {
	var decision Approval
	if err := c.inner.DoJSON(ctx, http.MethodPost, "/v1/approvals", ApprovalDecisionRequest{
		PlanID: prompt.PlanID, PlanRevision: prompt.Revision, PlanHash: prompt.PlanHash,
		StepID: stepID, Action: action, DecidedBy: strings.TrimSpace(decidedBy), Reason: strings.TrimSpace(reason),
	}, &decision); err != nil {
		return Approval{}, fmt.Errorf("submit approval decision: %w", err)
	}
	return decision, nil
}

func PromptAllows(prompt Prompt, action Action, stepID StepID) bool {
	for _, option := range prompt.Actions {
		if option.Action == action && option.StepID == stepID {
			return true
		}
	}
	return false
}

func ValidApprovalAction(action Action) bool {
	switch action {
	case ActionAllowOnce, ActionAllowLimited, ActionAllowPlan, ActionReject, ActionRequestChanges:
		return true
	default:
		return false
	}
}

// Package agentautostart auto-approves and starts runs when a task is in
// execution_mode=agent after planning completes. plan mode still stops at
// waiting_approval for explicit user approval.
package agentautostart

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/approvalsubmission"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/runexecution"
)

// Planner is the planning surface used by task submission and recovery.
type Planner interface {
	PlanTask(context.Context, kernel.Task, kernel.PlanID, uint64) (kernel.Task, approval.PlanDocument, error)
}

// Approver records an allow_plan decision.
type Approver interface {
	Act(context.Context, approvalsubmission.ActionRequest) (approval.Approval, error)
}

// Runner starts plan steps after approval.
type Runner interface {
	Start(context.Context, runexecution.StartRequest) (runexecution.StartResult, error)
}

// Wrapper plans then, for agent-mode tasks only, allow_plan + Start.
type Wrapper struct {
	inner    Planner
	approver Approver
	runner   Runner
}

// Wrap returns a Planner that auto-starts agent-mode tasks after a successful plan.
// If approver or runner is nil, it behaves like inner only.
func Wrap(inner Planner, approver Approver, runner Runner) *Wrapper {
	return &Wrapper{inner: inner, approver: approver, runner: runner}
}

func (w *Wrapper) PlanTask(ctx context.Context, task kernel.Task, planID kernel.PlanID, revision uint64) (kernel.Task, approval.PlanDocument, error) {
	if w == nil || w.inner == nil {
		return task, approval.PlanDocument{}, errors.New("agentautostart planner is required")
	}
	planned, plan, err := w.inner.PlanTask(ctx, task, planID, revision)
	if err != nil {
		return planned, plan, err
	}
	if w.approver == nil || w.runner == nil {
		return planned, plan, nil
	}
	mode := planned.ExecutionMode
	if mode == "" {
		mode = task.ExecutionMode
	}
	mode = kernel.NormalizeExecutionMode(string(mode))
	if mode == kernel.ExecutionModePlan {
		return planned, plan, nil
	}
	if err := w.autoApproveAndStart(ctx, planned, plan); err != nil {
		// Plan is already durable; surface start failure in logs only so recovery
		// can leave the task at waiting_approval for manual a/r.
		slog.Error("agent mode auto-start failed",
			"component", "agentautostart", "operation", "auto_start", "result", "failed",
			"task_id", planned.ID, "plan_id", plan.PlanID, "error", err,
		)
	}
	return planned, plan, nil
}

func (w *Wrapper) autoApproveAndStart(ctx context.Context, task kernel.Task, plan approval.PlanDocument) error {
	hash, err := plan.Hash()
	if err != nil {
		return err
	}
	hash = strings.TrimSpace(hash)
	if _, err := w.approver.Act(ctx, approvalsubmission.ActionRequest{
		PlanID:    plan.PlanID,
		Revision:  plan.Revision,
		PlanHash:  hash,
		Action:    approvalsubmission.ActionAllowPlan,
		DecidedBy: "autozeagent-agent-mode",
		Reason:    "agent mode auto-approve",
	}); err != nil {
		return err
	}
	_, err = w.runner.Start(ctx, runexecution.StartRequest{
		TaskID:       task.ID,
		PlanID:       plan.PlanID,
		PlanRevision: plan.Revision,
		PlanHash:     hash,
		Actor:        "autozeagent-agent-mode",
		TraceID:      string(plan.PlanID),
	})
	return err
}

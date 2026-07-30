package agentautostart

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/approvalsubmission"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/runexecution"
)

type fakePlanner struct {
	task kernel.Task
	plan approval.PlanDocument
	err  error
}

func (f *fakePlanner) PlanTask(context.Context, kernel.Task, kernel.PlanID, uint64) (kernel.Task, approval.PlanDocument, error) {
	return f.task, f.plan, f.err
}

type fakeApprover struct {
	calls atomic.Int32
	err   error
}

func (f *fakeApprover) Act(context.Context, approvalsubmission.ActionRequest) (approval.Approval, error) {
	f.calls.Add(1)
	if f.err != nil {
		return approval.Approval{}, f.err
	}
	return approval.Approval{}, nil
}

type fakeRunner struct {
	calls atomic.Int32
	err   error
}

func (f *fakeRunner) Start(context.Context, runexecution.StartRequest) (runexecution.StartResult, error) {
	f.calls.Add(1)
	if f.err != nil {
		return runexecution.StartResult{}, f.err
	}
	return runexecution.StartResult{}, nil
}

func samplePlan(mode kernel.ExecutionMode) (kernel.Task, approval.PlanDocument) {
	task := kernel.Task{
		ID: "task-1", SessionID: "session-1", Title: "t", Objective: "do it",
		State: kernel.TaskWaitingApproval, ExecutionMode: mode, Version: 2,
	}
	plan := approval.PlanDocument{
		PlanID: "plan-1", TaskID: "task-1", Revision: 1, Objective: "do it",
		Steps: []approval.StepScope{{
			StepID: "step-1", Position: 0, Title: "work", Risk: "R0",
		}},
	}
	return task, plan
}

func TestAgentModeAutoStarts(t *testing.T) {
	task, plan := samplePlan(kernel.ExecutionModeAgent)
	// Hash needs valid canonical plan — use empty steps risk carefully.
	// PlanDocument.Hash requires valid steps; fill minimal.
	approver := &fakeApprover{}
	runner := &fakeRunner{}
	w := Wrap(&fakePlanner{task: task, plan: plan}, approver, runner)
	_, _, err := w.PlanTask(context.Background(), task, plan.PlanID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if approver.calls.Load() != 1 {
		t.Fatalf("approve calls = %d", approver.calls.Load())
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("start calls = %d", runner.calls.Load())
	}
}

func TestPlanModeDoesNotAutoStart(t *testing.T) {
	task, plan := samplePlan(kernel.ExecutionModePlan)
	approver := &fakeApprover{}
	runner := &fakeRunner{}
	w := Wrap(&fakePlanner{task: task, plan: plan}, approver, runner)
	_, _, err := w.PlanTask(context.Background(), task, plan.PlanID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if approver.calls.Load() != 0 || runner.calls.Load() != 0 {
		t.Fatalf("plan mode must not auto-start: approve=%d start=%d",
			approver.calls.Load(), runner.calls.Load())
	}
}

func TestPlanErrorSkipsAutoStart(t *testing.T) {
	task, plan := samplePlan(kernel.ExecutionModeAgent)
	approver := &fakeApprover{}
	runner := &fakeRunner{}
	w := Wrap(&fakePlanner{task: task, plan: plan, err: errors.New("provider down")}, approver, runner)
	_, _, err := w.PlanTask(context.Background(), task, plan.PlanID, 1)
	if err == nil {
		t.Fatal("expected plan error")
	}
	if approver.calls.Load() != 0 || runner.calls.Load() != 0 {
		t.Fatal("must not auto-start on plan failure")
	}
}

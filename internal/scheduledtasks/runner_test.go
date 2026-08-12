package scheduledtasks

import (
	"context"
	"errors"
	"testing"

	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	"github.com/yyZe0122/yunmengze-agent/internal/tasksubmission"
	"github.com/yyZe0122/yunmengze-agent/pkg/schedulerapi"
)

type claimClient struct {
	tasks []schedulerapi.TaskRequest
	acks  []schedulerapi.AcknowledgeRequest
	err   error
}

func (c *claimClient) ClaimScheduledTasks(context.Context, schedulerapi.ClaimDueRequest) ([]schedulerapi.TaskRequest, error) {
	return c.tasks, c.err
}

func (c *claimClient) AcknowledgeScheduledTask(_ context.Context, request schedulerapi.AcknowledgeRequest) error {
	c.acks = append(c.acks, request)
	return nil
}

type submitStub struct {
	last tasksubmission.Request
	err  error
}

func (s *submitStub) Submit(_ context.Context, request tasksubmission.Request) (tasksubmission.Result, error) {
	s.last = request
	if s.err != nil {
		return tasksubmission.Result{}, s.err
	}
	return tasksubmission.Result{Task: kernel.Task{ID: request.TaskID, State: kernel.TaskRunning, ExecutionMode: request.ExecutionMode}}, nil
}

func TestRunnerAcceptsChatJob(t *testing.T) {
	client := &claimClient{tasks: []schedulerapi.TaskRequest{{
		JobID: "job-1", RunID: "run-1", LeaseID: "lease-1", SessionID: "session-1",
		Title: "Heartbeat", Objective: "check status", ExecutionMode: "agent",
		SkillIDs: []string{"demo"}, ModelRef: "openai/gpt-test", IdempotencyKey: "key/1",
	}}}
	submit := &submitStub{}
	runner, err := New(Config{Client: client, Submissions: submit, Owner: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if submit.last.SessionID != "session-1" || submit.last.ExecutionMode != kernel.ExecutionModeAgent {
		t.Fatalf("submit = %+v", submit.last)
	}
	if submit.last.ModelRef != "openai/gpt-test" {
		t.Fatalf("model_ref = %q", submit.last.ModelRef)
	}
	if len(submit.last.SkillIDs) != 1 || submit.last.SkillIDs[0] != "demo" {
		t.Fatalf("skills = %v", submit.last.SkillIDs)
	}
	if len(client.acks) != 1 || client.acks[0].Status != "task_created" || client.acks[0].CoreTaskID == "" {
		t.Fatalf("acks = %+v", client.acks)
	}
}

func TestRunnerAcceptsPlanMode(t *testing.T) {
	client := &claimClient{tasks: []schedulerapi.TaskRequest{{
		JobID: "job-2", RunID: "run-2", LeaseID: "lease-2", SessionID: "session-2",
		Title: "Review", Objective: "read only", ExecutionMode: "plan",
		ModelRef: "openai/gpt-test", IdempotencyKey: "key/2",
	}}}
	submit := &submitStub{}
	runner, err := New(Config{Client: client, Submissions: submit, Owner: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if submit.last.ExecutionMode != kernel.ExecutionModePlan {
		t.Fatalf("mode = %q", submit.last.ExecutionMode)
	}
}

func TestRunnerSkipsEmptyModelPin(t *testing.T) {
	client := &claimClient{tasks: []schedulerapi.TaskRequest{{
		JobID: "job-empty", RunID: "run-empty", LeaseID: "lease-empty", SessionID: "session-e",
		Title: "X", Objective: "y", ExecutionMode: "agent", IdempotencyKey: "key/empty",
	}}}
	submit := &submitStub{}
	runner, err := New(Config{Client: client, Submissions: submit, Owner: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.RunOnce(context.Background()); err == nil {
		t.Fatal("expected error for empty model_ref")
	}
	if submit.last.TaskID != "" {
		t.Fatalf("submit should not run: %+v", submit.last)
	}
	if len(client.acks) != 1 || client.acks[0].Status != "failed" {
		t.Fatalf("acks = %+v", client.acks)
	}
}

func TestRunnerAcksFailedOnSubmitError(t *testing.T) {
	client := &claimClient{tasks: []schedulerapi.TaskRequest{{
		JobID: "job-3", RunID: "run-3", LeaseID: "lease-3", SessionID: "session-3",
		Title: "X", Objective: "y", ExecutionMode: "agent", ModelRef: "openai/gpt-test", IdempotencyKey: "key/3",
	}}}
	submit := &submitStub{err: errors.New("provider down")}
	runner, err := New(Config{Client: client, Submissions: submit, Owner: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.RunOnce(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if len(client.acks) != 1 || client.acks[0].Status != "failed" {
		t.Fatalf("acks = %+v", client.acks)
	}
}

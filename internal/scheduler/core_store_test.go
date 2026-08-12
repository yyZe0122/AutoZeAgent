package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	coresqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
	"github.com/yyZe0122/yunmengze-agent/pkg/schedulerapi"
)

func TestCoreStoreClaimsAndAcknowledgesDueJob(t *testing.T) {
	ctx := context.Background()
	database, err := coresqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, err := kernel.NewRepository(database.SQL())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateSession(ctx, "session-jobs", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(database.SQL())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	request := schedulerapi.CreateRequest{
		Name: "heartbeat", SessionID: "session-jobs", TaskTitle: "Heartbeat",
		TaskObjective: "Inspect progress", ExecutionMode: schedulerapi.ExecutionModeAgent,
		SkillIDs: []string{"demo"}, ModelRef: "openai/gpt-test",
		IntervalSeconds: 60, NextRunAt: now.Add(-time.Second).Format(time.RFC3339Nano),
		TimeoutSeconds: 300, MaxRetries: 2, BackoffSeconds: 1,
		MisfirePolicy: schedulerapi.MisfireRunOnce, IdempotencyKey: "heartbeat/session-jobs",
	}
	job, err := store.Create(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if job.ExecutionMode != schedulerapi.ExecutionModeAgent {
		t.Fatalf("execution_mode = %q", job.ExecutionMode)
	}
	if job.ModelRef != "openai/gpt-test" {
		t.Fatalf("model_ref = %q", job.ModelRef)
	}
	if len(job.SkillIDs) != 1 || job.SkillIDs[0] != "demo" {
		t.Fatalf("skill_ids = %v", job.SkillIDs)
	}
	duplicate, err := store.Create(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != job.ID {
		t.Fatalf("duplicate job ID = %q, want %q", duplicate.ID, job.ID)
	}

	tasks, err := store.ClaimDue(ctx, schedulerapi.ClaimDueRequest{
		Owner: "test-daemon", Now: now.Format(time.RFC3339Nano), Limit: 10, LeaseSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].SessionID != "session-jobs" || tasks[0].ExecutionMode != schedulerapi.ExecutionModeAgent {
		t.Fatalf("claimed tasks = %+v", tasks)
	}
	if len(tasks[0].SkillIDs) != 1 || tasks[0].SkillIDs[0] != "demo" {
		t.Fatalf("claimed skill_ids = %v", tasks[0].SkillIDs)
	}
	if tasks[0].ModelRef != "openai/gpt-test" {
		t.Fatalf("claimed model_ref = %q", tasks[0].ModelRef)
	}
	again, err := store.ClaimDue(ctx, schedulerapi.ClaimDueRequest{
		Owner: "test-daemon", Now: now.Format(time.RFC3339Nano), Limit: 10, LeaseSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("second claim = %+v, want none while lease is active", again)
	}
	if _, err := store.Acknowledge(ctx, schedulerapi.AcknowledgeRequest{
		RunID: tasks[0].RunID, LeaseID: tasks[0].LeaseID, CoreTaskID: "task-created", Status: "task_created",
	}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := database.SQL().QueryRowContext(ctx, "SELECT status FROM job_runs WHERE run_id = ?", tasks[0].RunID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "task_created" {
		t.Fatalf("job run status = %q", status)
	}
	var leases int
	if err := database.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM job_leases WHERE job_id = ?", job.ID).Scan(&leases); err != nil {
		t.Fatal(err)
	}
	if leases != 0 {
		t.Fatalf("lease count = %d, want 0", leases)
	}
}

func TestCoreStoreDefaultsExecutionModeAgent(t *testing.T) {
	ctx := context.Background()
	database, err := coresqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, _ := kernel.NewRepository(database.SQL())
	_, _ = repository.CreateSession(ctx, "session-default", time.Now().UTC())
	store, _ := NewStore(database.SQL())
	job, err := store.Create(ctx, schedulerapi.CreateRequest{
		Name: "default", SessionID: "session-default", TaskTitle: "T", TaskObjective: "O",
		IntervalSeconds: 60, IdempotencyKey: "default-mode",
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ExecutionMode != schedulerapi.ExecutionModeAgent {
		t.Fatalf("execution_mode = %q", job.ExecutionMode)
	}
}

func TestCoreStorePinsMainModelRefWhenEmpty(t *testing.T) {
	ctx := context.Background()
	database, err := coresqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, _ := kernel.NewRepository(database.SQL())
	_, _ = repository.CreateSession(ctx, "session-pin", time.Now().UTC())
	store, err := NewStoreWithMainRef(database.SQL(), func() string { return "deepseek/deepseek-chat" })
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.Create(ctx, schedulerapi.CreateRequest{
		Name: "pin", SessionID: "session-pin", TaskTitle: "T", TaskObjective: "O",
		IntervalSeconds: 60, IdempotencyKey: "pin-main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ModelRef != "deepseek/deepseek-chat" {
		t.Fatalf("model_ref = %q", job.ModelRef)
	}
}

func TestCoreStoreRejectsMissingSession(t *testing.T) {
	ctx := context.Background()
	database, err := coresqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store, _ := NewStore(database.SQL())
	_, err = store.Create(ctx, schedulerapi.CreateRequest{
		Name: "missing", SessionID: "no-such", TaskTitle: "T", TaskObjective: "O",
		IntervalSeconds: 60, IdempotencyKey: "missing-session",
	})
	if err == nil || err.Error() != "session not found" {
		t.Fatalf("err = %v", err)
	}
}

func TestCoreStorePauseResumeCancel(t *testing.T) {
	ctx := context.Background()
	database, err := coresqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, _ := kernel.NewRepository(database.SQL())
	_, _ = repository.CreateSession(ctx, "session-state", time.Now().UTC())
	store, _ := NewStore(database.SQL())
	job, err := store.Create(ctx, schedulerapi.CreateRequest{
		Name: "state", SessionID: "session-state", TaskTitle: "State", TaskObjective: "State",
		IntervalSeconds: 60, IdempotencyKey: "state-job",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct{ status, want string }{
		{schedulerapi.StatusPaused, schedulerapi.StatusPaused},
		{schedulerapi.StatusActive, schedulerapi.StatusActive},
		{schedulerapi.StatusArchived, schedulerapi.StatusArchived},
	} {
		job, err = store.ChangeState(ctx, schedulerapi.StateRequest{JobID: job.ID, Reviewer: "test", Reason: "test state"}, step.status)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != step.want {
			t.Fatalf("job status = %q, want %q", job.Status, step.want)
		}
	}
}

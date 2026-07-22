package scheduler

import (
	"context"
	"testing"
	"time"

	"autozeagent.local/autozeagent/internal/kernel"
	coresqlite "autozeagent.local/autozeagent/internal/store/sqlite"
	"autozeagent.local/autozeagent/pkg/schedulerapi"
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
		TaskObjective: "Inspect progress", IntervalSeconds: 60, NextRunAt: now.Add(-time.Second).Format(time.RFC3339Nano),
		TimeoutSeconds: 300, MaxRetries: 2, BackoffSeconds: 1,
		MisfirePolicy: schedulerapi.MisfireRunOnce, IdempotencyKey: "heartbeat/session-jobs",
	}
	job, err := store.Create(ctx, request)
	if err != nil {
		t.Fatal(err)
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
	if len(tasks) != 1 || !tasks[0].RequiresPlan || tasks[0].SessionID != "session-jobs" {
		t.Fatalf("claimed tasks = %+v", tasks)
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
		RunID: tasks[0].RunID, LeaseID: tasks[0].LeaseID, CoreTaskID: "task-created", Status: "waiting_approval",
	}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := database.SQL().QueryRowContext(ctx, "SELECT status FROM job_runs WHERE run_id = ?", tasks[0].RunID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "waiting_approval" {
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

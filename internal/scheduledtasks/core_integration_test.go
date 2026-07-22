package scheduledtasks

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/planner"
	"autozeagent.local/autozeagent/internal/scheduler"
	coresqlite "autozeagent.local/autozeagent/internal/store/sqlite"
	"autozeagent.local/autozeagent/internal/tasksubmission"
	"autozeagent.local/autozeagent/pkg/providerapi"
	"autozeagent.local/autozeagent/pkg/schedulerapi"
)

func TestRunnerPersistsScheduledTaskAndRetriesWithoutDuplicate(t *testing.T) {
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
	if _, err := repository.CreateSession(ctx, "session-scheduled", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	store, err := scheduler.NewStore(database.SQL())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	job, err := store.Create(ctx, schedulerapi.CreateRequest{
		Name: "heartbeat", SessionID: "session-scheduled", TaskTitle: "Heartbeat",
		TaskObjective: "Inspect progress", IntervalSeconds: 60,
		NextRunAt:      now.Add(-time.Second).Format(time.RFC3339Nano),
		TimeoutSeconds: 300, MaxRetries: 2, BackoffSeconds: 1,
		MisfirePolicy: schedulerapi.MisfireRunOnce, IdempotencyKey: "heartbeat/session-scheduled",
	})
	if err != nil {
		t.Fatal(err)
	}

	provider := &scheduledProvider{response: providerapi.CompletionResponse{Content: scheduledPlanJSON}}
	plannerEngine, err := planner.New(planner.Config{Provider: provider, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	planningService, err := planner.NewService(repository, plannerEngine)
	if err != nil {
		t.Fatal(err)
	}
	submissions, err := tasksubmission.New(tasksubmission.Config{Repository: repository, Planner: planningService})
	if err != nil {
		t.Fatal(err)
	}
	client := &retryingClient{Store: store, failAcknowledgements: 1}
	runner, err := New(Config{Client: client, Submissions: submissions, Owner: "test-daemon"})
	if err != nil {
		t.Fatal(err)
	}

	if err := runner.RunOnce(ctx); err == nil {
		t.Fatal("first run should report the injected acknowledgement failure")
	}
	if len(client.claimed) != 1 {
		t.Fatalf("claimed tasks = %d, want 1", len(client.claimed))
	}
	request := client.claimed[0]
	wantTaskID := taskIDFor(request.IdempotencyKey)
	assertTaskCount(t, database.SQL(), wantTaskID, 1)
	task, err := repository.GetTask(ctx, wantTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.State != kernel.TaskWaitingApproval {
		t.Fatalf("task state = %q, want %q", task.State, kernel.TaskWaitingApproval)
	}

	// Retry the same delivery after Core persisted the Task but before Scheduler
	// persisted its acknowledgement. AllowExisting makes this at-least-once safe.
	if err := runner.accept(ctx, request); err != nil {
		t.Fatal(err)
	}
	assertTaskCount(t, database.SQL(), wantTaskID, 1)

	var status, coreTaskID string
	if err := database.SQL().QueryRowContext(ctx,
		"SELECT status, core_task_id FROM job_runs WHERE run_id = ?", request.RunID,
	).Scan(&status, &coreTaskID); err != nil {
		t.Fatal(err)
	}
	if status != "waiting_approval" || coreTaskID != string(wantTaskID) {
		t.Fatalf("job run status/core task = %q/%q, want waiting_approval/%q", status, coreTaskID, wantTaskID)
	}
	var leases int
	if err := database.SQL().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM job_leases WHERE job_id = ?", job.ID,
	).Scan(&leases); err != nil {
		t.Fatal(err)
	}
	if leases != 0 {
		t.Fatalf("lease count = %d, want 0", leases)
	}
}

type scheduledProvider struct {
	response providerapi.CompletionResponse
}

func (p *scheduledProvider) Name() string { return "scheduled-test" }

func (p *scheduledProvider) Complete(context.Context, providerapi.CompletionRequest) (providerapi.CompletionResponse, error) {
	return providerapi.CompletionResponse{}, errors.New("not used")
}

func (p *scheduledProvider) Stream(_ context.Context, _ providerapi.CompletionRequest, handler providerapi.StreamHandler) error {
	return providerapi.EmitResponse(p.response, handler)
}

func (p *scheduledProvider) Health(context.Context) providerapi.HealthStatus {
	return providerapi.HealthStatus{Healthy: true}
}

type retryingClient struct {
	*scheduler.Store
	failAcknowledgements int
	claimed              []schedulerapi.TaskRequest
}

func (c *retryingClient) ClaimScheduledTasks(ctx context.Context, request schedulerapi.ClaimDueRequest) ([]schedulerapi.TaskRequest, error) {
	tasks, err := c.Store.ClaimScheduledTasks(ctx, request)
	c.claimed = append(c.claimed, tasks...)
	return tasks, err
}

func (c *retryingClient) AcknowledgeScheduledTask(ctx context.Context, request schedulerapi.AcknowledgeRequest) error {
	if c.failAcknowledgements > 0 {
		c.failAcknowledgements--
		return errors.New("injected acknowledgement failure")
	}
	return c.Store.AcknowledgeScheduledTask(ctx, request)
}

func assertTaskCount(t *testing.T, db *sql.DB, taskID kernel.TaskID, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM tasks WHERE task_id = ?", taskID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("task count = %d, want %d", count, want)
	}
}

const scheduledPlanJSON = `{
	"objective": "Inspect progress",
	"budget": {
		"max_tokens": 100,
		"max_cost_micros": 0,
		"max_duration_ms": 30000
	},
	"steps": [
		{
			"title": "Inspect",
			"risk": "R0",
			"expected_side_effects": [],
			"rollback": "No changes were made",
			"timeout_ms": 5000,
			"capabilities": []
		}
	]
}`

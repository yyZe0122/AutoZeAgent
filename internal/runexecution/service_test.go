package runexecution

import (
	"context"
	"errors"
	"testing"
	"time"

	"autozeagent.local/autozeagent/internal/agent"
	"autozeagent.local/autozeagent/internal/applicationerror"
	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/audit"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/policy"
	storesqlite "autozeagent.local/autozeagent/internal/store/sqlite"
)

type blockingAgentRunner struct {
	started chan struct{}
	request agent.RunRequest
}

func (r *blockingAgentRunner) Run(ctx context.Context, request agent.RunRequest) (agent.Result, error) {
	r.request = request
	close(r.started)
	<-ctx.Done()
	return agent.Result{}, ctx.Err()
}

type countingAgentRunner struct {
	calls int
}

func (r *countingAgentRunner) Run(context.Context, agent.RunRequest) (agent.Result, error) {
	r.calls++
	return agent.Result{}, nil
}

func TestRunOnceStopsWhenClaimIsLost(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	stamp := now.Format(time.RFC3339Nano)

	statements := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO tasks (task_id, title, objective, state, execution_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", []any{"task-1", "test", "test", kernel.TaskRunning, kernel.ExecutionModePlan, stamp, stamp}},
		{"INSERT INTO plans (plan_id, task_id, revision, state, scope_hash, created_at, updated_at, document) VALUES (?, ?, 1, ?, ?, ?, ?, '{}')", []any{"plan-1", "task-1", kernel.PlanApproved, "hash-1", stamp, stamp}},
		{"INSERT INTO plan_steps (step_id, plan_id, position, title, state, effect_level, created_at, updated_at) VALUES (?, ?, 0, ?, ?, ?, ?, ?)", []any{"step-1", "plan-1", "test", kernel.StepApproved, "R0", stamp, stamp}},
		{"INSERT INTO runs (run_id, task_id, plan_id, state, started_at, updated_at, step_id) VALUES (?, ?, ?, ?, ?, ?, ?)", []any{"run-1", "task-1", "plan-1", kernel.RunCreated, stamp, stamp, "step-1"}},
		{`CREATE TRIGGER lose_run_claim
			BEFORE UPDATE OF state ON runs
			WHEN OLD.run_id = 'run-1' AND OLD.state = 'created' AND NEW.state = 'running'
			BEGIN
				UPDATE runs SET state = 'running' WHERE run_id = OLD.run_id;
				SELECT RAISE(IGNORE);
			END`, nil},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("setup database: %v", err)
		}
	}

	auditStore, err := audit.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	runner := &countingAgentRunner{}
	service := &Service{db: db, agent: runner, audit: auditStore, now: func() time.Time { return now }}

	if err := service.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("agent ran %d times after losing claim; want 0", runner.calls)
	}

	var state string
	if err := db.QueryRowContext(ctx, "SELECT state FROM runs WHERE run_id = 'run-1'").Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != string(kernel.RunCreated) {
		t.Fatalf("run state = %q; want %q", state, kernel.RunCreated)
	}
	var startedAudits int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_log WHERE resource_id = 'run-1' AND outcome = 'started'").Scan(&startedAudits); err != nil {
		t.Fatal(err)
	}
	if startedAudits != 0 {
		t.Fatalf("started audits = %d; want 0", startedAudits)
	}
}

func TestControlTaskPauseResumeCancel(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	stamp := now.Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, "INSERT INTO sessions (session_id, state, created_at, updated_at) VALUES (?, ?, ?, ?)", "session-1", kernel.SessionActive, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO tasks (task_id, session_id, title, objective, state, execution_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", "task-1", "session-1", "test", "test", kernel.TaskRunning, kernel.ExecutionModePlan, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	repository, err := kernel.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	approvalRepository, err := approval.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{db: db, repository: repository, approvals: approvalRepository, now: func() time.Time { return now }}

	activeContext, activeCancel := context.WithCancel(context.Background())
	defer activeCancel()
	service.setActive("task-1", activeCancel)
	paused, err := service.ControlTask(ctx, TaskActionRequest{TaskID: "task-1", ExpectedVersion: 1, Action: TaskActionPause, Reason: "operator pause"})
	if err != nil {
		t.Fatalf("pause task: %v", err)
	}
	if paused.State != kernel.TaskPaused || paused.Version != 2 {
		t.Fatalf("paused task = %+v", paused)
	}
	select {
	case <-activeContext.Done():
	default:
		t.Fatal("pause did not cancel the active execution context")
	}

	resumed, err := service.ControlTask(ctx, TaskActionRequest{TaskID: "task-1", ExpectedVersion: paused.Version, Action: TaskActionResume})
	if err != nil {
		t.Fatalf("resume task: %v", err)
	}
	if resumed.State != kernel.TaskRunning || resumed.Version != 3 {
		t.Fatalf("resumed task = %+v", resumed)
	}

	cancelContext, cancelActive := context.WithCancel(context.Background())
	defer cancelActive()
	service.setActive("task-1", cancelActive)
	cancelled, err := service.ControlTask(ctx, TaskActionRequest{TaskID: "task-1", ExpectedVersion: resumed.Version, Action: TaskActionCancel})
	if err != nil {
		t.Fatalf("cancel task: %v", err)
	}
	if cancelled.State != kernel.TaskCancelled || cancelled.Version != 4 {
		t.Fatalf("cancelled task = %+v", cancelled)
	}
	select {
	case <-cancelContext.Done():
	default:
		t.Fatal("cancel did not cancel the active execution context")
	}

	_, err = service.ControlTask(ctx, TaskActionRequest{TaskID: "task-1", ExpectedVersion: cancelled.Version, Action: TaskActionResume})
	if !applicationerror.IsCode(err, applicationerror.CodeConflict) {
		t.Fatalf("resume cancelled task error = %v, want conflict", err)
	}
}

func TestPauseInterruptsRunAndLeavesItRecoverable(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	stamp := now.Format(time.RFC3339Nano)
	plan := approval.PlanDocument{
		PlanID: "plan-1", TaskID: "task-1", Revision: 1, Objective: "keep running",
		Budget: approval.PlanBudget{MaxTokens: 1000, MaxDurationMillis: int64(time.Hour / time.Millisecond)},
		Steps: []approval.StepScope{{
			StepID: "step-1", Position: 0, Title: "wait", Risk: policy.RiskR0,
			TimeoutMillis: int64(time.Hour / time.Millisecond),
		}},
	}
	document, err := plan.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := plan.Hash()
	if err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO sessions (session_id, state, created_at, updated_at) VALUES (?, ?, ?, ?)", []any{"session-1", kernel.SessionActive, stamp, stamp}},
		{"INSERT INTO tasks (task_id, session_id, title, objective, state, execution_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", []any{"task-1", "session-1", "test", "test", kernel.TaskRunning, kernel.ExecutionModePlan, stamp, stamp}},
		{"INSERT INTO plans (plan_id, task_id, revision, state, scope_hash, created_at, updated_at, document) VALUES (?, ?, 1, ?, ?, ?, ?, ?)", []any{"plan-1", "task-1", kernel.PlanApproved, hash, stamp, stamp, string(document)}},
		{"INSERT INTO plan_steps (step_id, plan_id, position, title, state, effect_level, created_at, updated_at) VALUES (?, ?, 0, ?, ?, ?, ?, ?)", []any{"step-1", "plan-1", "wait", kernel.StepRunning, policy.RiskR0, stamp, stamp}},
		{"INSERT INTO runs (run_id, task_id, plan_id, state, started_at, updated_at, step_id) VALUES (?, ?, ?, ?, ?, ?, ?)", []any{"run-1", "task-1", "plan-1", kernel.RunRunning, stamp, stamp, "step-1"}},
		{"INSERT INTO approvals (approval_id, plan_id, plan_revision, decision, scope_hash, decided_by, decided_at) VALUES (?, ?, 1, ?, ?, ?, ?)", []any{"approval-1", "plan-1", approval.DecisionApproved, hash, "tester", stamp}},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("setup database: %v", err)
		}
	}
	repository, err := kernel.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	auditStore, err := audit.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	runner := &blockingAgentRunner{started: make(chan struct{})}
	service := &Service{db: db, repository: repository, agent: runner, audit: auditStore, now: func() time.Time { return now }}
	done := make(chan error, 1)
	go func() { done <- service.RunOnce(ctx) }()
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not start")
	}
	if runner.request.MaxTotalTokens != plan.Budget.MaxTokens || runner.request.MaxCostMicros != plan.Budget.MaxCostMicros {
		t.Fatalf("agent budgets = tokens:%d cost:%d", runner.request.MaxTotalTokens, runner.request.MaxCostMicros)
	}
	paused, err := service.ControlTask(ctx, TaskActionRequest{TaskID: "task-1", ExpectedVersion: 1, Action: TaskActionPause})
	if err != nil {
		t.Fatalf("pause task: %v", err)
	}
	if paused.State != kernel.TaskPaused {
		t.Fatalf("task state = %s, want %s", paused.State, kernel.TaskPaused)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunOnce after pause: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("paused run did not stop")
	}
	var runState, stepState string
	if err := db.QueryRowContext(ctx, "SELECT state FROM runs WHERE run_id = ?", "run-1").Scan(&runState); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT state FROM plan_steps WHERE step_id = ?", "step-1").Scan(&stepState); err != nil {
		t.Fatal(err)
	}
	if runState != string(kernel.RunRunning) || stepState != string(kernel.StepRunning) {
		t.Fatalf("recoverable states = run:%s step:%s", runState, stepState)
	}
}

func TestToolTimeoutMillisCappedByCapabilityGrant(t *testing.T) {
	step := approval.StepScope{
		TimeoutMillis: 60_000,
		Capabilities: []approval.CapabilityScope{
			{Capability: "fs_read", MaxDurationMillis: 5_000},
			{Capability: "fs_list", MaxDurationMillis: 10_000},
		},
	}
	if got := toolTimeoutMillis(step); got != 5_000 {
		t.Fatalf("toolTimeoutMillis = %d, want 5000", got)
	}
	step.Capabilities = nil
	if got := toolTimeoutMillis(step); got != 60_000 {
		t.Fatalf("toolTimeoutMillis without capabilities = %d, want 60000", got)
	}
}

func TestExecutionTimeoutUsesRemainingPlanDuration(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	started := now.Add(-59 * time.Minute).Format(time.RFC3339Nano)
	stamp := now.Format(time.RFC3339Nano)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO tasks (task_id, title, objective, state, execution_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", []any{"task-timeout", "test", "test", kernel.TaskRunning, kernel.ExecutionModePlan, stamp, stamp}},
		{"INSERT INTO plans (plan_id, task_id, revision, state, scope_hash, created_at, updated_at, document) VALUES (?, ?, 1, ?, ?, ?, ?, '{}')", []any{"plan-timeout", "task-timeout", kernel.PlanApproved, "hash", stamp, stamp}},
		{"INSERT INTO plan_steps (step_id, plan_id, position, title, state, effect_level, created_at, updated_at) VALUES (?, ?, 0, ?, ?, ?, ?, ?)", []any{"step-timeout", "plan-timeout", "test", kernel.StepRunning, "R0", stamp, stamp}},
		{"INSERT INTO runs (run_id, task_id, plan_id, state, started_at, updated_at, step_id) VALUES (?, ?, ?, ?, ?, ?, ?)", []any{"run-timeout", "task-timeout", "plan-timeout", kernel.RunRunning, started, stamp, "step-timeout"}},
	} {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{db: db, now: func() time.Time { return now }}
	plan := approval.PlanDocument{Budget: approval.PlanBudget{MaxDurationMillis: int64(time.Hour / time.Millisecond)}}
	// Short step tool timeout must not shrink the provider/agent execution context.
	step := approval.StepScope{TimeoutMillis: 1_000}

	timeout, err := service.executionTimeout(ctx, "plan-timeout", plan)
	if err != nil {
		t.Fatal(err)
	}
	if timeout != time.Minute {
		t.Fatalf("execution timeout = %s, want 1m (plan remaining, not step %dms)", timeout, step.TimeoutMillis)
	}

	service.now = func() time.Time { return now.Add(time.Minute) }
	_, err = service.executionTimeout(ctx, "plan-timeout", plan)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired execution error = %v, want context.DeadlineExceeded", err)
	}
}

func TestRemainingPlanBudgetSubtractsCompletedRuns(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	stamp := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO tasks (task_id, title, objective, state, execution_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", []any{"task-budget", "test", "test", kernel.TaskRunning, kernel.ExecutionModePlan, stamp, stamp}},
		{"INSERT INTO plans (plan_id, task_id, revision, state, scope_hash, created_at, updated_at, document) VALUES (?, ?, 1, ?, ?, ?, ?, '{}')", []any{"plan-budget", "task-budget", kernel.PlanApproved, "hash", stamp, stamp}},
		{"INSERT INTO plan_steps (step_id, plan_id, position, title, state, effect_level, created_at, updated_at) VALUES (?, ?, 0, ?, ?, ?, ?, ?)", []any{"step-budget-1", "plan-budget", "first", kernel.StepCompleted, "R0", stamp, stamp}},
		{"INSERT INTO plan_steps (step_id, plan_id, position, title, state, effect_level, created_at, updated_at) VALUES (?, ?, 1, ?, ?, ?, ?, ?)", []any{"step-budget-2", "plan-budget", "second", kernel.StepRunning, "R0", stamp, stamp}},
		{"INSERT INTO runs (run_id, task_id, plan_id, state, started_at, updated_at, step_id) VALUES (?, ?, ?, ?, ?, ?, ?)", []any{"run-budget-1", "task-budget", "plan-budget", kernel.RunCompleted, stamp, stamp, "step-budget-1"}},
		{"INSERT INTO runs (run_id, task_id, plan_id, state, started_at, updated_at, step_id) VALUES (?, ?, ?, ?, ?, ?, ?)", []any{"run-budget-2", "task-budget", "plan-budget", kernel.RunRunning, stamp, stamp, "step-budget-2"}},
		{"INSERT INTO agent_run_records (run_id, position, record_type, message, usage, created_at) VALUES (?, 0, 'assistant_message', ?, ?, ?)", []any{"run-budget-1", `{"role":"assistant","content":"first"}`, `{"total_tokens":40,"cost":{"currency":"USD","micros":300}}`, stamp}},
	} {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{db: db}
	item := execution{RunID: "run-budget-2", PlanID: "plan-budget"}
	plan := approval.PlanDocument{Budget: approval.PlanBudget{MaxTokens: 100, MaxCostMicros: 1000}}

	maxTokens, maxCost, err := service.remainingPlanBudget(ctx, item, plan)
	if err != nil {
		t.Fatal(err)
	}
	if maxTokens != 60 || maxCost != 700 {
		t.Fatalf("remaining budget = tokens:%d cost:%d, want 60/700", maxTokens, maxCost)
	}

	if _, err := db.ExecContext(ctx,
		"INSERT INTO agent_run_records (run_id, position, record_type, message, usage, created_at) VALUES (?, 1, 'assistant_message', ?, ?, ?)",
		"run-budget-1", `{"role":"assistant","content":"second"}`, `{"total_tokens":60,"cost":{"currency":"USD","micros":700}}`, stamp,
	); err != nil {
		t.Fatal(err)
	}
	_, _, err = service.remainingPlanBudget(ctx, item, plan)
	if !errors.Is(err, agent.ErrTokenBudgetExceeded) {
		t.Fatalf("exhausted budget error = %v, want ErrTokenBudgetExceeded", err)
	}
}

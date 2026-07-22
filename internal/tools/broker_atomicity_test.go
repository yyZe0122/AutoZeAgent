package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/artifacts"
	"autozeagent.local/autozeagent/internal/policy"
	storesqlite "autozeagent.local/autozeagent/internal/store/sqlite"
	"autozeagent.local/autozeagent/pkg/toolapi"
)

type atomicityTestTool struct {
	calls int
}

func (t *atomicityTestTool) Definition() toolapi.Definition {
	return toolapi.Definition{
		Name:                 "test.write",
		Description:          "test write tool",
		InputSchema:          json.RawMessage(`{"type":"object","properties":{}}`),
		Risk:                 string(policy.RiskR1),
		DefaultTimeoutMillis: 100,
	}
}

func (t *atomicityTestTool) Authorization(json.RawMessage) (Authorization, error) {
	return Authorization{Capability: "test.write"}, nil
}

func (t *atomicityTestTool) Execute(context.Context, json.RawMessage) (json.RawMessage, error) {
	t.calls++
	return json.RawMessage(`{"ok":true}`), nil
}

func TestExecuteRollsBackGrantWhenToolCallStartFails(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	stamp := now.Format(time.RFC3339Nano)
	expires := now.Add(time.Hour).Format(time.RFC3339Nano)

	statements := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO tasks (task_id, title, objective, state, created_at, updated_at) VALUES (?, ?, ?, 'running', ?, ?)", []any{"task-1", "test", "test", stamp, stamp}},
		{"INSERT INTO plans (plan_id, task_id, revision, state, scope_hash, created_at, updated_at, document) VALUES (?, ?, 1, 'running', ?, ?, ?, '{}')", []any{"plan-1", "task-1", "hash-1", stamp, stamp}},
		{"INSERT INTO plan_steps (step_id, plan_id, position, title, state, effect_level, created_at, updated_at) VALUES (?, ?, 0, ?, 'running', 'R1', ?, ?)", []any{"step-1", "plan-1", "test", stamp, stamp}},
		{"INSERT INTO approvals (approval_id, plan_id, plan_revision, decision, scope_hash, decided_by, decided_at) VALUES (?, ?, 1, 'approved', ?, 'tester', ?)", []any{"approval-1", "plan-1", "hash-1", stamp}},
		{`INSERT INTO capability_grants (
			grant_id, approval_id, task_id, plan_id, step_id, capability, resource_scope,
			issued_at, expires_at, plan_hash, paths_json, command_name, command_args_json,
			network_domains_json, max_duration_ms, max_calls, used_calls, one_time, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, '[]', ?, ?, ?, '[]', '', '[]', '[]', 1000, 1, 0, 1, ?)`, []any{"grant-1", "approval-1", "task-1", "plan-1", "step-1", "test.write", stamp, expires, "hash-1", stamp}},
		{"INSERT INTO runs (run_id, task_id, plan_id, state, started_at, updated_at, step_id) VALUES (?, ?, ?, 'running', ?, ?, ?)", []any{"run-1", "task-1", "plan-1", stamp, stamp, "step-1"}},
		{"INSERT INTO tool_calls (tool_call_id, run_id, step_id, tool_name, state, request, started_at) VALUES (?, ?, ?, ?, 'running', '{}', ?)", []any{"call-1", "run-1", "step-1", "existing", stamp}},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("setup database: %v", err)
		}
	}

	repository, err := approval.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	artifactStore, err := artifacts.NewStore(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	broker, err := NewBroker(Config{
		DB: db, Approvals: repository, Policy: policy.NewEvaluator(policy.DefaultConfig()),
		Artifacts: artifactStore, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := &atomicityTestTool{}
	if err := broker.Register(tool); err != nil {
		t.Fatal(err)
	}

	_, err = broker.Execute(ctx, toolapi.Request{
		CallID: "call-1", RunID: "run-1", TaskID: "task-1", PlanID: "plan-1",
		PlanHash: "hash-1", StepID: "step-1", CapabilityGrantID: "grant-1",
		Actor: "agent", Tool: "test.write", Arguments: json.RawMessage(`{}`),
	})
	if err == nil || !strings.Contains(err.Error(), "insert tool call") {
		t.Fatalf("Execute error = %v; want insert tool call failure", err)
	}
	if tool.calls != 0 {
		t.Fatalf("tool executed %d times; want 0", tool.calls)
	}

	var usedCalls int
	if err := db.QueryRowContext(ctx, "SELECT used_calls FROM capability_grants WHERE grant_id = 'grant-1'").Scan(&usedCalls); err != nil {
		t.Fatal(err)
	}
	if usedCalls != 0 {
		t.Fatalf("used_calls = %d; want 0", usedCalls)
	}
	var startedAudits int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_log WHERE resource_id = 'call-1' AND outcome = 'started'").Scan(&startedAudits); err != nil {
		t.Fatal(err)
	}
	if startedAudits != 0 {
		t.Fatalf("started audits = %d; want 0", startedAudits)
	}

	response, err := broker.Execute(ctx, toolapi.Request{
		CallID: "call-2", RunID: "run-1", TaskID: "task-1", PlanID: "plan-1",
		PlanHash: "hash-1", StepID: "step-1", CapabilityGrantID: "grant-1",
		Actor: "agent", Tool: "test.write", Arguments: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Execute after rollback returned error: %v", err)
	}
	if response.CallID != "call-2" || tool.calls != 1 {
		t.Fatalf("successful retry response = %#v, calls = %d", response, tool.calls)
	}
	if err := db.QueryRowContext(ctx, "SELECT used_calls FROM capability_grants WHERE grant_id = 'grant-1'").Scan(&usedCalls); err != nil {
		t.Fatal(err)
	}
	if usedCalls != 1 {
		t.Fatalf("used_calls after successful retry = %d; want 1", usedCalls)
	}
}

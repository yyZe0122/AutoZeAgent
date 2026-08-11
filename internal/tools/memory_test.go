package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/artifacts"
	"autozeagent.local/autozeagent/internal/memory"
	"autozeagent.local/autozeagent/internal/policy"
	"autozeagent.local/autozeagent/internal/runmeta"
	storesqlite "autozeagent.local/autozeagent/internal/store/sqlite"
	"autozeagent.local/autozeagent/pkg/toolapi"
)

func TestMemorySearchAndWriteViaBroker(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	now := time.Date(2026, 8, 6, 15, 0, 0, 0, time.UTC)
	stamp := now.Format(time.RFC3339Nano)
	expires := now.Add(time.Hour).Format(time.RFC3339Nano)

	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO tasks (task_id, title, objective, state, created_at, updated_at) VALUES (?, ?, ?, 'running', ?, ?)", []any{"task-m", "m", "m", stamp, stamp}},
		{"INSERT INTO plans (plan_id, task_id, revision, state, scope_hash, created_at, updated_at, document) VALUES (?, ?, 1, 'running', ?, ?, ?, '{}')", []any{"plan-m", "task-m", "hash-m", stamp, stamp}},
		{"INSERT INTO plan_steps (step_id, plan_id, position, title, state, effect_level, created_at, updated_at) VALUES (?, ?, 0, ?, 'running', 'R1', ?, ?)", []any{"step-m", "plan-m", "m", stamp, stamp}},
		{"INSERT INTO approvals (approval_id, plan_id, plan_revision, decision, scope_hash, decided_by, decided_at) VALUES (?, ?, 1, 'approved', ?, 'tester', ?)", []any{"appr-m", "plan-m", "hash-m", stamp}},
		{`INSERT INTO capability_grants (
			grant_id, approval_id, task_id, plan_id, step_id, capability, resource_scope,
			issued_at, expires_at, plan_hash, paths_json, command_name, command_args_json,
			network_domains_json, max_duration_ms, max_calls, used_calls, one_time, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, '[]', ?, ?, ?, '[]', '', '[]', '[]', 60000, 100, 0, 0, ?)`,
			[]any{"grant-mw", "appr-m", "task-m", "plan-m", "step-m", "memory_write", stamp, expires, "hash-m", stamp}},
		{"INSERT INTO runs (run_id, task_id, plan_id, state, started_at, updated_at, step_id) VALUES (?, ?, ?, 'running', ?, ?, ?)", []any{"run-m", "task-m", "plan-m", stamp, stamp, "step-m"}},
	} {
		if _, err := db.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}

	store, err := memory.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := memory.New(memory.Config{Store: store, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
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
	if _, err := RegisterMemoryTools(broker, mgr); err != nil {
		t.Fatal(err)
	}

	toolCtx := runmeta.With(ctx, runmeta.Context{SessionID: "session-m", TaskID: "task-m", RunID: "run-m"})

	// R0 search: no grant required.
	resp, err := broker.Execute(toolCtx, toolapi.Request{
		CallID: "c-search", RunID: "run-m", TaskID: "task-m", PlanID: "plan-m",
		PlanHash: "hash-m", StepID: "step-m", Actor: "agent",
		Tool: "memory_search", Arguments: json.RawMessage(`{"query":""}`),
	})
	if err != nil {
		t.Fatalf("memory_search: %v", err)
	}
	_ = resp

	// R1 write: grant required.
	_, err = broker.Execute(toolCtx, toolapi.Request{
		CallID: "c-write", RunID: "run-m", TaskID: "task-m", PlanID: "plan-m",
		PlanHash: "hash-m", StepID: "step-m", Actor: "agent",
		CapabilityGrantID: "grant-mw",
		Tool:              "memory_write", Arguments: json.RawMessage(`{"content":"prefer absolute paths"}`),
	})
	if err != nil {
		t.Fatalf("memory_write: %v", err)
	}

	resp, err = broker.Execute(toolCtx, toolapi.Request{
		CallID: "c-search2", RunID: "run-m", TaskID: "task-m", PlanID: "plan-m",
		PlanHash: "hash-m", StepID: "step-m", Actor: "agent",
		Tool: "memory_search", Arguments: json.RawMessage(`{"query":"absolute"}`),
	})
	if err != nil {
		t.Fatalf("memory_search after write: %v", err)
	}
	if !strings.Contains(string(resp.Output), "absolute") {
		t.Fatalf("search output = %s", resp.Output)
	}
}

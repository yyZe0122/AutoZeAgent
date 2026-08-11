package corequery

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	coresqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
)

func TestTaskUsageSumsAssistantMessages(t *testing.T) {
	ctx := context.Background()
	database, err := coresqlite.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	stamp := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)

	for _, statement := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO tasks (task_id, title, objective, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			[]any{"task-u", "t", "o", kernel.TaskRunning, stamp, stamp}},
		{"INSERT INTO plans (plan_id, task_id, revision, state, scope_hash, created_at, updated_at, document) VALUES (?, ?, 1, ?, ?, ?, ?, '{}')",
			[]any{"plan-u", "task-u", kernel.PlanApproved, "hash", stamp, stamp}},
		{"INSERT INTO runs (run_id, task_id, plan_id, state, started_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			[]any{"run-u1", "task-u", "plan-u", kernel.RunCompleted, stamp, stamp}},
		{"INSERT INTO runs (run_id, task_id, plan_id, state, started_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			[]any{"run-u2", "task-u", "plan-u", kernel.RunRunning, stamp, stamp}},
		{"INSERT INTO agent_run_records (run_id, position, record_type, message, usage, created_at) VALUES (?, 0, 'assistant_message', ?, ?, ?)",
			[]any{"run-u1", `{"role":"assistant","content":"a"}`, `{"input_tokens":10,"output_tokens":20,"total_tokens":30,"cache_read_tokens":40,"cache_write_tokens":5,"cost":{"currency":"USD","micros":100}}`, stamp}},
		{"INSERT INTO agent_run_records (run_id, position, record_type, message, usage, created_at) VALUES (?, 0, 'assistant_message', ?, ?, ?)",
			[]any{"run-u2", `{"role":"assistant","content":"b"}`, `{"input_tokens":5,"output_tokens":15,"total_tokens":20,"cache_read_tokens":10,"cost":{"currency":"USD","micros":50}}`, stamp}},
		{"INSERT INTO agent_run_records (run_id, position, record_type, message, usage, created_at) VALUES (?, 1, 'tool_result', ?, ?, ?)",
			[]any{"run-u2", `{"role":"tool","content":"x"}`, `{"total_tokens":999}`, stamp}},
	} {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}

	store, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := store.TaskUsage(ctx, "task-u")
	if err != nil {
		t.Fatal(err)
	}
	if usage.TaskID != "task-u" {
		t.Fatalf("task_id = %s", usage.TaskID)
	}
	if usage.InputTokens != 15 || usage.OutputTokens != 35 || usage.TotalTokens != 50 || usage.CostMicros != 150 {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.CacheReadTokens != 50 || usage.CacheWriteTokens != 5 {
		t.Fatalf("cache usage = %+v", usage)
	}
	// rate = 50 / (50 + 15) ≈ 0.769
	rate, ok := usage.CacheHitRate()
	if !ok || rate < 0.76 || rate > 0.77 {
		t.Fatalf("CacheHitRate = %v ok=%v", rate, ok)
	}

	// Unknown task → zeros, no error.
	empty, err := store.TaskUsage(ctx, "task-missing")
	if err != nil {
		t.Fatal(err)
	}
	if empty.TotalTokens != 0 || empty.TaskID != "task-missing" {
		t.Fatalf("empty = %+v", empty)
	}
}

func TestRunUsageSelfAndChildRollup(t *testing.T) {
	ctx := context.Background()
	database, err := coresqlite.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	stamp := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)

	for _, statement := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO tasks (task_id, title, objective, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			[]any{"task-r", "t", "o", kernel.TaskRunning, stamp, stamp}},
		{"INSERT INTO plans (plan_id, task_id, revision, state, scope_hash, created_at, updated_at, document) VALUES (?, ?, 1, ?, ?, ?, ?, '{}')",
			[]any{"plan-r", "task-r", kernel.PlanApproved, "hash", stamp, stamp}},
		{"INSERT INTO runs (run_id, task_id, plan_id, state, started_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			[]any{"parent-run", "task-r", "plan-r", kernel.RunCompleted, stamp, stamp}},
		{"INSERT INTO runs (run_id, task_id, plan_id, state, started_at, updated_at, parent_run_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
			[]any{"child-run", "task-r", "plan-r", kernel.RunCompleted, stamp, stamp, "parent-run"}},
		{"INSERT INTO runs (run_id, task_id, plan_id, state, started_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			[]any{"sibling-run", "task-r", "plan-r", kernel.RunCompleted, stamp, stamp}},
		{"INSERT INTO agent_run_records (run_id, position, record_type, message, usage, created_at) VALUES (?, 0, 'assistant_message', ?, ?, ?)",
			[]any{"parent-run", `{"role":"assistant","content":"p"}`, `{"input_tokens":10,"output_tokens":20,"total_tokens":30,"cost":{"currency":"USD","micros":100}}`, stamp}},
		{"INSERT INTO agent_run_records (run_id, position, record_type, message, usage, created_at) VALUES (?, 0, 'assistant_message', ?, ?, ?)",
			[]any{"child-run", `{"role":"assistant","content":"c"}`, `{"input_tokens":5,"output_tokens":15,"total_tokens":20,"cost":{"currency":"USD","micros":50}}`, stamp}},
		{"INSERT INTO agent_run_records (run_id, position, record_type, message, usage, created_at) VALUES (?, 0, 'assistant_message', ?, ?, ?)",
			[]any{"sibling-run", `{"role":"assistant","content":"s"}`, `{"input_tokens":100,"output_tokens":100,"total_tokens":200,"cost":{"currency":"USD","micros":999}}`, stamp}},
	} {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}

	store, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := store.RunUsage(ctx, "parent-run")
	if err != nil {
		t.Fatal(err)
	}
	if usage.RunID != "parent-run" || usage.ChildRunCount != 1 {
		t.Fatalf("meta = %+v", usage)
	}
	if usage.Self.TotalTokens != 30 || usage.Children.TotalTokens != 20 || usage.TotalTokens != 50 {
		t.Fatalf("tokens self=%d children=%d total=%d", usage.Self.TotalTokens, usage.Children.TotalTokens, usage.TotalTokens)
	}
	if usage.CostMicros != 150 {
		t.Fatalf("cost = %d", usage.CostMicros)
	}

	empty, err := store.RunUsage(ctx, "missing-run")
	if err != nil {
		t.Fatal(err)
	}
	if empty.TotalTokens != 0 || empty.RunID != "missing-run" {
		t.Fatalf("empty = %+v", empty)
	}
}

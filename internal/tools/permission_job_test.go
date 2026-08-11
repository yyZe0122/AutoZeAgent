package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/approval"
	"github.com/yyZe0122/yunmengze-agent/internal/artifacts"
	"github.com/yyZe0122/yunmengze-agent/internal/policy"
	storesqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
	"github.com/yyZe0122/yunmengze-agent/pkg/toolapi"
)

func TestIsNonInteractiveToolCaller(t *testing.T) {
	if !isNonInteractiveToolCaller(toolapi.Request{TaskID: "scheduled_abc"}) {
		t.Fatal("scheduled_ task should be non-interactive")
	}
	if !isNonInteractiveToolCaller(toolapi.Request{Actor: "scheduler"}) {
		t.Fatal("scheduler actor")
	}
	if isNonInteractiveToolCaller(toolapi.Request{TaskID: "task-1", Actor: "agent"}) {
		t.Fatal("interactive chat should wait")
	}
}

type denyWaitGate struct {
	created int
}

func (g *denyWaitGate) CreatePending(context.Context, PermissionPending) (string, error) {
	g.created++
	return "should-not", nil
}

func (g *denyWaitGate) Wait(context.Context, string) (PermissionDecision, error) {
	return PermissionDecision{}, errors.New("should not wait")
}

func TestAskModeJobTaskDoesNotWaitPermission(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	stamp := now.Format(time.RFC3339Nano)
	// Minimal kernel rows for tool call insert path (will deny before insert if no grant).
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO tasks (task_id, title, objective, state, created_at, updated_at) VALUES (?, ?, ?, 'running', ?, ?)", []any{"scheduled_deadbeef", "j", "j", stamp, stamp}},
		{"INSERT INTO plans (plan_id, task_id, revision, state, scope_hash, created_at, updated_at, document) VALUES (?, ?, 1, 'running', ?, ?, ?, '{}')", []any{"plan-1", "scheduled_deadbeef", "hash-1", stamp, stamp}},
		{"INSERT INTO plan_steps (step_id, plan_id, position, title, state, effect_level, created_at, updated_at) VALUES (?, ?, 0, ?, 'running', 'R2', ?, ?)", []any{"step-1", "plan-1", "t", stamp, stamp}},
		{"INSERT INTO runs (run_id, task_id, plan_id, state, started_at, updated_at, step_id) VALUES (?, ?, ?, 'running', ?, ?, ?)", []any{"run-1", "scheduled_deadbeef", "plan-1", stamp, stamp, "step-1"}},
	} {
		if _, err := db.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
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
	gate := &denyWaitGate{}
	broker, err := NewBroker(Config{
		DB: db, Approvals: repository, Policy: policy.NewEvaluator(policy.DefaultConfig()),
		Artifacts: artifactStore, Now: func() time.Time { return now },
		PermissionMode: PermissionModeAsk, Permission: gate,
	})
	if err != nil {
		t.Fatal(err)
	}
	tool := &atomicityTestTool{}
	if err := broker.Register(tool); err != nil {
		t.Fatal(err)
	}
	_, err = broker.Execute(ctx, toolapi.Request{
		CallID: "call-job", RunID: "run-1", TaskID: "scheduled_deadbeef", PlanID: "plan-1",
		PlanHash: "hash-1", StepID: "step-1", Actor: "scheduler",
		Tool: "test_write", Arguments: json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("expected deny without grant")
	}
	if !errors.Is(err, ErrToolDenied) {
		t.Fatalf("err = %v", err)
	}
	if gate.created != 0 {
		t.Fatalf("permission gate created = %d, want 0 (job fail-closed)", gate.created)
	}
	if tool.calls != 0 {
		t.Fatalf("tool executed")
	}
	if !strings.Contains(err.Error(), "capability grant") {
		t.Fatalf("err = %v", err)
	}
}

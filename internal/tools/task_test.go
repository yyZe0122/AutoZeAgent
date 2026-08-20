package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"os"

	"github.com/yyZe0122/yunmengze-agent/internal/agent"
	"github.com/yyZe0122/yunmengze-agent/internal/providerconfig"
	"github.com/yyZe0122/yunmengze-agent/internal/runmeta"
	coresqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
	"github.com/yyZe0122/yunmengze-agent/internal/version"
	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
)

type stubSubagent struct {
	last agent.RunRequest
	err  error
	body string
}

func (s *stubSubagent) Run(_ context.Context, req agent.RunRequest) (agent.Result, error) {
	s.last = req
	if s.err != nil {
		return agent.Result{}, s.err
	}
	return agent.Result{Content: s.body, Iterations: 1}, nil
}

func TestTaskSystemPromptIncludesVersion(t *testing.T) {
	got := taskSystemPrompt()
	want := "YunmengZe Agent " + version.Version
	if !strings.Contains(got, want) {
		t.Fatalf("prompt missing %q: %s", want, got)
	}
	if !strings.Contains(got, "sub-agent") {
		t.Fatalf("prompt missing sub-agent: %s", got)
	}
}

func TestFilterChildTools(t *testing.T) {
	parent := []string{"fs_read", "task", "fs_list"}
	got := filterChildTools(parent, nil)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	got = filterChildTools(parent, []string{"fs_list", "process_exec", "fs_list"})
	if len(got) != 1 || got[0] != "fs_list" {
		t.Fatalf("subset = %v", got)
	}
}

func TestTaskToolSpawnsChildRun(t *testing.T) {
	ctx := context.Background()
	database, err := coresqlite.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO sessions(session_id,state,created_at,updated_at) VALUES(?,?,?,?)", []any{"s1", "active", stamp, stamp}},
		{"INSERT INTO tasks(task_id,session_id,title,objective,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", []any{"t1", "s1", "T", "O", "running", stamp, stamp}},
		{"INSERT INTO plans(plan_id,task_id,revision,state,scope_hash,created_at,updated_at,document) VALUES(?,?,?,?,?,?,?,?)", []any{"p1", "t1", 1, "approved", "h1", stamp, stamp, `{}`}},
		{"INSERT INTO plan_steps(step_id,plan_id,position,title,state,effect_level,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)", []any{"step1", "p1", 0, "S", "running", "R1", stamp, stamp}},
		{"INSERT INTO runs(run_id,task_id,plan_id,state,started_at,updated_at,step_id) VALUES(?,?,?,?,?,?,?)", []any{"parent-run", "t1", "p1", "running", stamp, stamp, "step1"}},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}

	stub := &stubSubagent{body: "child-done"}
	tool, err := NewTaskTool(TaskToolConfig{DB: db, Runner: stub, Now: func() time.Time {
		return time.Date(2026, 7, 31, 15, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatal(err)
	}

	parentCtx := runmeta.With(ctx, runmeta.Context{
		RunID: "parent-run", TaskID: "t1", SessionID: "s1", PlanID: "p1", PlanHash: "h1",
		StepID: "step1", Actor: "agent", TraceID: "tr",
		AllowedTools: []string{"fs_read", "task"},
		CapabilityGrantIDs: map[string][]string{
			"fs_read": {"g-read"}, "task": {"g-task"},
		},
		MaxTotalTokens: 1000, Depth: 0, CallID: "call-task-1",
	})
	args, _ := json.Marshal(map[string]any{"prompt": "summarize README"})
	out, err := tool.Execute(parentCtx, args)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["state"] != "completed" || payload["content"] != "child-done" {
		t.Fatalf("payload = %#v", payload)
	}
	runID, _ := payload["run_id"].(string)
	if runID == "" || !strings.HasPrefix(runID, "c") {
		t.Fatalf("run_id = %q", runID)
	}
	if stub.last.Depth != 1 {
		t.Fatalf("child depth = %d", stub.last.Depth)
	}
	if stub.last.RunID != runID {
		t.Fatalf("runner run_id = %s want %s", stub.last.RunID, runID)
	}
	if len(stub.last.Messages) == 0 || stub.last.Messages[0].Role != providerapi.RoleSystem {
		t.Fatalf("child messages = %#v", stub.last.Messages)
	}
	if !strings.Contains(stub.last.Messages[0].Content, "YunmengZe Agent "+version.Version) {
		t.Fatalf("child system prompt = %q", stub.last.Messages[0].Content)
	}
	var parent string
	if err := db.QueryRow(`SELECT parent_run_id FROM runs WHERE run_id = ?`, runID).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != "parent-run" {
		t.Fatalf("parent_run_id = %q", parent)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM runs WHERE run_id = ?`, runID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "completed" {
		t.Fatalf("child run state = %q", state)
	}
}

func TestTaskToolInjectsAgentsOverlay(t *testing.T) {
	ctx := context.Background()
	database, err := coresqlite.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO sessions(session_id,state,created_at,updated_at) VALUES(?,?,?,?)", []any{"s1", "active", stamp, stamp}},
		{"INSERT INTO tasks(task_id,session_id,title,objective,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", []any{"t1", "s1", "T", "O", "running", stamp, stamp}},
		{"INSERT INTO plans(plan_id,task_id,revision,state,scope_hash,created_at,updated_at,document) VALUES(?,?,?,?,?,?,?,?)", []any{"p1", "t1", 1, "approved", "h1", stamp, stamp, `{}`}},
		{"INSERT INTO plan_steps(step_id,plan_id,position,title,state,effect_level,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)", []any{"step1", "p1", 0, "S", "running", "R1", stamp, stamp}},
		{"INSERT INTO runs(run_id,task_id,plan_id,state,started_at,updated_at,step_id) VALUES(?,?,?,?,?,?,?)", []any{"parent-run", "t1", "p1", "running", stamp, stamp, "step1"}},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	cfg := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg, providerconfig.AgentsFilename), []byte("全局：子代理也要遵守"), 0o600); err != nil {
		t.Fatal(err)
	}
	stub := &stubSubagent{body: "ok"}
	tool, err := NewTaskTool(TaskToolConfig{DB: db, Runner: stub, ConfigDir: cfg, Now: func() time.Time {
		return time.Date(2026, 8, 17, 15, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatal(err)
	}
	parentCtx := runmeta.With(ctx, runmeta.Context{
		RunID: "parent-run", TaskID: "t1", SessionID: "s1", PlanID: "p1", PlanHash: "h1",
		StepID: "step1", Actor: "agent", TraceID: "tr",
		AllowedTools: []string{"fs_read", "task"}, Depth: 0, CallID: "call-agents",
	})
	args, _ := json.Marshal(map[string]any{"prompt": "do it"})
	if _, err := tool.Execute(parentCtx, args); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, m := range stub.last.Messages {
		if m.Role == providerapi.RoleSystem {
			joined += m.Content + "\n"
		}
	}
	if !strings.Contains(joined, "全局：子代理也要遵守") {
		t.Fatalf("child missing AGENTS: %q", joined)
	}
}

func TestTaskToolMaxDepth(t *testing.T) {
	ctx := context.Background()
	database, err := coresqlite.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	stub := &stubSubagent{body: "nope"}
	tool, err := NewTaskTool(TaskToolConfig{DB: database.SQL(), Runner: stub, MaxDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	parentCtx := runmeta.With(ctx, runmeta.Context{
		RunID: "p", TaskID: "t", PlanID: "pl", PlanHash: "h", StepID: "s",
		AllowedTools: []string{"task"}, Depth: 2, CallID: "c1",
	})
	out, err := tool.Execute(parentCtx, json.RawMessage(`{"prompt":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "max_depth_exceeded") {
		t.Fatalf("out = %s", out)
	}
	if stub.last.RunID != "" {
		t.Fatal("runner should not run at max depth")
	}
}

func TestTaskToolRequiresContext(t *testing.T) {
	database, err := coresqlite.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	tool, err := NewTaskTool(TaskToolConfig{DB: database.SQL(), Runner: &stubSubagent{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tool.Execute(context.Background(), json.RawMessage(`{"prompt":"x"}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

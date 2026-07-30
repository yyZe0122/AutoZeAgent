package chatsession

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"autozeagent.local/autozeagent/internal/agent"
	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
	storesqlite "autozeagent.local/autozeagent/internal/store/sqlite"
	"autozeagent.local/autozeagent/pkg/providerapi"
)

type fakeAgent struct {
	mu      sync.Mutex
	request agent.RunRequest
	done    chan struct{}
}

func (f *fakeAgent) Run(_ context.Context, request agent.RunRequest) (agent.Result, error) {
	f.mu.Lock()
	f.request = request
	f.mu.Unlock()
	close(f.done)
	return agent.Result{Content: "你好！", Iterations: 1}, nil
}

func TestStartChatSkipsPlannerShape(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	repo, err := kernel.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	approvals, err := approval.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	queries, err := corequery.New(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	session, err := repo.CreateSession(ctx, "session-chat", now)
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTaskWithSkillSnapshot(ctx, "task-chat", session.ID, "你好", "你好", nil, "", kernel.ExecutionModeAgent, now)
	if err != nil {
		t.Fatal(err)
	}

	fake := &fakeAgent{done: make(chan struct{})}
	svc, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: fake, Transcript: queries,
		WorkspaceRoots: []string{t.TempDir()}, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.StartChat(ctx, StartRequest{Task: task, Actor: "test", UserText: "你好"})
	if err != nil {
		t.Fatalf("StartChat: %v", err)
	}
	if result.RunID == "" || result.PlanID == "" {
		t.Fatalf("result = %#v", result)
	}
	// Immediately after StartChat: never planning / waiting_approval.
	mid, err := repo.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mid.State == kernel.TaskPlanning || mid.State == kernel.TaskWaitingApproval {
		t.Fatalf("chat task entered planner states: %s", mid.State)
	}
	if mid.State != kernel.TaskRunning && mid.State != kernel.TaskCompleted {
		t.Fatalf("after StartChat state = %s, want running|completed", mid.State)
	}
	var planState string
	if err := db.QueryRowContext(ctx, "SELECT state FROM plans WHERE plan_id = ?", result.PlanID).Scan(&planState); err != nil {
		t.Fatal(err)
	}
	if planState != string(kernel.PlanApproved) {
		t.Fatalf("chat plan state = %s, want approved", planState)
	}

	select {
	case <-fake.done:
	case <-time.After(3 * time.Second):
		t.Fatal("agent.Run not called")
	}

	fake.mu.Lock()
	req := fake.request
	fake.mu.Unlock()
	if len(req.Messages) < 2 {
		t.Fatalf("messages = %#v", req.Messages)
	}
	if req.Messages[0].Role != providerapi.RoleSystem {
		t.Fatalf("first message role = %s", req.Messages[0].Role)
	}
	if strings.Contains(req.Messages[0].Content, "Execute exactly one approved plan step") {
		t.Fatal("chat used plan-step system prompt")
	}
	if req.Messages[1].Role != providerapi.RoleUser || req.Messages[1].Content != "你好" {
		t.Fatalf("user message = %#v", req.Messages[1])
	}
	// Wait for complete.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := repo.GetTask(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == kernel.TaskCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, err := repo.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != kernel.TaskCompleted {
		t.Fatalf("task state = %s want completed", got.State)
	}

	msgs, err := queries.SessionTranscript(ctx, session.ID, corequery.TranscriptOptions{Page: corequery.Page{Limit: 50}})
	if err != nil {
		t.Fatal(err)
	}
	var users int
	for _, m := range msgs {
		if m.Role == "user" {
			users++
			if strings.Contains(m.Content, "Current step:") || strings.Contains(m.Content, "Task objective:") {
				t.Fatalf("ghost prompt in transcript: %#v", m)
			}
			if m.Content != "你好" {
				t.Fatalf("user content = %q", m.Content)
			}
		}
	}
	if users != 1 {
		t.Fatalf("user bubbles = %d msgs=%#v", users, msgs)
	}
}

func TestStartChatTwiceDoesNotCollideStepID(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	repo, err := kernel.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	approvals, err := approval.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	queries, err := corequery.New(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	session, err := repo.CreateSession(ctx, "session-multi", now)
	if err != nil {
		t.Fatal(err)
	}
	fake1 := &fakeAgent{done: make(chan struct{})}
	fake2 := &fakeAgent{done: make(chan struct{})}
	root := t.TempDir()
	svc1, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: fake1, Transcript: queries,
		WorkspaceRoots: []string{root}, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	task1, err := repo.CreateTaskWithSkillSnapshot(ctx, "task-a", session.ID, "hi", "hi", nil, "", kernel.ExecutionModeAgent, now)
	if err != nil {
		t.Fatal(err)
	}
	r1, err := svc1.StartChat(ctx, StartRequest{Task: task1, Actor: "test", UserText: "hi"})
	if err != nil {
		t.Fatalf("first StartChat: %v", err)
	}
	select {
	case <-fake1.done:
	case <-time.After(3 * time.Second):
		t.Fatal("first agent not called")
	}
	// Wait first task complete so second is independent.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := repo.GetTask(ctx, task1.ID)
		if got.State == kernel.TaskCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	svc2, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: fake2, Transcript: queries,
		WorkspaceRoots: []string{root}, Now: func() time.Time { return now.Add(time.Second) },
	})
	if err != nil {
		t.Fatal(err)
	}
	task2, err := repo.CreateTaskWithSkillSnapshot(ctx, "task-b", session.ID, "again", "again", nil, "", kernel.ExecutionModeAgent, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := svc2.StartChat(ctx, StartRequest{Task: task2, Actor: "test", UserText: "again"})
	if err != nil {
		t.Fatalf("second StartChat: %v", err)
	}
	if r1.PlanID == r2.PlanID {
		t.Fatalf("plan ids collided: %s", r1.PlanID)
	}
	var step1, step2 string
	if err := db.QueryRowContext(ctx, "SELECT step_id FROM plan_steps WHERE plan_id = ?", r1.PlanID).Scan(&step1); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT step_id FROM plan_steps WHERE plan_id = ?", r2.PlanID).Scan(&step2); err != nil {
		t.Fatal(err)
	}
	if step1 == step2 {
		t.Fatalf("step ids must differ across tasks: both %q", step1)
	}
	if !strings.HasPrefix(step1, "chat-step-") || !strings.HasPrefix(step2, "chat-step-") {
		t.Fatalf("steps = %q %q", step1, step2)
	}
	select {
	case <-fake2.done:
	case <-time.After(3 * time.Second):
		t.Fatal("second agent not called")
	}
}

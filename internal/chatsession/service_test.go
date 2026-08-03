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

// blockingAgent waits until ctx is canceled (ControlTask interrupt path).
type blockingAgent struct {
	started chan struct{}
}

func (b *blockingAgent) Run(ctx context.Context, _ agent.RunRequest) (agent.Result, error) {
	close(b.started)
	<-ctx.Done()
	return agent.Result{}, ctx.Err()
}

func TestStartChatInjectsSkillSnapshot(t *testing.T) {
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
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	session, err := repo.CreateSession(ctx, "session-skill", now)
	if err != nil {
		t.Fatal(err)
	}
	instructions := `<skill id="demo" name="Demo" description="d" source="user">
Use conventional commits.
</skill>
`
	task, err := repo.CreateTaskWithSkillSnapshot(ctx, "task-skill", session.ID, "hi", "hi",
		[]string{"demo"}, instructions, kernel.ExecutionModeAgent, now)
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
	if _, err := svc.StartChat(ctx, StartRequest{Task: task, Actor: "test", UserText: "hi"}); err != nil {
		t.Fatalf("StartChat: %v", err)
	}
	select {
	case <-fake.done:
	case <-time.After(3 * time.Second):
		t.Fatal("agent.Run not called")
	}
	fake.mu.Lock()
	req := fake.request
	fake.mu.Unlock()
	if len(req.Messages) != 3 {
		t.Fatalf("messages len=%d want 3: %#v", len(req.Messages), req.Messages)
	}
	if req.Messages[0].Role != providerapi.RoleSystem {
		t.Fatalf("msg0 role=%s", req.Messages[0].Role)
	}
	if req.Messages[1].Role != providerapi.RoleSystem {
		t.Fatalf("msg1 role=%s want system skill", req.Messages[1].Role)
	}
	if !strings.Contains(req.Messages[1].Content, skillSystemPreamble) {
		t.Fatalf("skill preamble missing: %q", req.Messages[1].Content)
	}
	if !strings.Contains(req.Messages[1].Content, "Use conventional commits") {
		t.Fatalf("skill body missing: %q", req.Messages[1].Content)
	}
	if req.Messages[2].Role != providerapi.RoleUser || req.Messages[2].Content != "hi" {
		t.Fatalf("user message = %#v", req.Messages[2])
	}
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

func TestInterruptCancelsActiveChatRun(t *testing.T) {
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
	session, err := repo.CreateSession(ctx, "session-interrupt", now)
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTaskWithSkillSnapshot(ctx, "task-interrupt", session.ID, "hold", "hold", nil, "", kernel.ExecutionModeAgent, now)
	if err != nil {
		t.Fatal(err)
	}
	blocker := &blockingAgent{started: make(chan struct{})}
	svc, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: blocker, Transcript: queries,
		WorkspaceRoots: []string{t.TempDir()}, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.StartChat(ctx, StartRequest{Task: task, Actor: "test", UserText: "hold"})
	if err != nil {
		t.Fatalf("StartChat: %v", err)
	}
	select {
	case <-blocker.started:
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not start")
	}

	// Simulate ControlTask cancel: transition task then interrupt chat.
	task, err = repo.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := repo.CancelTask(ctx, task.ID, task.Version, "operator cancel", now.Add(time.Second))
	if err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if cancelled.State != kernel.TaskCancelled {
		t.Fatalf("task state = %s", cancelled.State)
	}
	svc.Interrupt(task.ID)

	deadline := time.Now().Add(3 * time.Second)
	var runState string
	for time.Now().Before(deadline) {
		if err := db.QueryRowContext(ctx, "SELECT state FROM runs WHERE run_id = ?", result.RunID).Scan(&runState); err != nil {
			t.Fatal(err)
		}
		if runState == string(kernel.RunCancelled) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if runState != string(kernel.RunCancelled) {
		t.Fatalf("run state = %s, want cancelled", runState)
	}
	// Task must stay cancelled (cancelChat must not overwrite).
	got, err := repo.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != kernel.TaskCancelled {
		t.Fatalf("task state after interrupt = %s, want cancelled", got.State)
	}
}

func TestInterruptPauseLeavesRunRecoverable(t *testing.T) {
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
	session, err := repo.CreateSession(ctx, "session-pause", now)
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTaskWithSkillSnapshot(ctx, "task-pause", session.ID, "hold", "hold", nil, "", kernel.ExecutionModeAgent, now)
	if err != nil {
		t.Fatal(err)
	}
	blocker := &blockingAgent{started: make(chan struct{})}
	svc, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: blocker, Transcript: queries,
		WorkspaceRoots: []string{t.TempDir()}, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.StartChat(ctx, StartRequest{Task: task, Actor: "test", UserText: "hold"})
	if err != nil {
		t.Fatalf("StartChat: %v", err)
	}
	select {
	case <-blocker.started:
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not start")
	}
	task, err = repo.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := repo.TransitionTask(ctx, task.ID, task.Version, kernel.TaskPaused, "operator pause", now.Add(time.Second))
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if paused.State != kernel.TaskPaused {
		t.Fatalf("task = %s", paused.State)
	}
	svc.Interrupt(task.ID)

	// Wait for agent goroutine to observe cancel and exit without failing the run.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		svc.activeMu.Lock()
		_, active := svc.active[task.ID]
		svc.activeMu.Unlock()
		if !active {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	var runState string
	if err := db.QueryRowContext(ctx, "SELECT state FROM runs WHERE run_id = ?", result.RunID).Scan(&runState); err != nil {
		t.Fatal(err)
	}
	if runState != string(kernel.RunRunning) && runState != string(kernel.RunCreated) {
		t.Fatalf("paused chat run state = %s, want running|created (recoverable)", runState)
	}
	got, err := repo.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != kernel.TaskPaused {
		t.Fatalf("task state = %s, want paused", got.State)
	}
}

func TestStartChatPlanModeReadOnlyGrants(t *testing.T) {
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
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	session, err := repo.CreateSession(ctx, "session-plan-ro", now)
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTaskWithSkillSnapshot(ctx, "task-plan-ro", session.ID, "analyze", "analyze", nil, "", kernel.ExecutionModePlan, now)
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
	if _, err := svc.StartChat(ctx, StartRequest{Task: task, Actor: "test", UserText: "analyze"}); err != nil {
		t.Fatalf("StartChat: %v", err)
	}
	select {
	case <-fake.done:
	case <-time.After(3 * time.Second):
		t.Fatal("agent.Run not called")
	}
	fake.mu.Lock()
	req := fake.request
	fake.mu.Unlock()
	for _, name := range req.AllowedTools {
		switch name {
		case "fs_read", "fs_list", "fs_stat", "task":
		default:
			t.Fatalf("plan mode must not allow %q; tools=%v", name, req.AllowedTools)
		}
	}
	if len(req.AllowedTools) != 4 {
		t.Fatalf("plan tools = %v, want read/list/stat/task", req.AllowedTools)
	}
	if len(req.Messages) < 1 || req.Messages[0].Role != providerapi.RoleSystem {
		t.Fatalf("messages = %#v", req.Messages)
	}
	if !strings.Contains(req.Messages[0].Content, "plan mode") {
		t.Fatalf("system prompt missing plan mode: %q", req.Messages[0].Content)
	}
	if strings.Contains(req.Messages[0].Content, "build mode") {
		t.Fatal("plan mode used agent build prompt")
	}
	// Never planning / waiting_approval.
	got, err := repo.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State == kernel.TaskPlanning || got.State == kernel.TaskWaitingApproval {
		t.Fatalf("plan chat entered legacy planner states: %s", got.State)
	}
}

func TestStartChatAgentHighRiskToolsConfig(t *testing.T) {
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
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	session, err := repo.CreateSession(ctx, "session-hr", now)
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTaskWithSkillSnapshot(ctx, "task-hr", session.ID, "run", "run", nil, "", kernel.ExecutionModeAgent, now)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeAgent{done: make(chan struct{})}
	svc, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: fake, Transcript: queries,
		WorkspaceRoots: []string{t.TempDir()}, AllowGit: true, AllowProcess: true,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartChat(ctx, StartRequest{Task: task, Actor: "test", UserText: "run"}); err != nil {
		t.Fatalf("StartChat: %v", err)
	}
	select {
	case <-fake.done:
	case <-time.After(3 * time.Second):
		t.Fatal("agent.Run not called")
	}
	fake.mu.Lock()
	tools := append([]string(nil), fake.request.AllowedTools...)
	fake.mu.Unlock()
	want := map[string]bool{
		"git_status": true, "git_diff": true, "git_add": true, "git_commit": true,
		"process_exec": true,
	}
	for _, name := range tools {
		delete(want, name)
	}
	if len(want) > 0 {
		t.Fatalf("missing high-risk tools %v; got %v", want, tools)
	}

	// Plan mode with same flags must still deny high-risk.
	fake2 := &fakeAgent{done: make(chan struct{})}
	svc2, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: fake2, Transcript: queries,
		WorkspaceRoots: []string{t.TempDir()}, AllowGit: true, AllowProcess: true,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	planTask, err := repo.CreateTaskWithSkillSnapshot(ctx, "task-hr-plan", session.ID, "ro", "ro", nil, "", kernel.ExecutionModePlan, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc2.StartChat(ctx, StartRequest{Task: planTask, Actor: "test", UserText: "ro"}); err != nil {
		t.Fatalf("StartChat plan: %v", err)
	}
	select {
	case <-fake2.done:
	case <-time.After(3 * time.Second):
		t.Fatal("plan agent.Run not called")
	}
	fake2.mu.Lock()
	planTools := fake2.request.AllowedTools
	fake2.mu.Unlock()
	for _, name := range planTools {
		if name == "process_exec" || strings.HasPrefix(name, "git_") {
			t.Fatalf("plan mode must not allow %q; tools=%v", name, planTools)
		}
	}
}

func TestStartChatAgentModeWriteGrants(t *testing.T) {
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
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	session, err := repo.CreateSession(ctx, "session-agent-rw", now)
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTaskWithSkillSnapshot(ctx, "task-agent-rw", session.ID, "edit", "edit", nil, "", kernel.ExecutionModeAgent, now)
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
	if _, err := svc.StartChat(ctx, StartRequest{Task: task, Actor: "test", UserText: "edit"}); err != nil {
		t.Fatalf("StartChat: %v", err)
	}
	select {
	case <-fake.done:
	case <-time.After(3 * time.Second):
		t.Fatal("agent.Run not called")
	}
	fake.mu.Lock()
	req := fake.request
	fake.mu.Unlock()
	want := map[string]bool{"fs_read": true, "fs_list": true, "fs_stat": true, "fs_write": true, "fs_patch": true, "fs_mkdir": true, "task": true}
	got := map[string]bool{}
	for _, name := range req.AllowedTools {
		got[name] = true
	}
	for name := range want {
		if !got[name] {
			t.Fatalf("agent tools missing %q: %v", name, req.AllowedTools)
		}
	}
	if len(req.Messages) < 1 || !strings.Contains(req.Messages[0].Content, "build mode") {
		t.Fatalf("agent system prompt = %#v", req.Messages)
	}
}

func TestStartChatAgentWriteCeilingFalse(t *testing.T) {
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
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	session, err := repo.CreateSession(ctx, "session-ceiling", now)
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTaskWithSkillSnapshot(ctx, "task-ceiling", session.ID, "ro", "ro", nil, "", kernel.ExecutionModeAgent, now)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeAgent{done: make(chan struct{})}
	ceiling := false
	svc, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: fake, Transcript: queries,
		WorkspaceRoots: []string{t.TempDir()}, AllowWriteCeiling: &ceiling, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartChat(ctx, StartRequest{Task: task, Actor: "test", UserText: "ro"}); err != nil {
		t.Fatalf("StartChat: %v", err)
	}
	select {
	case <-fake.done:
	case <-time.After(3 * time.Second):
		t.Fatal("agent.Run not called")
	}
	fake.mu.Lock()
	req := fake.request
	fake.mu.Unlock()
	for _, name := range req.AllowedTools {
		if name == "fs_write" || name == "fs_patch" || name == "fs_mkdir" {
			t.Fatalf("write ceiling false still allowed %q: %v", name, req.AllowedTools)
		}
	}
}

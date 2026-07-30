package kernel

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	storesqlite "autozeagent.local/autozeagent/internal/store/sqlite"
)

func TestInitialPlanningTasksSkipsAgentMode(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repo, err := NewRepository(database.SQL())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	if _, err := repo.CreateSession(ctx, "session-plan-filter", now); err != nil {
		t.Fatal(err)
	}
	agentTask, err := repo.CreateTaskWithSkillSnapshot(ctx, "task-agent", "session-plan-filter", "a", "agent obj", nil, "", ExecutionModeAgent, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.TransitionTask(ctx, agentTask.ID, agentTask.Version, TaskPlanning, "chat", now); err != nil {
		t.Fatal(err)
	}
	planTask, err := repo.CreateTaskWithSkillSnapshot(ctx, "task-plan", "session-plan-filter", "p", "plan obj", nil, "", ExecutionModePlan, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.TransitionTask(ctx, planTask.ID, planTask.Version, TaskPlanning, "plan", now); err != nil {
		t.Fatal(err)
	}

	tasks, err := repo.InitialPlanningTasks(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != planTask.ID {
		t.Fatalf("InitialPlanningTasks = %+v, want only plan-mode task", tasks)
	}
}

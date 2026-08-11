package taskcontrol

import (
	"context"
	"testing"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/applicationerror"
	"github.com/yyZe0122/yunmengze-agent/internal/approval"
	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	storesqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
)

type chatInterruptStub struct {
	calls []kernel.TaskID
}

func (c *chatInterruptStub) Interrupt(id kernel.TaskID) {
	c.calls = append(c.calls, id)
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
	if _, err := db.ExecContext(ctx, "INSERT INTO tasks (task_id, session_id, title, objective, state, execution_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", "task-1", "session-1", "test", "test", kernel.TaskRunning, kernel.ExecutionModeAgent, stamp, stamp); err != nil {
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
	chatStub := &chatInterruptStub{}
	service, err := New(Config{
		DB: db, Repository: repository, Approvals: approvalRepository, Chat: chatStub,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}

	paused, err := service.ControlTask(ctx, TaskActionRequest{TaskID: "task-1", ExpectedVersion: 1, Action: TaskActionPause, Reason: "operator pause"})
	if err != nil {
		t.Fatalf("pause task: %v", err)
	}
	if paused.State != kernel.TaskPaused || paused.Version != 2 {
		t.Fatalf("paused task = %+v", paused)
	}
	if len(chatStub.calls) != 1 || chatStub.calls[0] != "task-1" {
		t.Fatalf("chat interrupt on pause = %v, want [task-1]", chatStub.calls)
	}

	resumed, err := service.ControlTask(ctx, TaskActionRequest{TaskID: "task-1", ExpectedVersion: paused.Version, Action: TaskActionResume})
	if err != nil {
		t.Fatalf("resume task: %v", err)
	}
	if resumed.State != kernel.TaskRunning || resumed.Version != 3 {
		t.Fatalf("resumed task = %+v", resumed)
	}
	if len(chatStub.calls) != 1 {
		t.Fatalf("resume should not interrupt chat: calls=%v", chatStub.calls)
	}

	cancelled, err := service.ControlTask(ctx, TaskActionRequest{TaskID: "task-1", ExpectedVersion: resumed.Version, Action: TaskActionCancel})
	if err != nil {
		t.Fatalf("cancel task: %v", err)
	}
	if cancelled.State != kernel.TaskCancelled || cancelled.Version != 4 {
		t.Fatalf("cancelled task = %+v", cancelled)
	}
	if len(chatStub.calls) != 2 || chatStub.calls[1] != "task-1" {
		t.Fatalf("chat interrupt on cancel = %v, want two task-1 calls", chatStub.calls)
	}

	_, err = service.ControlTask(ctx, TaskActionRequest{TaskID: "task-1", ExpectedVersion: cancelled.Version, Action: TaskActionResume})
	if !applicationerror.IsCode(err, applicationerror.CodeConflict) {
		t.Fatalf("resume cancelled task error = %v, want conflict", err)
	}
}

func TestNewRequiresDeps(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}
}

package tasksubmission

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"autozeagent.local/autozeagent/internal/applicationerror"
	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/kernel"
)

// TestSubmitReturnsBeforeSlowPlanner ensures HTTP/path clients are not blocked
// on provider latency (async PlanTask after created→planning).
func TestSubmitReturnsBeforeSlowPlanner(t *testing.T) {
	repo := &memRepo{}
	planner := &slowPlanner{delay: 200 * time.Millisecond}
	svc, err := New(Config{
		Repository: repo,
		Planner:    planner,
		Now:        func() time.Time { return time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC) },
		NewID: func(prefix string) (string, error) {
			return prefix + "test", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	result, err := svc.Submit(context.Background(), Request{
		Title: "t", Objective: "do it", EnsureSession: true,
		ExecutionMode: kernel.ExecutionModePlan,
	})
	elapsed := time.Since(start)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("Submit blocked for %v; want async return", elapsed)
	}
	if !errors.Is(err, ErrPlanning) {
		t.Fatalf("err = %v, want ErrPlanning", err)
	}
	if !applicationerror.IsCode(err, applicationerror.CodePlanningPending) {
		t.Fatalf("code not PlanningPending: %v", err)
	}
	if result.Task.State != kernel.TaskPlanning {
		t.Fatalf("state = %q, want planning", result.Task.State)
	}
	// Let the async planner finish without leaking the goroutine into other tests.
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&planner.calls) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("async PlanTask never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(planner.delay + 50*time.Millisecond)
}

type memRepo struct {
	mu       sync.Mutex
	sessions map[kernel.SessionID]kernel.Session
	tasks    map[kernel.TaskID]kernel.Task
}

func (r *memRepo) ensure() {
	if r.sessions == nil {
		r.sessions = map[kernel.SessionID]kernel.Session{}
	}
	if r.tasks == nil {
		r.tasks = map[kernel.TaskID]kernel.Task{}
	}
}

func (r *memRepo) CreateSession(_ context.Context, id kernel.SessionID, now time.Time) (kernel.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensure()
	s := kernel.Session{ID: id, State: kernel.SessionActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	r.sessions[id] = s
	return s, nil
}

func (r *memRepo) GetSession(_ context.Context, id kernel.SessionID) (kernel.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensure()
	s, ok := r.sessions[id]
	if !ok {
		return kernel.Session{}, kernel.ErrNotFound
	}
	return s, nil
}

func (r *memRepo) CreateTaskWithSkillSnapshot(_ context.Context, id kernel.TaskID, sessionID kernel.SessionID, title, objective string, _ []string, _ string, mode kernel.ExecutionMode, now time.Time) (kernel.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensure()
	task, err := kernel.NewTaskWithMode(id, sessionID, title, objective, mode, now)
	if err != nil {
		return kernel.Task{}, err
	}
	r.tasks[id] = task
	return task, nil
}

func (r *memRepo) GetTask(_ context.Context, id kernel.TaskID) (kernel.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensure()
	task, ok := r.tasks[id]
	if !ok {
		return kernel.Task{}, kernel.ErrNotFound
	}
	return task, nil
}

func (r *memRepo) GetTaskSkillSnapshot(context.Context, kernel.TaskID) (kernel.TaskSkillSnapshot, error) {
	return kernel.TaskSkillSnapshot{}, nil
}

func (r *memRepo) TransitionTask(_ context.Context, id kernel.TaskID, expected uint64, to kernel.TaskState, _ string, now time.Time) (kernel.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ensure()
	task, ok := r.tasks[id]
	if !ok {
		return kernel.Task{}, kernel.ErrNotFound
	}
	if task.Version != expected {
		return task, kernel.ErrVersionConflict
	}
	if err := task.Transition(to, now); err != nil {
		return task, err
	}
	r.tasks[id] = task
	return task, nil
}

type slowPlanner struct {
	delay time.Duration
	calls int32
}

func (p *slowPlanner) PlanTask(ctx context.Context, task kernel.Task, planID kernel.PlanID, revision uint64) (kernel.Task, approval.PlanDocument, error) {
	atomic.AddInt32(&p.calls, 1)
	select {
	case <-ctx.Done():
		return task, approval.PlanDocument{}, ctx.Err()
	case <-time.After(p.delay):
	}
	return task, approval.PlanDocument{PlanID: planID, TaskID: task.ID, Revision: revision}, nil
}

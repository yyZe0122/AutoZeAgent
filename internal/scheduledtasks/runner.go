// Package scheduledtasks transfers Scheduler task requests into the mandatory
// Kernel without allowing Scheduler to create plans, approvals, grants, or tool
// calls.
package scheduledtasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/tasksubmission"
	"autozeagent.local/autozeagent/pkg/schedulerapi"
)

type Client interface {
	ClaimScheduledTasks(context.Context, schedulerapi.ClaimDueRequest) ([]schedulerapi.TaskRequest, error)
	AcknowledgeScheduledTask(context.Context, schedulerapi.AcknowledgeRequest) error
}

type TaskSubmitter interface {
	Submit(context.Context, tasksubmission.Request) (tasksubmission.Result, error)
}

type Config struct {
	Client       Client
	Submissions  TaskSubmitter
	PollInterval time.Duration
	Owner        string
	Limit        int
	LeaseSeconds int64
	OnError      func(error)
}

type Runner struct {
	client       Client
	submissions  TaskSubmitter
	pollInterval time.Duration
	owner        string
	limit        int
	leaseSeconds int64
	onError      func(error)
}

func New(config Config) (*Runner, error) {
	if config.Client == nil || config.Submissions == nil {
		return nil, errors.New("scheduled task client and submission service are required")
	}
	config.Owner = strings.TrimSpace(config.Owner)
	if config.Owner == "" {
		return nil, errors.New("scheduled task owner is required")
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 5 * time.Second
	}
	if config.Limit <= 0 || config.Limit > 100 {
		config.Limit = 10
	}
	if config.LeaseSeconds <= 0 || config.LeaseSeconds > 3600 {
		config.LeaseSeconds = 60
	}
	return &Runner{
		client: config.Client, submissions: config.Submissions, pollInterval: config.PollInterval,
		owner: config.Owner, limit: config.Limit, leaseSeconds: config.LeaseSeconds,
		onError: config.OnError,
	}, nil
}

// Run polls immediately, then at the configured interval. Capability or module
// failures are additive degradation: Core keeps running and retries later.
func (r *Runner) Run(ctx context.Context) {
	if ctx == nil {
		return
	}
	r.runOnceAndReport(ctx)
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOnceAndReport(ctx)
		}
	}
}

func (r *Runner) runOnceAndReport(ctx context.Context) {
	if err := r.RunOnce(ctx); err != nil && r.onError != nil && !errors.Is(err, context.Canceled) {
		r.onError(err)
	}
}

// RunOnce is exposed for deterministic bootstrap checks and tests.
func (r *Runner) RunOnce(ctx context.Context) error {
	if ctx == nil {
		return errors.New("scheduled task poll context is required")
	}
	tasks, err := r.client.ClaimScheduledTasks(ctx, schedulerapi.ClaimDueRequest{
		Owner: r.owner, Limit: r.limit, LeaseSeconds: r.leaseSeconds,
	})
	if err != nil {
		return err
	}
	var result []error
	for _, request := range tasks {
		if err := r.accept(ctx, request); err != nil {
			result = append(result, err)
		}
	}
	return errors.Join(result...)
}

func (r *Runner) accept(ctx context.Context, request schedulerapi.TaskRequest) error {
	if !request.RequiresPlan {
		err := errors.New("scheduled task rejected because requires_plan is false")
		ackErr := r.client.AcknowledgeScheduledTask(ctx, schedulerapi.AcknowledgeRequest{
			RunID: request.RunID, LeaseID: request.LeaseID, Status: "cancelled", Error: err.Error(),
		})
		return errors.Join(err, ackErr)
	}
	taskID := taskIDFor(request.IdempotencyKey)
	result, err := r.submissions.Submit(ctx, tasksubmission.Request{
		TaskID: taskID, SessionID: kernel.SessionID(request.SessionID), PlanID: planIDFor(taskID, 1),
		Title: request.Title, Objective: request.Objective, EnsureSession: false, AllowExisting: true,
	})
	if err != nil {
		if errors.Is(err, tasksubmission.ErrPlanning) {
			return fmt.Errorf("plan scheduled Core task %s: %w", result.Task.ID, err)
		}
		ackErr := r.client.AcknowledgeScheduledTask(ctx, schedulerapi.AcknowledgeRequest{
			RunID: request.RunID, LeaseID: request.LeaseID, Status: "failed", Error: err.Error(),
		})
		return errors.Join(fmt.Errorf("submit scheduled Core task: %w", err), ackErr)
	}
	task := result.Task
	status := "task_created"
	if task.State == kernel.TaskWaitingApproval {
		status = "waiting_approval"
	}
	if err := r.client.AcknowledgeScheduledTask(ctx, schedulerapi.AcknowledgeRequest{
		RunID: request.RunID, LeaseID: request.LeaseID, CoreTaskID: string(task.ID), Status: status,
	}); err != nil {
		return fmt.Errorf("acknowledge scheduled Core task %s: %w", task.ID, err)
	}
	return nil
}

func taskIDFor(idempotencyKey string) kernel.TaskID {
	digest := sha256.Sum256([]byte(strings.TrimSpace(idempotencyKey)))
	return kernel.TaskID("scheduled_" + hex.EncodeToString(digest[:16]))
}

func planIDFor(taskID kernel.TaskID, revision uint64) kernel.PlanID {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", strings.TrimSpace(string(taskID)), revision)))
	return kernel.PlanID("scheduled_plan_" + hex.EncodeToString(digest[:16]))
}

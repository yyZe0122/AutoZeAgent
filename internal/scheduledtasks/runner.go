// Package scheduledtasks transfers due Scheduler jobs into Core via tasksubmission.
// It never creates approvals, grants, agent runs, or tool calls.
package scheduledtasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	"github.com/yyZe0122/yunmengze-agent/internal/runlog"
	"github.com/yyZe0122/yunmengze-agent/internal/tasksubmission"
	"github.com/yyZe0122/yunmengze-agent/pkg/schedulerapi"
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

// Run polls immediately, then at the configured interval.
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
	mode := kernel.NormalizeExecutionMode(request.ExecutionMode)
	if !mode.Valid() {
		mode = kernel.ExecutionModeAgent
	}
	taskID := taskIDFor(request.IdempotencyKey)
	ids := runlog.IDs{
		SessionID: request.SessionID,
		TaskID:    string(taskID),
	}
	slog.Info("scheduled task submit started", runlog.Attrs("scheduledtasks", "accept", "started", ids,
		"job_id", request.JobID, "job_run_id", request.RunID, "execution_mode", string(mode))...)
	result, err := r.submissions.Submit(ctx, tasksubmission.Request{
		TaskID:        taskID,
		SessionID:     kernel.SessionID(request.SessionID),
		Title:         request.Title,
		Objective:     request.Objective,
		SkillIDs:      append([]string(nil), request.SkillIDs...),
		ExecutionMode: mode,
		EnsureSession: false,
		AllowExisting: true,
	})
	if err != nil {
		slog.Error("scheduled task submit failed", runlog.Attrs("scheduledtasks", "accept", "failed", ids,
			"job_run_id", request.RunID, "error", err)...)
		ackErr := r.client.AcknowledgeScheduledTask(ctx, schedulerapi.AcknowledgeRequest{
			RunID: request.RunID, LeaseID: request.LeaseID, Status: "failed", Error: err.Error(),
		})
		return errors.Join(fmt.Errorf("submit scheduled chat task: %w", err), ackErr)
	}
	if err := r.client.AcknowledgeScheduledTask(ctx, schedulerapi.AcknowledgeRequest{
		RunID: request.RunID, LeaseID: request.LeaseID, CoreTaskID: string(result.Task.ID), Status: "task_created",
	}); err != nil {
		slog.Error("scheduled task ack failed", runlog.Attrs("scheduledtasks", "ack", "failed", runlog.IDs{
			SessionID: string(result.Task.SessionID), TaskID: string(result.Task.ID), RunID: string(result.RunID),
		}, "job_run_id", request.RunID, "error", err)...)
		return fmt.Errorf("acknowledge scheduled Core task %s: %w", result.Task.ID, err)
	}
	slog.Info("scheduled task accepted", runlog.Attrs("scheduledtasks", "accept", "succeeded", runlog.IDs{
		SessionID: string(result.Task.SessionID), TaskID: string(result.Task.ID), RunID: string(result.RunID),
		PlanID: string(result.PlanID),
	}, "job_run_id", request.RunID)...)
	return nil
}

func taskIDFor(idempotencyKey string) kernel.TaskID {
	digest := sha256.Sum256([]byte(strings.TrimSpace(idempotencyKey)))
	return kernel.TaskID("scheduled_" + hex.EncodeToString(digest[:16]))
}

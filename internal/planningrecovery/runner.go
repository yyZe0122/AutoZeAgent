// Package planningrecovery retries initial Core tasks whose planning attempt did
// not persist a plan. It does not own replanning of tasks that already have a
// plan revision.
package planningrecovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/kernel"
)

type Repository interface {
	InitialPlanningTasks(context.Context, int) ([]kernel.Task, error)
}

type Planner interface {
	PlanTask(context.Context, kernel.Task, kernel.PlanID, uint64) (kernel.Task, approval.PlanDocument, error)
}

type Config struct {
	Repository   Repository
	Planner      Planner
	PollInterval time.Duration
	Limit        int
	OnError      func(error)
}

type Runner struct {
	repository   Repository
	planner      Planner
	pollInterval time.Duration
	limit        int
	onError      func(error)
}

func New(config Config) (*Runner, error) {
	if config.Repository == nil || config.Planner == nil {
		return nil, errors.New("planning recovery repository and planner are required")
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 30 * time.Second
	}
	if config.Limit <= 0 || config.Limit > 100 {
		config.Limit = 20
	}
	return &Runner{
		repository:   config.Repository,
		planner:      config.Planner,
		pollInterval: config.PollInterval,
		limit:        config.Limit,
		onError:      config.OnError,
	}, nil
}

// Run retries immediately at startup and then at a bounded interval. Provider
// failures leave the task in planning so a later pass can retry safely.
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

// RunOnce is exposed for deterministic startup checks and tests.
func (r *Runner) RunOnce(ctx context.Context) error {
	if ctx == nil {
		return errors.New("planning recovery context is required")
	}
	tasks, err := r.repository.InitialPlanningTasks(ctx, r.limit)
	if err != nil {
		return err
	}
	var result []error
	for _, task := range tasks {
		_, _, err := r.planner.PlanTask(ctx, task, planIDFor(task.ID), 1)
		if err == nil || errors.Is(err, kernel.ErrVersionConflict) {
			continue
		}
		result = append(result, fmt.Errorf("recover planning task %s: %w", task.ID, err))
	}
	return errors.Join(result...)
}

func planIDFor(taskID kernel.TaskID) kernel.PlanID {
	digest := sha256.Sum256([]byte(taskID))
	return kernel.PlanID("recovered_" + hex.EncodeToString(digest[:16]))
}

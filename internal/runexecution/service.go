// Package runexecution starts and resumes approved plan-step Agent runs.
package runexecution

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"autozeagent.local/autozeagent/internal/agent"
	"autozeagent.local/autozeagent/internal/applicationerror"
	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/audit"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/pkg/providerapi"
)

var (
	ErrInvalidRequest = errors.New("invalid run request")
	ErrNotApproved    = errors.New("plan is not approved")
	ErrInvalidState   = errors.New("plan or task is not executable")
)

type AgentRunner interface {
	Run(context.Context, agent.RunRequest) (agent.Result, error)
}

type PlanLoader interface {
	LoadPlanDocument(context.Context, kernel.PlanID) (corequery.StoredPlanDocument, error)
}

type Config struct {
	DB           *sql.DB
	Plans        PlanLoader
	Approvals    *approval.Repository
	Repository   *kernel.Repository
	Agent        AgentRunner
	PollInterval time.Duration
	Now          func() time.Time
	OnError      func(error)
}

type Service struct {
	db           *sql.DB
	plans        PlanLoader
	approvals    *approval.Repository
	repository   *kernel.Repository
	agent        AgentRunner
	audit        *audit.Store
	pollInterval time.Duration
	now          func() time.Time
	onError      func(error)
	activeMu     sync.Mutex
	activeTask   kernel.TaskID
	activeCancel context.CancelFunc
}

type TaskAction string

const (
	TaskActionPause  TaskAction = "pause"
	TaskActionResume TaskAction = "resume"
	TaskActionCancel TaskAction = "cancel"
)

type TaskActionRequest struct {
	TaskID          kernel.TaskID `json:"-"`
	ExpectedVersion uint64        `json:"expected_version"`
	Action          TaskAction    `json:"action"`
	Reason          string        `json:"reason,omitempty"`
}

type StartRequest struct {
	TaskID       kernel.TaskID `json:"task_id"`
	PlanID       kernel.PlanID `json:"plan_id"`
	PlanRevision uint64        `json:"plan_revision"`
	PlanHash     string        `json:"plan_hash"`
	Actor        string        `json:"-"`
	TraceID      string        `json:"-"`
}

type StartResult struct {
	TaskID kernel.TaskID  `json:"task_id"`
	PlanID kernel.PlanID  `json:"plan_id"`
	RunIDs []kernel.RunID `json:"run_ids"`
}

type execution struct {
	RunID         kernel.RunID
	TaskID        kernel.TaskID
	SessionID     kernel.SessionID
	PlanID        kernel.PlanID
	StepID        kernel.StepID
	RunState      kernel.RunState
	TaskObjective string
	PlanHash      string
	PlanDocument  []byte
}

func New(config Config) (*Service, error) {
	if config.DB == nil || config.Plans == nil || config.Approvals == nil || config.Repository == nil || config.Agent == nil {
		return nil, errors.New("run database, plan loader, approval repository, kernel repository, and agent are required")
	}
	auditStore, err := audit.NewStore(config.DB)
	if err != nil {
		return nil, err
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 500 * time.Millisecond
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.OnError == nil {
		config.OnError = func(error) {}
	}
	return &Service{
		db: config.DB, plans: config.Plans, approvals: config.Approvals, repository: config.Repository, agent: config.Agent,
		audit: auditStore, pollInterval: config.PollInterval, now: config.Now, onError: config.OnError,
	}, nil
}

// ControlTask applies the lifecycle controls needed by a long-running local
// Agent. Pause and cancel also stop the active Provider or Tool context; the
// durable Run and Agent records remain available for resume or inspection.
func (s *Service) ControlTask(ctx context.Context, request TaskActionRequest) (kernel.Task, error) {
	if ctx == nil {
		return kernel.Task{}, errors.New("task action context is required")
	}
	request.TaskID = kernel.TaskID(strings.TrimSpace(string(request.TaskID)))
	request.Reason = strings.TrimSpace(request.Reason)
	if request.TaskID == "" || request.ExpectedVersion == 0 {
		return kernel.Task{}, applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, fmt.Errorf("%w: task and expected version are required", ErrInvalidRequest))
	}

	var (
		task kernel.Task
		err  error
	)
	switch request.Action {
	case TaskActionPause:
		task, err = s.repository.TransitionTask(ctx, request.TaskID, request.ExpectedVersion, kernel.TaskPaused, request.Reason, s.now())
	case TaskActionResume:
		task, err = s.repository.TransitionTask(ctx, request.TaskID, request.ExpectedVersion, kernel.TaskRunning, request.Reason, s.now())
	case TaskActionCancel:
		task, err = s.repository.CancelTask(ctx, request.TaskID, request.ExpectedVersion, request.Reason, s.now())
	default:
		return kernel.Task{}, applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, fmt.Errorf("%w: unsupported task action %q", ErrInvalidRequest, request.Action))
	}
	if err != nil {
		return kernel.Task{}, classifyTaskActionError(err)
	}
	if request.Action == TaskActionPause || request.Action == TaskActionCancel {
		s.interrupt(request.TaskID)
	}
	if request.Action == TaskActionCancel {
		if err := s.approvals.RevokeTaskGrants(context.WithoutCancel(ctx), request.TaskID, s.now()); err != nil {
			return task, err
		}
	}
	return task, nil
}

// Start validates the canonical plan and current plan-level approval, persists
// one deterministic Run per step, and returns without waiting for Provider work.
func (s *Service) Start(ctx context.Context, request StartRequest) (StartResult, error) {
	if ctx == nil {
		return StartResult{}, errors.New("run start context is required")
	}
	request.TaskID = kernel.TaskID(strings.TrimSpace(string(request.TaskID)))
	request.PlanID = kernel.PlanID(strings.TrimSpace(string(request.PlanID)))
	request.PlanHash = strings.TrimSpace(request.PlanHash)
	request.Actor = strings.TrimSpace(request.Actor)
	if request.Actor == "" {
		request.Actor = "local-user"
	}
	if request.TaskID == "" || request.PlanID == "" || request.PlanRevision == 0 || request.PlanHash == "" {
		return StartResult{}, applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, fmt.Errorf("%w: task, plan, revision, and hash are required", ErrInvalidRequest))
	}
	// Plan mode means "plan first, human approve, then Start"; grants + Broker still fail-closed.
	stored, err := s.plans.LoadPlanDocument(ctx, request.PlanID)
	if err != nil {
		return StartResult{}, classifyStartError(err)
	}
	plan, err := validateStoredPlan(request, stored)
	if err != nil {
		return StartResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StartResult{}, fmt.Errorf("begin run start: %w", err)
	}
	defer tx.Rollback()
	existing, err := listRunIDs(ctx, tx, plan.PlanID)
	if err != nil {
		return StartResult{}, err
	}
	if len(existing) != 0 {
		if err := tx.Commit(); err != nil {
			return StartResult{}, fmt.Errorf("commit existing run lookup: %w", err)
		}
		return StartResult{TaskID: plan.TaskID, PlanID: plan.PlanID, RunIDs: existing}, nil
	}
	approved, err := loadCurrentPlanApproval(ctx, tx, plan, s.now())
	if err != nil {
		return StartResult{}, classifyStartError(err)
	}
	if err := requireExecutableStates(ctx, tx, plan); err != nil {
		return StartResult{}, classifyStartError(err)
	}
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, "UPDATE plans SET state = ?, version = version + 1, updated_at = ? WHERE plan_id = ? AND state = ?",
		kernel.PlanApproved, formatTime(now), plan.PlanID, kernel.PlanWaitingApproval,
	); err != nil {
		return StartResult{}, fmt.Errorf("approve plan for run: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET state = ?, version = version + 1, updated_at = ? WHERE task_id = ? AND state IN (?, ?)",
		kernel.TaskRunning, formatTime(now), plan.TaskID, kernel.TaskWaitingApproval, kernel.TaskApproved,
	); err != nil {
		return StartResult{}, fmt.Errorf("start task execution: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE plan_steps SET state = ?, version = version + 1, updated_at = ? WHERE plan_id = ? AND state = ?",
		kernel.StepApproved, formatTime(now), plan.PlanID, kernel.StepPending,
	); err != nil {
		return StartResult{}, fmt.Errorf("approve plan steps: %w", err)
	}

	runIDs := make([]kernel.RunID, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		runID := deterministicRunID(plan.PlanID, step.StepID)
		if _, err := tx.ExecContext(ctx, "INSERT INTO runs (run_id, task_id, plan_id, state, started_at, finished_at, error, version, updated_at, step_id) VALUES (?, ?, ?, ?, ?, NULL, NULL, 1, ?, ?)",
			runID, plan.TaskID, plan.PlanID, kernel.RunCreated, formatTime(now), formatTime(now), step.StepID,
		); err != nil {
			return StartResult{}, fmt.Errorf("create run for step %s: %w", step.StepID, err)
		}
		runIDs = append(runIDs, runID)
	}
	if err := s.audit.RecordTx(ctx, tx, audit.Entry{
		OccurredAt: now, Actor: request.Actor, Action: "run.start", ResourceType: "plan", ResourceID: string(plan.PlanID),
		Outcome: "accepted", TraceID: request.TraceID,
		Details: map[string]any{"task_id": plan.TaskID, "plan_revision": plan.Revision, "approval_id": approved.ID, "run_count": len(runIDs)},
	}); err != nil {
		return StartResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return StartResult{}, fmt.Errorf("commit run start: %w", err)
	}
	slog.Info("run request accepted", "component", "run", "operation", "start", "result", "accepted", "task_id", plan.TaskID, "plan_id", plan.PlanID, "run_count", len(runIDs))
	return StartResult{TaskID: plan.TaskID, PlanID: plan.PlanID, RunIDs: runIDs}, nil
}

func (s *Service) Run(ctx context.Context) {
	if ctx == nil {
		return
	}
	s.runOnceAndReport(ctx)
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnceAndReport(ctx)
		}
	}
}

func (s *Service) runOnceAndReport(ctx context.Context) {
	if err := s.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.onError(err)
	}
}

// RunOnce executes at most one eligible step so a single daemon worker keeps
// step order deterministic and recovery simple.
func (s *Service) RunOnce(ctx context.Context) error {
	if ctx == nil {
		return errors.New("run execution context is required")
	}
	item, err := s.nextExecution(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if item.RunState == kernel.RunCreated {
		claimed, err := s.claim(ctx, item)
		if err != nil {
			return err
		}
		if !claimed {
			return nil
		}
		item.RunState = kernel.RunRunning
	}
	plan, step, err := executionPlan(item)
	if err != nil {
		return s.fail(ctx, item, err)
	}
	maxTokens, maxCostMicros, err := s.remainingPlanBudget(ctx, item, plan)
	if err != nil {
		return s.fail(ctx, item, err)
	}
	// Plan remaining wall-clock only. Step timeout_ms is tool grant wall time, not provider stream budget.
	executionTimeout, err := s.executionTimeout(ctx, item.PlanID, plan)
	if err != nil {
		return s.fail(ctx, item, err)
	}
	grantIDs, err := s.issueGrants(ctx, plan, step)
	if err != nil {
		return s.fail(ctx, item, err)
	}
	toolTimeoutMS := toolTimeoutMillis(step)
	messages := executionMessages(item.TaskObjective, plan, step)
	executionContext, cancel := context.WithTimeout(ctx, executionTimeout)
	s.setActive(item.TaskID, cancel)
	slog.Info("run execution started",
		"component", "run", "operation", "execute", "result", "started",
		"run_id", item.RunID, "task_id", item.TaskID, "plan_id", item.PlanID, "step_id", item.StepID, "trace_id", item.RunID,
		"execution_timeout_ms", executionTimeout.Milliseconds(),
		"tool_timeout_ms", toolTimeoutMS,
		"allowed_tools", allowedTools(step),
		"max_tokens", maxTokens,
	)
	result, err := s.agent.Run(executionContext, agent.RunRequest{
		RunID: string(item.RunID), TaskID: string(item.TaskID), SessionID: string(item.SessionID),
		PlanID: string(item.PlanID), PlanHash: item.PlanHash,
		StepID: string(item.StepID), Actor: "agent", TraceID: string(item.RunID), Messages: messages,
		AllowedTools: allowedTools(step), CapabilityGrantIDs: grantIDs,
		MaxOutputTokens: maxTokens, MaxTotalTokens: maxTokens,
		MaxCostMicros: maxCostMicros, ToolTimeoutMillis: toolTimeoutMS,
	})
	s.clearActive(item.TaskID)
	cancel()
	if err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			slog.Info("run execution paused", "component", "run", "operation", "execute", "result", "paused", "run_id", item.RunID, "task_id", item.TaskID, "plan_id", item.PlanID, "step_id", item.StepID, "trace_id", item.RunID)
			return ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			state, stateErr := s.taskState(context.WithoutCancel(ctx), item.TaskID)
			if stateErr == nil && (state == kernel.TaskPaused || state == kernel.TaskCancelled) {
				slog.Info("run execution interrupted", "component", "run", "operation", "execute", "result", state, "run_id", item.RunID, "task_id", item.TaskID, "plan_id", item.PlanID, "step_id", item.StepID, "trace_id", item.RunID)
				return nil
			}
			if stateErr != nil {
				return s.fail(ctx, item, errors.Join(err, stateErr))
			}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("plan execution budget exceeded (execution_timeout_ms=%d tool_timeout_ms=%d): %w",
				executionTimeout.Milliseconds(), toolTimeoutMS, err)
		}
		return s.fail(ctx, item, err)
	}
	return s.complete(ctx, item, result)
}

func (s *Service) remainingPlanBudget(ctx context.Context, item execution, plan approval.PlanDocument) (int64, int64, error) {
	var usedTokens, usedCostMicros int64
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CAST(COALESCE(json_extract(a.usage, '$.total_tokens'), 0) AS INTEGER)), 0),
			COALESCE(SUM(CAST(COALESCE(json_extract(a.usage, '$.cost.micros'), 0) AS INTEGER)), 0)
		FROM agent_run_records a
		JOIN runs r ON r.run_id = a.run_id
		WHERE r.plan_id = ? AND r.run_id <> ? AND a.record_type = 'assistant_message'`,
		item.PlanID, item.RunID,
	).Scan(&usedTokens, &usedCostMicros)
	if err != nil {
		return 0, 0, fmt.Errorf("load plan usage: %w", err)
	}
	maxTokens := plan.Budget.MaxTokens - usedTokens
	if maxTokens <= 0 {
		return 0, 0, agent.ErrTokenBudgetExceeded
	}
	maxCostMicros := plan.Budget.MaxCostMicros
	if maxCostMicros > 0 {
		maxCostMicros -= usedCostMicros
		if maxCostMicros <= 0 {
			return 0, 0, agent.ErrCostBudgetExceeded
		}
	}
	return maxTokens, maxCostMicros, nil
}

// executionTimeout is remaining plan wall-clock for the full agent loop (provider + tools).
// Step timeout_ms is not applied here; it bounds each tool call via ToolTimeoutMillis / grants.
func (s *Service) executionTimeout(ctx context.Context, planID kernel.PlanID, plan approval.PlanDocument) (time.Duration, error) {
	var startedAt string
	if err := s.db.QueryRowContext(ctx, "SELECT MIN(started_at) FROM runs WHERE plan_id = ?", planID).Scan(&startedAt); err != nil {
		return 0, fmt.Errorf("load plan start time: %w", err)
	}
	started, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return 0, fmt.Errorf("parse plan start time: %w", err)
	}
	remaining := time.Duration(plan.Budget.MaxDurationMillis)*time.Millisecond - s.now().UTC().Sub(started.UTC())
	if remaining <= 0 {
		return 0, fmt.Errorf("plan duration budget exhausted: %w", context.DeadlineExceeded)
	}
	return remaining, nil
}

func (s *Service) nextExecution(ctx context.Context) (execution, error) {
	var item execution
	var runID, taskID, planID, stepID, state, document string
	var sessionID sql.NullString
	// Skip session-chat runs (step_id chat-step / chat-step-*); chatsession owns those.
	// Also skip agent-mode tasks: agent execution is StartChat only, not this worker.
	err := s.db.QueryRowContext(ctx, "SELECT r.run_id, r.task_id, r.plan_id, r.step_id, r.state, t.objective, t.session_id, p.scope_hash, p.document "+
		"FROM runs r JOIN tasks t ON t.task_id = r.task_id JOIN plans p ON p.plan_id = r.plan_id "+
		"JOIN plan_steps s ON s.step_id = r.step_id "+
		"WHERE r.state IN (?, ?) AND t.state = ? AND t.execution_mode = ? "+
		"AND r.step_id NOT LIKE ? AND NOT EXISTS ("+
		"SELECT 1 FROM runs earlier JOIN plan_steps es ON es.step_id = earlier.step_id "+
		"WHERE earlier.plan_id = r.plan_id AND es.position < s.position AND earlier.state <> ?) "+
		"ORDER BY r.started_at, s.position, r.run_id LIMIT 1",
		kernel.RunCreated, kernel.RunRunning, kernel.TaskRunning, kernel.ExecutionModePlan,
		"chat-step%", kernel.RunCompleted,
	).Scan(&runID, &taskID, &planID, &stepID, &state, &item.TaskObjective, &sessionID, &item.PlanHash, &document)
	if err != nil {
		return execution{}, err
	}
	item.RunID = kernel.RunID(runID)
	item.TaskID = kernel.TaskID(taskID)
	item.PlanID = kernel.PlanID(planID)
	item.StepID = kernel.StepID(stepID)
	item.RunState = kernel.RunState(state)
	item.PlanDocument = []byte(document)
	if sessionID.Valid {
		item.SessionID = kernel.SessionID(sessionID.String)
	}
	return item, nil
}

func (s *Service) claim(ctx context.Context, item execution) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin run claim: %w", err)
	}
	defer tx.Rollback()
	now := s.now().UTC()
	result, err := tx.ExecContext(ctx, "UPDATE runs SET state = ?, version = version + 1, updated_at = ? WHERE run_id = ? AND state = ?",
		kernel.RunRunning, formatTime(now), item.RunID, kernel.RunCreated,
	)
	if err != nil {
		return false, fmt.Errorf("claim run: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read claimed run rows: %w", err)
	}
	if affected != 1 {
		return false, nil
	}
	result, err = tx.ExecContext(ctx, "UPDATE plan_steps SET state = ?, version = version + 1, updated_at = ? WHERE step_id = ? AND state = ?",
		kernel.StepRunning, formatTime(now), item.StepID, kernel.StepApproved,
	)
	if err != nil {
		return false, fmt.Errorf("start plan step: %w", err)
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read started plan step rows: %w", err)
	}
	if affected != 1 {
		return false, nil
	}
	if err := s.audit.RecordTx(ctx, tx, audit.Entry{
		OccurredAt: now, Actor: "core", Action: "run.execute", ResourceType: "run", ResourceID: string(item.RunID),
		Outcome: "started", TraceID: string(item.RunID), Details: map[string]any{"task_id": item.TaskID, "plan_id": item.PlanID, "step_id": item.StepID},
	}); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit run claim: %w", err)
	}
	slog.Info("run execution started", "component", "run", "operation", "execute", "result", "started", "run_id", item.RunID, "task_id", item.TaskID, "plan_id", item.PlanID, "step_id", item.StepID, "trace_id", item.RunID)
	return true, nil
}

func (s *Service) issueGrants(ctx context.Context, plan approval.PlanDocument, step approval.StepScope) (map[string][]string, error) {
	approved, err := loadCurrentPlanApproval(ctx, s.db, plan, s.now())
	if err != nil {
		return nil, err
	}
	issuedAt := s.now().UTC()
	expiresAt := issuedAt.Add(time.Duration(plan.Budget.MaxDurationMillis) * time.Millisecond)
	if minimum := issuedAt.Add(time.Hour); expiresAt.Before(minimum) {
		expiresAt = minimum
	}
	if approved.ExpiresAt != nil {
		expiresAt = approved.ExpiresAt.UTC()
	}
	if !expiresAt.After(issuedAt) {
		return nil, approval.ErrNotApproved
	}
	grants := make(map[string][]string)
	for _, scope := range step.Capabilities {
		grantID, err := deterministicGrantID(approved.ID, step.StepID, scope)
		if err != nil {
			return nil, err
		}
		_, err = s.approvals.IssueGrant(ctx, approval.GrantInput{
			ID: grantID, ApprovalID: approved.ID, Plan: plan, StepID: step.StepID,
			Scope: scope, IssuedAt: issuedAt, ExpiresAt: expiresAt,
		})
		if err != nil && !errors.Is(err, approval.ErrAlreadyExists) {
			return nil, err
		}
		grants[scope.Capability] = append(grants[scope.Capability], string(grantID))
	}
	return grants, nil
}

func (s *Service) complete(ctx context.Context, item execution, result agent.Result) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin run completion: %w", err)
	}
	defer tx.Rollback()
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, "UPDATE runs SET state = ?, version = version + 1, updated_at = ?, finished_at = ?, error = NULL WHERE run_id = ? AND state = ?",
		kernel.RunCompleted, formatTime(now), formatTime(now), item.RunID, kernel.RunRunning,
	); err != nil {
		return fmt.Errorf("complete run: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE plan_steps SET state = ?, version = version + 1, updated_at = ? WHERE step_id = ? AND state = ?",
		kernel.StepCompleted, formatTime(now), item.StepID, kernel.StepRunning,
	); err != nil {
		return fmt.Errorf("complete plan step: %w", err)
	}
	var remaining int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE plan_id = ? AND state <> ?", item.PlanID, kernel.RunCompleted).Scan(&remaining); err != nil {
		return fmt.Errorf("count remaining runs: %w", err)
	}
	if remaining == 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE tasks SET state = ?, version = version + 1, updated_at = ? WHERE task_id = ? AND state IN (?, ?)",
			kernel.TaskCompleted, formatTime(now), item.TaskID, kernel.TaskRunning, kernel.TaskPaused,
		); err != nil {
			return fmt.Errorf("complete task: %w", err)
		}
	}
	if err := s.audit.RecordTx(ctx, tx, audit.Entry{
		OccurredAt: now, Actor: "agent", Action: "run.execute", ResourceType: "run", ResourceID: string(item.RunID),
		Outcome: "succeeded", TraceID: string(item.RunID),
		Details: map[string]any{"step_id": item.StepID, "iterations": result.Iterations, "tool_calls": len(result.ToolCalls)},
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit run completion: %w", err)
	}
	slog.Info("run execution completed", "component", "run", "operation", "execute", "result", "succeeded", "run_id", item.RunID, "task_id", item.TaskID, "plan_id", item.PlanID, "step_id", item.StepID, "trace_id", item.RunID, "iterations", result.Iterations, "tool_calls", len(result.ToolCalls))
	return nil
}

func (s *Service) fail(ctx context.Context, item execution, cause error) error {
	if cause == nil {
		return nil
	}
	failure := strings.TrimSpace(cause.Error())
	if len(failure) > 2048 {
		failure = failure[:2048]
	}
	tx, err := s.db.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("begin run failure: %w", err))
	}
	defer tx.Rollback()
	now := s.now().UTC()
	if _, err := tx.ExecContext(context.WithoutCancel(ctx), "UPDATE runs SET state = ?, version = version + 1, updated_at = ?, finished_at = ?, error = ? WHERE run_id = ? AND state IN (?, ?)",
		kernel.RunFailed, formatTime(now), formatTime(now), failure, item.RunID, kernel.RunCreated, kernel.RunRunning,
	); err != nil {
		return errors.Join(cause, fmt.Errorf("fail run: %w", err))
	}
	if _, err := tx.ExecContext(context.WithoutCancel(ctx), "UPDATE plan_steps SET state = ?, version = version + 1, updated_at = ? WHERE step_id = ? AND state = ?",
		kernel.StepFailed, formatTime(now), item.StepID, kernel.StepRunning,
	); err != nil {
		return errors.Join(cause, fmt.Errorf("fail plan step: %w", err))
	}
	if _, err := tx.ExecContext(context.WithoutCancel(ctx), "UPDATE tasks SET state = ?, version = version + 1, updated_at = ? WHERE task_id = ? AND state = ?",
		kernel.TaskFailed, formatTime(now), item.TaskID, kernel.TaskRunning,
	); err != nil {
		return errors.Join(cause, fmt.Errorf("fail task: %w", err))
	}
	if err := s.audit.RecordTx(context.WithoutCancel(ctx), tx, audit.Entry{
		OccurredAt: now, Actor: "agent", Action: "run.execute", ResourceType: "run", ResourceID: string(item.RunID),
		Outcome: "failed", TraceID: string(item.RunID), Details: map[string]any{"step_id": item.StepID, "error": failure},
	}); err != nil {
		return errors.Join(cause, err)
	}
	if err := tx.Commit(); err != nil {
		return errors.Join(cause, fmt.Errorf("commit run failure: %w", err))
	}
	slog.Error("run execution failed", "component", "run", "operation", "execute", "result", "failed", "run_id", item.RunID, "task_id", item.TaskID, "plan_id", item.PlanID, "step_id", item.StepID, "trace_id", item.RunID, "error", cause)
	return cause
}

func (s *Service) setActive(taskID kernel.TaskID, cancel context.CancelFunc) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	s.activeTask = taskID
	s.activeCancel = cancel
}

func (s *Service) clearActive(taskID kernel.TaskID) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if s.activeTask == taskID {
		s.activeTask = ""
		s.activeCancel = nil
	}
}

func (s *Service) interrupt(taskID kernel.TaskID) {
	s.activeMu.Lock()
	cancel := s.activeCancel
	active := s.activeTask == taskID
	s.activeMu.Unlock()
	if active && cancel != nil {
		cancel()
	}
}

func (s *Service) taskExecutionMode(ctx context.Context, taskID kernel.TaskID) (kernel.ExecutionMode, error) {
	var mode string
	err := s.db.QueryRowContext(ctx, "SELECT execution_mode FROM tasks WHERE task_id = ?", taskID).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: task %s", kernel.ErrNotFound, taskID)
	}
	if err != nil {
		return "", fmt.Errorf("load task execution mode: %w", err)
	}
	return kernel.NormalizeExecutionMode(mode), nil
}

func (s *Service) taskState(ctx context.Context, taskID kernel.TaskID) (kernel.TaskState, error) {
	var state string
	if err := s.db.QueryRowContext(ctx, "SELECT state FROM tasks WHERE task_id = ?", taskID).Scan(&state); err != nil {
		return "", fmt.Errorf("load task state: %w", err)
	}
	return kernel.TaskState(state), nil
}

func classifyTaskActionError(err error) error {
	switch {
	case errors.Is(err, kernel.ErrNotFound):
		return applicationerror.Wrap(applicationerror.CodeNotFound, false, err)
	case errors.Is(err, kernel.ErrVersionConflict), errors.Is(err, kernel.ErrInvalidTransition):
		return applicationerror.Wrap(applicationerror.CodeConflict, false, err)
	case errors.Is(err, kernel.ErrInvalidAggregate):
		return applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, err)
	default:
		return err
	}
}

func validateStoredPlan(request StartRequest, stored corequery.StoredPlanDocument) (approval.PlanDocument, error) {
	var plan approval.PlanDocument
	if err := json.Unmarshal(stored.Document, &plan); err != nil {
		return approval.PlanDocument{}, applicationerror.Wrap(applicationerror.CodePlanDocumentUnavailable, false, fmt.Errorf("decode stored plan: %w", err))
	}
	computed, err := plan.Hash()
	if err != nil {
		return approval.PlanDocument{}, applicationerror.Wrap(applicationerror.CodePlanDocumentUnavailable, false, err)
	}
	if plan.TaskID != request.TaskID || plan.PlanID != request.PlanID || plan.Revision != request.PlanRevision || stored.Revision != request.PlanRevision ||
		computed != request.PlanHash || stored.Hash != request.PlanHash {
		return approval.PlanDocument{}, applicationerror.Wrap(applicationerror.CodePlanChanged, false, approval.ErrPlanChanged)
	}
	return plan, nil
}

func executionPlan(item execution) (approval.PlanDocument, approval.StepScope, error) {
	var plan approval.PlanDocument
	if err := json.Unmarshal(item.PlanDocument, &plan); err != nil {
		return approval.PlanDocument{}, approval.StepScope{}, fmt.Errorf("decode execution plan: %w", err)
	}
	computed, err := plan.Hash()
	if err != nil || computed != item.PlanHash || plan.PlanID != item.PlanID || plan.TaskID != item.TaskID {
		return approval.PlanDocument{}, approval.StepScope{}, approval.ErrPlanChanged
	}
	for _, step := range plan.Steps {
		if step.StepID == item.StepID {
			return plan, step, nil
		}
	}
	return approval.PlanDocument{}, approval.StepScope{}, fmt.Errorf("step %s is absent from canonical plan", item.StepID)
}

func executionMessages(taskObjective string, plan approval.PlanDocument, step approval.StepScope) []providerapi.Message {
	capabilityJSON, _ := json.Marshal(step.Capabilities)
	return []providerapi.Message{
		{Role: providerapi.RoleSystem, Content: "Execute exactly one approved plan step. Use only the advertised tools and approved capability scopes. Do not execute other steps, broaden paths or commands, or claim success without evidence. Return a concise final result for this step."},
		{Role: providerapi.RoleUser, Content: fmt.Sprintf("Task objective: %s\nApproved plan objective: %s\nCurrent step: %s\nExpected side effects: %s\nRollback: %s\nApproved capabilities: %s", strings.TrimSpace(taskObjective), plan.Objective, step.Title, strings.Join(step.ExpectedSideEffects, "; "), step.Rollback, capabilityJSON)},
	}
}

// toolTimeoutMillis returns a timeout that cannot exceed any capability grant
// MaxDurationMillis on the step, so broker Duration checks stay within grants.
func toolTimeoutMillis(step approval.StepScope) int64 {
	timeout := step.TimeoutMillis
	if timeout <= 0 {
		return timeout
	}
	for _, capability := range step.Capabilities {
		if capability.MaxDurationMillis > 0 && capability.MaxDurationMillis < timeout {
			timeout = capability.MaxDurationMillis
		}
	}
	return timeout
}

func allowedTools(step approval.StepScope) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(step.Capabilities))
	for _, capability := range step.Capabilities {
		if _, exists := seen[capability.Capability]; exists {
			continue
		}
		seen[capability.Capability] = struct{}{}
		result = append(result, capability.Capability)
	}
	sort.Strings(result)
	return result
}

type queryRowContext interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadCurrentPlanApproval(ctx context.Context, query queryRowContext, plan approval.PlanDocument, now time.Time) (approval.Approval, error) {
	hash, err := plan.Hash()
	if err != nil {
		return approval.Approval{}, err
	}
	var value approval.Approval
	var decidedAt string
	var expiresAt sql.NullString
	err = query.QueryRowContext(ctx, "SELECT approval_id, decided_by, reason, decided_at, expires_at FROM approvals "+
		"WHERE plan_id = ? AND plan_revision = ? AND scope_hash = ? AND decision = ? "+
		"AND scope_type = ? AND step_id IS NULL AND invalidated_at IS NULL "+
		"AND (expires_at IS NULL OR expires_at > ?) ORDER BY decided_at DESC, approval_id DESC LIMIT 1",
		plan.PlanID, plan.Revision, hash, approval.DecisionApproved, approval.ScopePlan, formatTime(now.UTC()),
	).Scan(&value.ID, &value.DecidedBy, &value.Reason, &decidedAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return approval.Approval{}, ErrNotApproved
	}
	if err != nil {
		return approval.Approval{}, fmt.Errorf("load plan approval: %w", err)
	}
	value.PlanID = plan.PlanID
	value.PlanRevision = plan.Revision
	value.PlanHash = hash
	value.Scope = approval.ScopePlan
	value.Decision = approval.DecisionApproved
	value.DecidedAt, err = time.Parse(time.RFC3339Nano, decidedAt)
	if err != nil {
		return approval.Approval{}, fmt.Errorf("parse approval decision time: %w", err)
	}
	if expiresAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, expiresAt.String)
		if err != nil {
			return approval.Approval{}, fmt.Errorf("parse approval expiration: %w", err)
		}
		parsed = parsed.UTC()
		value.ExpiresAt = &parsed
	}
	return value, nil
}

func requireExecutableStates(ctx context.Context, tx *sql.Tx, plan approval.PlanDocument) error {
	var planState, taskState string
	err := tx.QueryRowContext(ctx, "SELECT p.state, t.state FROM plans p JOIN tasks t ON t.task_id = p.task_id WHERE p.plan_id = ? AND p.task_id = ?",
		plan.PlanID, plan.TaskID,
	).Scan(&planState, &taskState)
	if errors.Is(err, sql.ErrNoRows) {
		return corequery.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load execution state: %w", err)
	}
	validPlan := planState == string(kernel.PlanWaitingApproval) || planState == string(kernel.PlanApproved)
	validTask := taskState == string(kernel.TaskWaitingApproval) || taskState == string(kernel.TaskApproved) || taskState == string(kernel.TaskRunning)
	if !validPlan || !validTask {
		return fmt.Errorf("%w: plan=%s task=%s", ErrInvalidState, planState, taskState)
	}
	return nil
}

func listRunIDs(ctx context.Context, tx *sql.Tx, planID kernel.PlanID) ([]kernel.RunID, error) {
	rows, err := tx.QueryContext(ctx, "SELECT r.run_id FROM runs r LEFT JOIN plan_steps s ON s.step_id = r.step_id WHERE r.plan_id = ? ORDER BY s.position, r.run_id",
		planID,
	)
	if err != nil {
		return nil, fmt.Errorf("list plan runs: %w", err)
	}
	defer rows.Close()
	result := make([]kernel.RunID, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, kernel.RunID(id))
	}
	return result, rows.Err()
}

func deterministicRunID(planID kernel.PlanID, stepID kernel.StepID) kernel.RunID {
	digest := sha256.Sum256([]byte("run\x00" + string(planID) + "\x00" + string(stepID)))
	return kernel.RunID("run-" + hex.EncodeToString(digest[:16]))
}

func deterministicGrantID(approvalID approval.ApprovalID, stepID kernel.StepID, scope approval.CapabilityScope) (approval.GrantID, error) {
	encoded, err := json.Marshal(scope)
	if err != nil {
		return "", fmt.Errorf("encode grant scope: %w", err)
	}
	digest := sha256.Sum256(append([]byte(string(approvalID)+"\x00"+string(stepID)+"\x00"), encoded...))
	return approval.GrantID("grant-" + hex.EncodeToString(digest[:16])), nil
}

func classifyStartError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, corequery.ErrNotFound), errors.Is(err, kernel.ErrNotFound):
		return applicationerror.Wrap(applicationerror.CodeNotFound, false, err)
	case errors.Is(err, ErrInvalidRequest):
		return applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, err)
	case errors.Is(err, approval.ErrPlanChanged):
		return applicationerror.Wrap(applicationerror.CodePlanChanged, false, err)
	case errors.Is(err, ErrNotApproved), errors.Is(err, ErrInvalidState):
		return applicationerror.Wrap(applicationerror.CodeConflict, false, err)
	default:
		return err
	}
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

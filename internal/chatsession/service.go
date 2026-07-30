// Package chatsession runs multi-turn Session chat without the Planner.
// Agent mode: user message → workspace-preauthorized Run → Agent loop.
// Plan mode remains on the planning / approval path outside this package.
package chatsession

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"autozeagent.local/autozeagent/internal/agent"
	"autozeagent.local/autozeagent/internal/applicationerror"
	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/audit"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/policy"
	"autozeagent.local/autozeagent/pkg/providerapi"
)

const (
	// chatStepIDPrefix prefixes per-task step IDs. plan_steps.step_id is a global PK,
	// so the literal "chat-step" cannot be reused across tasks.
	chatStepIDPrefix = "chat-step-"
	chatSystemPrompt = "You are AutoZeAgent, a local coding assistant in a multi-turn chat session. " +
		"Reply helpfully in the user's language. Use tools only when needed to answer or complete the request. " +
		"Prefer absolute paths under the workspace; relative paths are resolved against the workspace root. " +
		"Do not invent plan steps or claim tool success without evidence."
	defaultMaxTokens     int64  = 128_000
	defaultMaxDurationMS int64  = 30 * 60 * 1000
	defaultToolTimeoutMS int64  = 60_000
	defaultMaxCalls      uint64 = 10_000
)

// chatStepID returns a globally unique plan_steps.step_id for one chat task turn.
func chatStepID(taskID kernel.TaskID) kernel.StepID {
	return kernel.StepID(chatStepIDPrefix + deterministicID("chat-step", string(taskID)))
}

func planChatStepID(plan approval.PlanDocument) kernel.StepID {
	if len(plan.Steps) > 0 && strings.TrimSpace(string(plan.Steps[0].StepID)) != "" {
		return plan.Steps[0].StepID
	}
	return chatStepID(plan.TaskID)
}

var (
	ErrInvalidRequest = errors.New("invalid chat request")
	ErrUnavailable    = errors.New("chat session unavailable")
)

type AgentRunner interface {
	Run(context.Context, agent.RunRequest) (agent.Result, error)
}

type TranscriptLoader interface {
	SessionTranscript(context.Context, kernel.SessionID, corequery.TranscriptOptions) ([]corequery.TranscriptMessage, error)
}

type Config struct {
	DB         *sql.DB
	Repository *kernel.Repository
	Approvals  *approval.Repository
	Agent      AgentRunner
	Transcript TranscriptLoader
	// WorkspaceRoots are absolute roots for default read tools (fs_read/list/stat).
	WorkspaceRoots []string
	// AllowWrite enables fs_write/fs_patch/fs_mkdir in the workspace grant.
	AllowWrite bool
	Now        func() time.Time
	OnError    func(error)
}

type Service struct {
	db         *sql.DB
	repository *kernel.Repository
	approvals  *approval.Repository
	agent      AgentRunner
	transcript TranscriptLoader
	roots      []string
	allowWrite bool
	audit      *audit.Store
	now        func() time.Time
	onError    func(error)

	activeMu     sync.Mutex
	activeTask   kernel.TaskID
	activeCancel context.CancelFunc
}

type StartRequest struct {
	Task    kernel.Task
	Actor   string
	TraceID string
	// UserText overrides task.Objective when non-empty.
	UserText string
}

type StartResult struct {
	Task   kernel.Task   `json:"task"`
	RunID  kernel.RunID  `json:"run_id"`
	PlanID kernel.PlanID `json:"plan_id"`
}

func New(config Config) (*Service, error) {
	if config.DB == nil || config.Repository == nil || config.Approvals == nil || config.Agent == nil || config.Transcript == nil {
		return nil, errors.New("chat session requires db, repository, approvals, agent, and transcript loader")
	}
	roots := make([]string, 0, len(config.WorkspaceRoots))
	for _, root := range config.WorkspaceRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("chat workspace root: %w", err)
		}
		roots = append(roots, abs)
	}
	if len(roots) == 0 {
		return nil, errors.New("chat session requires at least one workspace root")
	}
	auditStore, err := audit.NewStore(config.DB)
	if err != nil {
		return nil, err
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.OnError == nil {
		config.OnError = func(error) {}
	}
	return &Service{
		db: config.DB, repository: config.Repository, approvals: config.Approvals,
		agent: config.Agent, transcript: config.Transcript, roots: roots,
		allowWrite: config.AllowWrite, audit: auditStore, now: config.Now, onError: config.OnError,
	}, nil
}

// StartChat prepares a workspace-scoped plan+grants for the task, then runs one
// chat agent turn asynchronously. The task must be execution_mode=agent and
// state=created (fresh submission).
func (s *Service) StartChat(ctx context.Context, request StartRequest) (StartResult, error) {
	if ctx == nil {
		return StartResult{}, errors.New("chat context is required")
	}
	if s == nil || s.agent == nil {
		return StartResult{}, applicationerror.Wrap(applicationerror.CodeUnavailable, false, ErrUnavailable)
	}
	task := request.Task
	if task.ID == "" || task.SessionID == "" {
		return StartResult{}, applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, fmt.Errorf("%w: task and session are required", ErrInvalidRequest))
	}
	mode := kernel.NormalizeExecutionMode(string(task.ExecutionMode))
	if mode != kernel.ExecutionModeAgent {
		return StartResult{}, applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, fmt.Errorf("%w: chat requires agent execution_mode", ErrInvalidRequest))
	}
	if task.State != kernel.TaskCreated {
		// Idempotent: already started chat for this task.
		if runID, ok, err := s.existingChatRun(ctx, task.ID); err != nil {
			return StartResult{}, err
		} else if ok {
			return StartResult{Task: task, RunID: runID, PlanID: chatPlanID(task.ID)}, nil
		}
		return StartResult{}, applicationerror.Wrap(applicationerror.CodeConflict, false, fmt.Errorf("%w: task %s is %s", ErrInvalidRequest, task.ID, task.State))
	}

	userText := strings.TrimSpace(request.UserText)
	if userText == "" {
		userText = strings.TrimSpace(task.Objective)
	}
	if userText == "" {
		userText = strings.TrimSpace(task.Title)
	}
	if userText == "" {
		return StartResult{}, applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, fmt.Errorf("%w: user message is required", ErrInvalidRequest))
	}
	actor := strings.TrimSpace(request.Actor)
	if actor == "" {
		actor = "local-user"
	}

	plan, planHash, task, err := s.ensureChatWorkspaceAuth(ctx, task)
	if err != nil {
		return StartResult{}, err
	}

	runID, err := s.createChatRun(ctx, task, plan, planHash, actor, request.TraceID)
	if err != nil {
		return StartResult{}, err
	}
	task, err = s.repository.GetTask(ctx, task.ID)
	if err != nil {
		return StartResult{}, classify(err)
	}

	history, err := s.loadHistory(ctx, task.SessionID, task.ID, userText)
	if err != nil {
		return StartResult{}, err
	}
	grantIDs, err := s.issueChatGrants(ctx, plan)
	if err != nil {
		return StartResult{}, err
	}

	// Capture for async execution.
	execTask := task
	execPlan := plan
	execHash := planHash
	execRun := runID
	execHistory := history
	execGrants := grantIDs
	execUser := userText
	go s.executeChat(execTask, execPlan, execHash, execRun, execHistory, execGrants, execUser)

	return StartResult{Task: task, RunID: runID, PlanID: plan.PlanID}, nil
}

// ensureChatWorkspaceAuth creates (or reuses) an already-approved synthetic plan
// and system approval. Task path is created → running only — never planning.
func (s *Service) ensureChatWorkspaceAuth(ctx context.Context, task kernel.Task) (approval.PlanDocument, string, kernel.Task, error) {
	planID := chatPlanID(task.ID)
	var storedHash, state, document string
	err := s.db.QueryRowContext(ctx, `
		SELECT scope_hash, state, document FROM plans WHERE plan_id = ? AND task_id = ?`,
		planID, task.ID,
	).Scan(&storedHash, &state, &document)
	if err == nil {
		if state != string(kernel.PlanApproved) {
			return approval.PlanDocument{}, "", task, applicationerror.Wrap(applicationerror.CodeConflict, false,
				fmt.Errorf("%w: chat plan %s is %s, want approved", ErrInvalidRequest, planID, state))
		}
		var plan approval.PlanDocument
		if err := json.Unmarshal([]byte(document), &plan); err != nil {
			return approval.PlanDocument{}, "", task, fmt.Errorf("decode chat plan: %w", err)
		}
		if err := s.recordSystemApproval(ctx, plan); err != nil {
			return approval.PlanDocument{}, "", task, err
		}
		task, err = s.repository.GetTask(ctx, task.ID)
		if err != nil {
			return approval.PlanDocument{}, "", task, classify(err)
		}
		return plan, storedHash, task, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return approval.PlanDocument{}, "", task, fmt.Errorf("lookup chat plan: %w", err)
	}

	plan := s.buildWorkspacePlan(planID, task.ID)
	documentBytes, err := plan.CanonicalJSON()
	if err != nil {
		return approval.PlanDocument{}, "", task, err
	}
	hash, err := plan.Hash()
	if err != nil {
		return approval.PlanDocument{}, "", task, err
	}
	drafts := []kernel.PlanStepDraft{{
		ID: plan.Steps[0].StepID, Position: 0, Title: plan.Steps[0].Title, EffectLevel: string(plan.Steps[0].Risk),
	}}
	_, task, err = s.repository.CreateApprovedWorkspacePlan(
		ctx, plan.PlanID, task.ID, task.Version, plan.Revision, hash, documentBytes, drafts,
		"session chat workspace auth", s.now(),
	)
	if err != nil {
		return approval.PlanDocument{}, "", task, classify(err)
	}
	if err := s.recordSystemApproval(ctx, plan); err != nil {
		return approval.PlanDocument{}, "", task, err
	}
	return plan, hash, task, nil
}

func (s *Service) recordSystemApproval(ctx context.Context, plan approval.PlanDocument) error {
	approvalID := approval.ApprovalID(deterministicID("chat-approval", string(plan.PlanID)))
	expires := s.now().UTC().Add(24 * time.Hour)
	_, err := s.approvals.RecordSystemApproval(ctx, approval.DecisionInput{
		ID: approvalID, Plan: plan, Scope: approval.ScopePlan,
		Decision: approval.DecisionApproved, DecidedBy: "session-workspace-auth",
		Reason: "session chat workspace preauthorization", DecidedAt: s.now(), ExpiresAt: &expires,
	})
	if err != nil && !errors.Is(err, approval.ErrAlreadyExists) {
		return classify(err)
	}
	return nil
}

func (s *Service) createChatRun(ctx context.Context, task kernel.Task, plan approval.PlanDocument, planHash, actor, traceID string) (kernel.RunID, error) {
	runID := chatRunID(task.ID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin chat run: %w", err)
	}
	defer tx.Rollback()

	var existing string
	err = tx.QueryRowContext(ctx, "SELECT run_id FROM runs WHERE run_id = ?", runID).Scan(&existing)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return runID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("lookup chat run: %w", err)
	}

	now := s.now().UTC()
	// Workspace auth already set task to running; only accept running (or created for races).
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks SET state = ?, version = version + 1, updated_at = ?
		WHERE task_id = ? AND state IN (?, ?)`,
		kernel.TaskRunning, formatTime(now), task.ID,
		kernel.TaskRunning, kernel.TaskCreated,
	)
	if err != nil {
		return "", fmt.Errorf("start chat task: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		var state string
		_ = tx.QueryRowContext(ctx, "SELECT state FROM tasks WHERE task_id = ?", task.ID).Scan(&state)
		if state != string(kernel.TaskRunning) && state != string(kernel.TaskCompleted) && state != string(kernel.TaskFailed) {
			return "", applicationerror.Wrap(applicationerror.CodeConflict, false, fmt.Errorf("%w: cannot start chat task in state %s", ErrInvalidRequest, state))
		}
	}
	_, _ = tx.ExecContext(ctx, `
		UPDATE plan_steps SET state = ?, version = version + 1, updated_at = ?
		WHERE plan_id = ? AND state IN (?, ?)`,
		kernel.StepApproved, formatTime(now), plan.PlanID, kernel.StepPending, kernel.StepApproved,
	)
	stepID := planChatStepID(plan)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO runs (run_id, task_id, plan_id, state, started_at, finished_at, error, version, updated_at, step_id)
		VALUES (?, ?, ?, ?, ?, NULL, NULL, 1, ?, ?)`,
		runID, task.ID, plan.PlanID, kernel.RunCreated, formatTime(now), formatTime(now), stepID,
	)
	if err != nil {
		return "", fmt.Errorf("insert chat run: %w", err)
	}
	if err := s.audit.RecordTx(ctx, tx, audit.Entry{
		OccurredAt: now, Actor: actor, Action: "chat.start", ResourceType: "run", ResourceID: string(runID),
		Outcome: "accepted", TraceID: traceID,
		Details: map[string]any{"task_id": task.ID, "session_id": task.SessionID, "plan_id": plan.PlanID, "plan_hash": planHash},
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit chat run: %w", err)
	}
	return runID, nil
}

func (s *Service) issueChatGrants(ctx context.Context, plan approval.PlanDocument) (map[string][]string, error) {
	step := plan.Steps[0]
	hash, err := plan.Hash()
	if err != nil {
		return nil, err
	}
	var approvalID string
	err = s.db.QueryRowContext(ctx, `
		SELECT approval_id FROM approvals
		WHERE plan_id = ? AND plan_revision = ? AND scope_hash = ? AND decision = ?
		AND scope_type = ? AND step_id IS NULL AND invalidated_at IS NULL
		ORDER BY decided_at DESC, approval_id DESC LIMIT 1`,
		plan.PlanID, plan.Revision, hash, approval.DecisionApproved, approval.ScopePlan,
	).Scan(&approvalID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, approval.ErrNotApproved
	}
	if err != nil {
		return nil, fmt.Errorf("load chat approval: %w", err)
	}
	issuedAt := s.now().UTC()
	expiresAt := issuedAt.Add(time.Duration(plan.Budget.MaxDurationMillis) * time.Millisecond)
	if min := issuedAt.Add(time.Hour); expiresAt.Before(min) {
		expiresAt = min
	}
	grants := make(map[string][]string)
	for _, scope := range step.Capabilities {
		grantID := approval.GrantID(deterministicID("chat-grant", approvalID, string(step.StepID), scope.Capability))
		_, err = s.approvals.IssueGrant(ctx, approval.GrantInput{
			ID: grantID, ApprovalID: approval.ApprovalID(approvalID), Plan: plan, StepID: step.StepID,
			Scope: scope, IssuedAt: issuedAt, ExpiresAt: expiresAt,
		})
		if err != nil && !errors.Is(err, approval.ErrAlreadyExists) {
			return nil, err
		}
		grants[scope.Capability] = append(grants[scope.Capability], string(grantID))
	}
	return grants, nil
}

func (s *Service) loadHistory(ctx context.Context, sessionID kernel.SessionID, currentTask kernel.TaskID, currentUser string) ([]providerapi.Message, error) {
	msgs, err := s.transcript.SessionTranscript(ctx, sessionID, corequery.TranscriptOptions{
		Page: corequery.Page{Limit: 200},
	})
	if err != nil {
		return nil, err
	}
	out := make([]providerapi.Message, 0, len(msgs))
	for _, msg := range msgs {
		// Skip the current turn's synthetic user bubble; it is in Messages.
		if msg.TaskID == currentTask && msg.Role == "user" && strings.HasPrefix(msg.ID, "task-user:") {
			continue
		}
		if msg.TaskID == currentTask && strings.TrimSpace(msg.Content) == strings.TrimSpace(currentUser) && msg.Role == "user" {
			continue
		}
		switch strings.ToLower(msg.Role) {
		case "user":
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			out = append(out, providerapi.Message{Role: providerapi.RoleUser, Content: msg.Content})
		case "assistant":
			m := providerapi.Message{Role: providerapi.RoleAssistant, Content: msg.Content, Thinking: msg.Thinking}
			for _, tc := range msg.ToolCalls {
				m.ToolCalls = append(m.ToolCalls, providerapi.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments})
			}
			out = append(out, m)
		case "tool":
			out = append(out, providerapi.Message{
				Role: providerapi.RoleTool, Content: msg.Content, ToolCallID: msg.ToolCallID,
			})
		}
	}
	return out, nil
}

func (s *Service) executeChat(
	task kernel.Task,
	plan approval.PlanDocument,
	planHash string,
	runID kernel.RunID,
	history []providerapi.Message,
	grantIDs map[string][]string,
	userText string,
) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(plan.Budget.MaxDurationMillis)*time.Millisecond)
	defer cancel()
	s.setActive(task.ID, cancel)
	defer s.clearActive(task.ID)

	stepID := planChatStepID(plan)
	// Mark run running.
	now := s.now().UTC()
	_, _ = s.db.ExecContext(ctx, `
		UPDATE runs SET state = ?, version = version + 1, updated_at = ? WHERE run_id = ? AND state = ?`,
		kernel.RunRunning, formatTime(now), runID, kernel.RunCreated,
	)
	_, _ = s.db.ExecContext(ctx, `
		UPDATE plan_steps SET state = ?, version = version + 1, updated_at = ? WHERE step_id = ? AND plan_id = ?`,
		kernel.StepRunning, formatTime(now), stepID, plan.PlanID,
	)

	allowed := make([]string, 0, len(plan.Steps[0].Capabilities))
	for _, cap := range plan.Steps[0].Capabilities {
		allowed = append(allowed, cap.Capability)
	}
	timeoutMS := plan.Steps[0].TimeoutMillis
	for _, cap := range plan.Steps[0].Capabilities {
		if cap.MaxDurationMillis > 0 && cap.MaxDurationMillis < timeoutMS {
			timeoutMS = cap.MaxDurationMillis
		}
	}

	messages := []providerapi.Message{
		{Role: providerapi.RoleSystem, Content: chatSystemPrompt},
		{Role: providerapi.RoleUser, Content: userText},
	}
	result, err := s.agent.Run(ctx, agent.RunRequest{
		RunID: string(runID), TaskID: string(task.ID), SessionID: string(task.SessionID),
		PlanID: string(plan.PlanID), PlanHash: planHash, StepID: string(stepID),
		Actor: "agent", TraceID: string(runID),
		Messages: messages, History: history,
		AllowedTools: allowed, CapabilityGrantIDs: grantIDs,
		MaxOutputTokens: plan.Budget.MaxTokens, MaxTotalTokens: plan.Budget.MaxTokens,
		MaxCostMicros: plan.Budget.MaxCostMicros, ToolTimeoutMillis: timeoutMS,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Info("chat run interrupted", "component", "chatsession", "operation", "execute", "result", "canceled",
				"run_id", runID, "task_id", task.ID)
			return
		}
		s.failChat(context.WithoutCancel(ctx), task.ID, runID, stepID, err)
		s.onError(err)
		return
	}
	if err := s.completeChat(context.WithoutCancel(ctx), task.ID, runID, stepID, result); err != nil {
		s.onError(err)
	}
}

func (s *Service) completeChat(ctx context.Context, taskID kernel.TaskID, runID kernel.RunID, stepID kernel.StepID, result agent.Result) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE runs SET state = ?, version = version + 1, updated_at = ?, finished_at = ?, error = NULL
		WHERE run_id = ? AND state IN (?, ?)`,
		kernel.RunCompleted, formatTime(now), formatTime(now), runID, kernel.RunRunning, kernel.RunCreated,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE plan_steps SET state = ?, version = version + 1, updated_at = ?
		WHERE step_id = ?`, kernel.StepCompleted, formatTime(now), stepID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET state = ?, version = version + 1, updated_at = ?
		WHERE task_id = ? AND state IN (?, ?)`,
		kernel.TaskCompleted, formatTime(now), taskID, kernel.TaskRunning, kernel.TaskPaused,
	); err != nil {
		return err
	}
	if err := s.audit.RecordTx(ctx, tx, audit.Entry{
		OccurredAt: now, Actor: "agent", Action: "chat.execute", ResourceType: "run", ResourceID: string(runID),
		Outcome: "succeeded", TraceID: string(runID),
		Details: map[string]any{"task_id": taskID, "iterations": result.Iterations, "tool_calls": len(result.ToolCalls)},
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) failChat(ctx context.Context, taskID kernel.TaskID, runID kernel.RunID, stepID kernel.StepID, cause error) {
	failure := strings.TrimSpace(cause.Error())
	if len(failure) > 2048 {
		failure = failure[:2048]
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	now := s.now().UTC()
	_, _ = tx.ExecContext(ctx, `
		UPDATE runs SET state = ?, version = version + 1, updated_at = ?, finished_at = ?, error = ?
		WHERE run_id = ?`, kernel.RunFailed, formatTime(now), formatTime(now), failure, runID)
	_, _ = tx.ExecContext(ctx, `
		UPDATE plan_steps SET state = ?, version = version + 1, updated_at = ? WHERE step_id = ?`,
		kernel.StepFailed, formatTime(now), stepID)
	_, _ = tx.ExecContext(ctx, `
		UPDATE tasks SET state = ?, version = version + 1, updated_at = ?
		WHERE task_id = ? AND state IN (?, ?)`,
		kernel.TaskFailed, formatTime(now), taskID, kernel.TaskRunning, kernel.TaskPaused)
	_ = s.audit.RecordTx(ctx, tx, audit.Entry{
		OccurredAt: now, Actor: "agent", Action: "chat.execute", ResourceType: "run", ResourceID: string(runID),
		Outcome: "failed", TraceID: string(runID), Details: map[string]any{"task_id": taskID, "error": failure},
	})
	_ = tx.Commit()
	slog.Error("chat run failed", "component", "chatsession", "operation", "execute", "result", "failed",
		"run_id", runID, "task_id", taskID, "error", cause)
}

func (s *Service) buildWorkspacePlan(planID kernel.PlanID, taskID kernel.TaskID) approval.PlanDocument {
	caps := []approval.CapabilityScope{
		{Capability: "fs_read", Paths: append([]string(nil), s.roots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
		{Capability: "fs_list", Paths: append([]string(nil), s.roots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
		{Capability: "fs_stat", Paths: append([]string(nil), s.roots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
	}
	risk := policy.RiskR0
	effects := []string{"read workspace files when needed"}
	if s.allowWrite {
		caps = append(caps,
			approval.CapabilityScope{Capability: "fs_write", Paths: append([]string(nil), s.roots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
			approval.CapabilityScope{Capability: "fs_patch", Paths: append([]string(nil), s.roots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
			approval.CapabilityScope{Capability: "fs_mkdir", Paths: append([]string(nil), s.roots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
		)
		risk = policy.RiskR1
		effects = append(effects, "write workspace files when needed")
	}
	return approval.PlanDocument{
		PlanID: planID, TaskID: taskID, Revision: 1,
		Objective: "session chat workspace tools",
		Budget: approval.PlanBudget{
			MaxTokens: defaultMaxTokens, MaxCostMicros: 0, MaxDurationMillis: defaultMaxDurationMS,
		},
		Steps: []approval.StepScope{{
			StepID: chatStepID(taskID), Position: 0, Title: "session chat",
			Risk: risk, ExpectedSideEffects: effects,
			Rollback: "none", TimeoutMillis: defaultToolTimeoutMS, Capabilities: caps,
		}},
	}
}

func (s *Service) existingChatRun(ctx context.Context, taskID kernel.TaskID) (kernel.RunID, bool, error) {
	runID := chatRunID(taskID)
	var id string
	err := s.db.QueryRowContext(ctx, "SELECT run_id FROM runs WHERE run_id = ?", runID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return kernel.RunID(id), true, nil
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

func chatPlanID(taskID kernel.TaskID) kernel.PlanID {
	return kernel.PlanID(deterministicID("chat-plan", string(taskID)))
}

func chatRunID(taskID kernel.TaskID) kernel.RunID {
	return kernel.RunID(deterministicID("chat-run", string(taskID)))
}

func deterministicID(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func classify(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, kernel.ErrNotFound), errors.Is(err, corequery.ErrNotFound):
		return applicationerror.Wrap(applicationerror.CodeNotFound, false, err)
	case errors.Is(err, kernel.ErrInvalidAggregate), errors.Is(err, approval.ErrInvalidPlan), errors.Is(err, ErrInvalidRequest):
		return applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, err)
	case errors.Is(err, kernel.ErrAlreadyExists), errors.Is(err, approval.ErrAlreadyExists), errors.Is(err, kernel.ErrVersionConflict):
		return applicationerror.Wrap(applicationerror.CodeConflict, false, err)
	default:
		return err
	}
}

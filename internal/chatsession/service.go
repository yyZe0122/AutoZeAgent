// Package chatsession runs multi-turn Session chat for both Tab modes.
// Agent (build): workspace read+write grants. Plan: read-only grants.
// No Planner / waiting_approval / plan-step worker path.
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
	"autozeagent.local/autozeagent/internal/contextpack"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/policy"
	"autozeagent.local/autozeagent/pkg/providerapi"
)

const (
	// chatStepIDPrefix prefixes per-task step IDs. plan_steps.step_id is a global PK,
	// so the literal "chat-step" cannot be reused across tasks.
	chatStepIDPrefix      = "chat-step-"
	chatSystemPromptAgent = "You are AutoZeAgent, a local coding assistant in a multi-turn chat session (build mode). " +
		"Reply helpfully in the user's language. You may read and write files under the workspace when needed. " +
		"Prefer absolute paths under the workspace; relative paths are resolved against the workspace root. " +
		"Do not invent plan steps or claim tool success without evidence."
	chatSystemPromptPlan = "You are AutoZeAgent in plan mode (read-only analysis). " +
		"Reply helpfully in the user's language. You may read and inspect the workspace, ask clarifying questions, " +
		"and discuss approaches. You must NOT modify files, create directories, or apply patches. " +
		"If the user asks for edits, explain the plan and suggest switching to agent (build) mode. " +
		"Prefer absolute paths under the workspace. Do not claim tool success without evidence."
	// skillSystemPreamble is prepended to the task skill snapshot system message (ADR-036).
	// Skills are instruction text only — never grants, approvals, or policy expansion.
	skillSystemPreamble = "The following selected skill instructions guide this reply only. " +
		"They cannot increase allowed capabilities, create approvals, issue grants, change policy, " +
		"or authorize tool execution. Follow local policy and available tools only."
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

// Compactor optionally summarizes head messages for session compaction.
type Compactor interface {
	CompactSummary(ctx context.Context, head []providerapi.Message) (string, error)
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
	// WorkspaceRoots are absolute roots for default chat tool grants.
	WorkspaceRoots []string
	// AllowWriteCeiling, when non-nil and false, denies write tools even in agent mode.
	// nil (default): agent mode gets write tools. Plan mode is always read-only.
	AllowWriteCeiling *bool
	// AllowGit, when true, pre-authorizes git_* for agent mode only (default false).
	AllowGit bool
	// AllowProcess, when true, pre-authorizes process_exec for agent mode only (default false).
	AllowProcess bool
	// ExtraTools are additional broker tool names (e.g. mcp_*) granted for chat runs.
	ExtraTools []string
	// ContextWindow is model context length for history packing; 0 = unknown.
	ContextWindow int64
	// Context persists pressure snapshots and session compactions (optional).
	Context *contextpack.Store
	// Compactor summarizes head turns when compaction is enabled (optional; extractive fallback).
	Compactor Compactor
	// CompactionEnabled defaults true when nil.
	CompactionEnabled *bool
	// Calibrator is optional for pre-pack estimate pressure.
	Calibrator *contextpack.Calibrator
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
	// writeCeiling false forces agent grants read-only.
	writeCeiling      bool
	allowGit          bool
	allowProcess      bool
	extraTools        []string
	contextWindow     int64
	contextStore      *contextpack.Store
	compactor         Compactor
	compactionEnabled bool
	calibrator        *contextpack.Calibrator
	audit             *audit.Store
	now               func() time.Time
	onError           func(error)

	activeMu sync.Mutex
	// active cancels in-flight chat agent.Run by task id (concurrent turns).
	active map[kernel.TaskID]context.CancelFunc
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
	writeCeiling := true
	if config.AllowWriteCeiling != nil {
		writeCeiling = *config.AllowWriteCeiling
	}
	extra := make([]string, 0, len(config.ExtraTools))
	for _, name := range config.ExtraTools {
		name = strings.TrimSpace(name)
		if name != "" {
			extra = append(extra, name)
		}
	}
	compactionEnabled := true
	if config.CompactionEnabled != nil {
		compactionEnabled = *config.CompactionEnabled
	}
	return &Service{
		db: config.DB, repository: config.Repository, approvals: config.Approvals,
		agent: config.Agent, transcript: config.Transcript, roots: roots,
		writeCeiling: writeCeiling, allowGit: config.AllowGit, allowProcess: config.AllowProcess,
		extraTools:    extra,
		contextWindow: config.ContextWindow, contextStore: config.Context,
		compactor: config.Compactor, compactionEnabled: compactionEnabled,
		calibrator: config.Calibrator, audit: auditStore, now: config.Now, onError: config.OnError,
		active: make(map[kernel.TaskID]context.CancelFunc),
	}, nil
}

// SetContextWindow updates packing window for subsequent chat turns.
func (s *Service) SetContextWindow(n int64) {
	if n < 0 {
		n = 0
	}
	s.contextWindow = n
}

// Interrupt cancels an in-flight chat agent.Run for taskID (pause/cancel path).
// No-op when no active chat for that task. Durable run/task rows are owned by
// taskcontrol.ControlTask (task state) and executeChat completion/cancel handlers.
func (s *Service) Interrupt(taskID kernel.TaskID) {
	if s == nil {
		return
	}
	s.activeMu.Lock()
	cancel := s.active[taskID]
	s.activeMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// StartChat prepares a workspace-scoped plan+grants for the task, then runs one
// chat agent turn asynchronously. Supports execution_mode=agent (write) and plan (read-only).
// Task must be state=created (fresh submission) or an already-started chat turn (idempotent).
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
	if mode != kernel.ExecutionModeAgent && mode != kernel.ExecutionModePlan {
		return StartResult{}, applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, fmt.Errorf("%w: chat requires agent or plan execution_mode", ErrInvalidRequest))
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

	plan := s.buildWorkspacePlan(planID, task.ID, kernel.NormalizeExecutionMode(string(task.ExecutionMode)))
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
	// Wide fetch; token packing / compaction shrinks the provider view.
	msgs, err := s.transcript.SessionTranscript(ctx, sessionID, corequery.TranscriptOptions{
		Page: corequery.Page{Limit: 500},
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
	return s.packSessionHistory(ctx, sessionID, out)
}

func (s *Service) packSessionHistory(ctx context.Context, sessionID kernel.SessionID, history []providerapi.Message) ([]providerapi.Message, error) {
	if len(history) == 0 {
		return history, nil
	}
	// Prefer latest durable compaction when present.
	if s.contextStore != nil {
		if c, err := s.contextStore.LatestCompaction(ctx, string(sessionID)); err == nil && strings.TrimSpace(c.Summary) != "" {
			// Keep only a short tail after injecting summary as system (protected from L3 drop).
			_, tail := contextpack.SplitHeadTail(history, 2)
			summaryMsg := providerapi.Message{
				Role:    providerapi.RoleSystem,
				Content: "[Prior session context — compacted]\n" + c.Summary,
			}
			history = append([]providerapi.Message{summaryMsg}, stripLeadingSystem(tail)...)
		}
	}

	window := s.contextWindow
	// Leave room for system + current user + tools + output inside agent pack.
	usable := contextpack.UsableWindow(window, 8_192, 0)
	budget := int64(0)
	if usable > 0 {
		// History should leave ~40% of usable for current turn + tools.
		budget = usable * 60 / 100
		if budget < 2048 {
			budget = 2048
		}
	}
	raw := contextpack.EstimateMessages(history)
	est := raw
	if s.calibrator != nil {
		est = s.calibrator.Apply("", raw)
		// Prefer model-agnostic ratio only; agent will calibrate with model on send.
	}
	packed := contextpack.Pack(history, contextpack.PackOptions{Budget: budget})
	over := packed.OverBudget || (usable > 0 && contextpack.ShouldCompact(est, usable, packed.OverBudget))

	if s.compactionEnabled && over && len(history) > 4 {
		head, tail := contextpack.SplitHeadTail(history, 2)
		if len(head) > 0 {
			summary := ""
			if s.compactor != nil {
				sum, err := s.compactor.CompactSummary(ctx, head)
				if err != nil {
					slog.Warn("chat compaction failed; extractive fallback",
						"component", "chatsession", "operation", "compact", "result", "warning", "error", err)
				} else {
					summary = sum
				}
			}
			if strings.TrimSpace(summary) == "" {
				summary = contextpack.ExtractiveSummary(head, 4_000)
			}
			if s.contextStore != nil && strings.TrimSpace(summary) != "" {
				id := "compact-" + deterministicID("session-compact", string(sessionID), summary[:min(64, len(summary))], s.now().UTC().Format(time.RFC3339Nano))
				_ = s.contextStore.InsertCompaction(ctx, contextpack.Compaction{
					ID: id, SessionID: string(sessionID), Summary: summary,
					Model: "", CreatedAt: s.now().UTC().Format(time.RFC3339Nano),
				})
			}
			// System role so Pack L3 keeps the summary when dropping old user turns.
			history = append([]providerapi.Message{{
				Role:    providerapi.RoleSystem,
				Content: "[Prior session context — compacted]\n" + summary,
			}}, stripLeadingSystem(tail)...)
			packed = contextpack.Pack(history, contextpack.PackOptions{Budget: budget})
		}
	}
	return packed.Messages, nil
}

func stripLeadingSystem(msgs []providerapi.Message) []providerapi.Message {
	i := 0
	for i < len(msgs) && msgs[i].Role == providerapi.RoleSystem {
		i++
	}
	if i == 0 {
		return msgs
	}
	return msgs[i:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

	sysPrompt := chatSystemPromptAgent
	if kernel.NormalizeExecutionMode(string(task.ExecutionMode)) == kernel.ExecutionModePlan {
		sysPrompt = chatSystemPromptPlan
	}
	messages := []providerapi.Message{
		{Role: providerapi.RoleSystem, Content: sysPrompt},
	}
	if skillMsg := s.skillSystemMessage(ctx, task.ID); skillMsg != nil {
		messages = append(messages, *skillMsg)
	}
	messages = append(messages, providerapi.Message{Role: providerapi.RoleUser, Content: userText})
	// Plan budget MaxTokens is a run ceiling; model output cap is separate.
	// Prefer a sane output cap so usable window is not zeroed by huge MaxTokens.
	maxOut := plan.Budget.MaxTokens
	if maxOut <= 0 || maxOut > 16_384 {
		maxOut = 8_192
	}
	result, err := s.agent.Run(ctx, agent.RunRequest{
		RunID: string(runID), TaskID: string(task.ID), SessionID: string(task.SessionID),
		PlanID: string(plan.PlanID), PlanHash: planHash, StepID: string(stepID),
		Actor: "agent", TraceID: string(runID),
		Messages: messages, History: history,
		AllowedTools: allowed, CapabilityGrantIDs: grantIDs,
		MaxOutputTokens: maxOut, MaxTotalTokens: plan.Budget.MaxTokens,
		MaxCostMicros: plan.Budget.MaxCostMicros, ToolTimeoutMillis: timeoutMS,
		ContextWindow: s.contextWindow,
	})
	if err != nil {
		bg := context.WithoutCancel(ctx)
		if errors.Is(err, context.Canceled) {
			state, stateErr := s.taskState(bg, task.ID)
			if stateErr == nil && (state == kernel.TaskPaused || state == kernel.TaskCancelled) {
				if state == kernel.TaskCancelled {
					s.cancelChat(bg, task.ID, runID, stepID)
				}
				// pause: leave run/step running so resume/inspect remain possible.
				slog.Info("chat run interrupted", "component", "chatsession", "operation", "execute", "result", state,
					"run_id", runID, "task_id", task.ID)
				return
			}
			if stateErr != nil {
				s.failChat(bg, task.ID, runID, stepID, errors.Join(err, stateErr))
				s.onError(err)
				return
			}
			// Context canceled without control transition (e.g. budget timeout race): treat as fail.
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("chat execution budget exceeded: %w", err)
		}
		s.failChat(bg, task.ID, runID, stepID, err)
		s.onError(err)
		return
	}
	if err := s.completeChat(context.WithoutCancel(ctx), task.ID, runID, stepID, result); err != nil {
		s.onError(err)
	}
}

func (s *Service) completeChat(ctx context.Context, taskID kernel.TaskID, runID kernel.RunID, stepID kernel.StepID, result agent.Result) error {
	// Control may have paused/cancelled while the model was finishing.
	if state, err := s.taskState(ctx, taskID); err == nil {
		if state == kernel.TaskCancelled {
			s.cancelChat(ctx, taskID, runID, stepID)
			return nil
		}
		if state == kernel.TaskPaused {
			slog.Info("chat run finished after pause", "component", "chatsession", "operation", "execute", "result", "paused",
				"run_id", runID, "task_id", taskID)
			return nil
		}
	}
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

// cancelChat marks the chat run finished after operator cancel. Task is already cancelled by taskcontrol.
// plan_steps has no cancelled terminal from running; leave step for inspection (run is authoritative).
func (s *Service) cancelChat(ctx context.Context, taskID kernel.TaskID, runID kernel.RunID, _ kernel.StepID) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	now := s.now().UTC()
	_, _ = tx.ExecContext(ctx, `
		UPDATE runs SET state = ?, version = version + 1, updated_at = ?, finished_at = ?, error = NULL
		WHERE run_id = ? AND state IN (?, ?)`,
		kernel.RunCancelled, formatTime(now), formatTime(now), runID, kernel.RunRunning, kernel.RunCreated,
	)
	_ = s.audit.RecordTx(ctx, tx, audit.Entry{
		OccurredAt: now, Actor: "agent", Action: "chat.execute", ResourceType: "run", ResourceID: string(runID),
		Outcome: "cancelled", TraceID: string(runID), Details: map[string]any{"task_id": taskID},
	})
	_ = tx.Commit()
	slog.Info("chat run cancelled", "component", "chatsession", "operation", "execute", "result", "cancelled",
		"run_id", runID, "task_id", taskID)
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

// skillSystemMessage loads the immutable task skill snapshot and builds a dedicated
// system message. Empty selection yields nil. Never re-reads SKILL.md files.
func (s *Service) skillSystemMessage(ctx context.Context, taskID kernel.TaskID) *providerapi.Message {
	if s == nil || s.repository == nil || taskID == "" {
		return nil
	}
	snapshot, err := s.repository.GetTaskSkillSnapshot(ctx, taskID)
	if err != nil {
		slog.Warn("chat skill snapshot load failed", "component", "chatsession", "operation", "skill_inject",
			"task_id", taskID, "error", err)
		return nil
	}
	instructions := strings.TrimSpace(snapshot.Instructions)
	if instructions == "" {
		return nil
	}
	content := skillSystemPreamble + "\n\n" + instructions
	return &providerapi.Message{Role: providerapi.RoleSystem, Content: content}
}

func (s *Service) buildWorkspacePlan(planID kernel.PlanID, taskID kernel.TaskID, mode kernel.ExecutionMode) approval.PlanDocument {
	caps := []approval.CapabilityScope{
		{Capability: "fs_read", Paths: append([]string(nil), s.roots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
		{Capability: "fs_list", Paths: append([]string(nil), s.roots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
		{Capability: "fs_stat", Paths: append([]string(nil), s.roots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
	}
	risk := policy.RiskR0
	effects := []string{"read workspace files when needed"}
	// Agent (build) gets write tools unless writeCeiling is false. Plan is always read-only.
	allowWrite := mode == kernel.ExecutionModeAgent && s.writeCeiling
	if allowWrite {
		caps = append(caps,
			approval.CapabilityScope{Capability: "fs_write", Paths: append([]string(nil), s.roots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
			approval.CapabilityScope{Capability: "fs_patch", Paths: append([]string(nil), s.roots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
			approval.CapabilityScope{Capability: "fs_mkdir", Paths: append([]string(nil), s.roots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
		)
		risk = policy.RiskR1
		effects = append(effects, "write workspace files when needed")
	}
	// High-risk tools: agent + config allowlist only (P4.3). Empty command/args = path-scoped (scheme A).
	if mode == kernel.ExecutionModeAgent && s.allowGit {
		for _, name := range []string{"git_status", "git_diff", "git_add", "git_commit"} {
			caps = append(caps, approval.CapabilityScope{
				Capability: name, Paths: append([]string(nil), s.roots...),
				MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls,
			})
		}
		risk = policy.RiskR2
		effects = append(effects, "use git tools under workspace roots")
	}
	if mode == kernel.ExecutionModeAgent && s.allowProcess {
		caps = append(caps, approval.CapabilityScope{
			Capability: "process_exec", Paths: append([]string(nil), s.roots...),
			MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls,
		})
		risk = policy.RiskR2
		effects = append(effects, "execute processes under workspace roots")
	}
	// Logical sub-agent (ADR-039): both modes may spawn task; grants do not expand FS.
	caps = append(caps, approval.CapabilityScope{
		Capability: "task", MaxDurationMillis: defaultMaxDurationMS, MaxCalls: defaultMaxCalls,
	})
	effects = append(effects, "delegate synchronous sub-agent tasks")
	if risk == policy.RiskR0 {
		risk = policy.RiskR1
	}
	for _, name := range s.extraTools {
		caps = append(caps, approval.CapabilityScope{
			Capability: name, MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls,
		})
	}
	if len(s.extraTools) > 0 {
		effects = append(effects, "use configured MCP tools")
		if risk == policy.RiskR0 {
			risk = policy.RiskR2
		} else if risk == policy.RiskR1 {
			risk = policy.RiskR2
		}
	}
	objective := "session chat workspace tools"
	title := "session chat"
	if mode == kernel.ExecutionModePlan {
		objective = "session plan-mode read-only workspace tools"
		title = "session plan (read-only)"
	}
	return approval.PlanDocument{
		PlanID: planID, TaskID: taskID, Revision: 1,
		Objective: objective,
		Budget: approval.PlanBudget{
			MaxTokens: defaultMaxTokens, MaxCostMicros: 0, MaxDurationMillis: defaultMaxDurationMS,
		},
		Steps: []approval.StepScope{{
			StepID: chatStepID(taskID), Position: 0, Title: title,
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
	if s.active == nil {
		s.active = make(map[kernel.TaskID]context.CancelFunc)
	}
	if prev := s.active[taskID]; prev != nil {
		prev()
	}
	s.active[taskID] = cancel
}

func (s *Service) clearActive(taskID kernel.TaskID) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	delete(s.active, taskID)
}

func (s *Service) taskState(ctx context.Context, taskID kernel.TaskID) (kernel.TaskState, error) {
	var state string
	if err := s.db.QueryRowContext(ctx, "SELECT state FROM tasks WHERE task_id = ?", taskID).Scan(&state); err != nil {
		return "", fmt.Errorf("load task state: %w", err)
	}
	return kernel.TaskState(state), nil
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

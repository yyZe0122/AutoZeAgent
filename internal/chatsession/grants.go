package chatsession

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/applicationerror"
	"github.com/yyZe0122/yunmengze-agent/internal/approval"
	"github.com/yyZe0122/yunmengze-agent/internal/audit"
	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	"github.com/yyZe0122/yunmengze-agent/internal/policy"
)

func (s *Service) resolveGrantRoots(ctx context.Context, task kernel.Task) []string {
	workspace := ""
	if sess, err := s.repository.GetSession(ctx, task.SessionID); err == nil {
		workspace = strings.TrimSpace(sess.Workspace)
	}
	if s.chatCfg != nil {
		if workspace == "" {
			workspace = s.chatCfg.ResolveSessionWorkspace("", s.daemonCWD)
		}
		if workspace == "" && len(s.roots) > 0 {
			workspace = s.roots[0]
		}
		roots := s.chatCfg.GrantRootsForSession(workspace)
		if len(roots) > 0 {
			if s.pathGuard != nil && workspace != "" {
				_ = s.pathGuard.AddRoot(workspace)
			}
			return roots
		}
	}
	if workspace != "" {
		if s.pathGuard != nil {
			_ = s.pathGuard.AddRoot(workspace)
		}
		out := []string{workspace}
		for _, r := range s.roots {
			if r != workspace {
				out = append(out, r)
			}
		}
		return out
	}
	return append([]string(nil), s.roots...)
}

// SetContextWindow updates packing window for subsequent chat turns.
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

	grantRoots := s.resolveGrantRoots(ctx, task)
	if len(grantRoots) == 0 {
		return approval.PlanDocument{}, "", task, applicationerror.Wrap(applicationerror.CodeInvalidRequest, false,
			fmt.Errorf("%w: no workspace roots for session", ErrInvalidRequest))
	}
	plan := s.buildWorkspacePlan(planID, task.ID, kernel.NormalizeExecutionMode(string(task.ExecutionMode)), grantRoots)
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
		// ask-mode high-risk scopes (OneTime true or process/git without preauth) are issued only after decide.
		if s.permissionMode == "ask" && isAskOnlyCapability(scope) && !s.preauthHighRisk(scope.Capability) {
			continue
		}
		// Prefer non-one-time scopes for preauth (session grants).
		if scope.OneTime && s.permissionMode == "ask" {
			continue
		}
		grantID := approval.GrantID(deterministicID("chat-grant", approvalID, string(step.StepID), scope.Capability, fmt.Sprintf("%v", scope.OneTime), fmt.Sprintf("%d", scope.MaxCalls)))
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

func isAskOnlyCapability(scope approval.CapabilityScope) bool {
	name := scope.Capability
	if name == "process_exec" || strings.HasPrefix(name, "git_") {
		return true
	}
	return false
}

func (s *Service) preauthHighRisk(capability string) bool {
	if capability == "process_exec" {
		return s.allowProcess
	}
	if strings.HasPrefix(capability, "git_") {
		return s.allowGit
	}
	return false
}

func (s *Service) buildWorkspacePlan(planID kernel.PlanID, taskID kernel.TaskID, mode kernel.ExecutionMode, roots []string) approval.PlanDocument {
	if len(roots) == 0 {
		roots = append([]string(nil), s.roots...)
	}
	pathRoots := append([]string(nil), roots...)
	caps := []approval.CapabilityScope{
		{Capability: "fs_read", Paths: pathRoots, MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
		{Capability: "fs_list", Paths: append([]string(nil), pathRoots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
		{Capability: "fs_stat", Paths: append([]string(nil), pathRoots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
		{Capability: "fs_glob", Paths: append([]string(nil), pathRoots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
		{Capability: "fs_grep", Paths: append([]string(nil), pathRoots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
	}
	risk := policy.RiskR0
	effects := []string{"read workspace files when needed"}
	// Agent (build) gets write tools unless writeCeiling is false. Plan is always read-only.
	allowWrite := mode == kernel.ExecutionModeAgent && s.writeCeiling
	if allowWrite {
		caps = append(caps,
			approval.CapabilityScope{Capability: "fs_write", Paths: append([]string(nil), pathRoots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
			approval.CapabilityScope{Capability: "fs_patch", Paths: append([]string(nil), pathRoots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
			approval.CapabilityScope{Capability: "fs_mkdir", Paths: append([]string(nil), pathRoots...), MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls},
		)
		risk = policy.RiskR1
		effects = append(effects, "write workspace files when needed")
	}
	// High-risk tools: agent + config allowlist only (P4.3). Empty command/args = path-scoped (scheme A).
	// ask mode (ADR-043): embed once+session plan scopes without pre-issuing grants (issueChatGrants skips these).
	askMode := mode == kernel.ExecutionModeAgent && s.permissionMode == "ask"
	if mode == kernel.ExecutionModeAgent && (s.allowGit || askMode) {
		for _, name := range []string{"git_status", "git_diff", "git_add", "git_commit"} {
			if s.allowGit {
				caps = append(caps, approval.CapabilityScope{
					Capability: name, Paths: append([]string(nil), pathRoots...),
					MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls,
				})
			}
			if askMode && !s.allowGit {
				// Interactive: plan contains scopes for IssueGrant after decide.
				caps = append(caps,
					approval.CapabilityScope{
						Capability: name, Paths: append([]string(nil), pathRoots...),
						MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: 1, OneTime: true,
					},
					approval.CapabilityScope{
						Capability: name, Paths: append([]string(nil), pathRoots...),
						MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls, OneTime: false,
					},
				)
			}
		}
		if s.allowGit || askMode {
			risk = policy.RiskR2
			effects = append(effects, "use git tools under workspace roots")
		}
	}
	if mode == kernel.ExecutionModeAgent && (s.allowProcess || askMode) {
		if s.allowProcess {
			caps = append(caps, approval.CapabilityScope{
				Capability: "process_exec", Paths: append([]string(nil), pathRoots...),
				MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls,
			})
		}
		if askMode && !s.allowProcess {
			caps = append(caps,
				approval.CapabilityScope{
					Capability: "process_exec", Paths: append([]string(nil), pathRoots...),
					MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: 1, OneTime: true,
				},
				approval.CapabilityScope{
					Capability: "process_exec", Paths: append([]string(nil), pathRoots...),
					MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls, OneTime: false,
				},
			)
		}
		if s.allowProcess || askMode {
			risk = policy.RiskR2
			effects = append(effects, "execute processes under workspace roots")
		}
	}
	// http_get ask scopes (network domain filled at decide via plan template with empty domains fails —
	// http_get is not path-scoped; skip plan embed for http until domain-wildcard grants exist.
	// Logical sub-agent (ADR-039): both modes may spawn task; grants do not expand FS.
	caps = append(caps, approval.CapabilityScope{
		Capability: "task", MaxDurationMillis: defaultMaxDurationMS, MaxCalls: defaultMaxCalls,
	})
	effects = append(effects, "delegate synchronous sub-agent tasks")
	// In-process memory tools (ADR-044): search is R0; write/promote need plan scope + grant (agent only).
	caps = append(caps,
		approval.CapabilityScope{
			Capability: "memory_search", MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls,
		},
		approval.CapabilityScope{
			Capability: "session_search", MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls,
		},
	)
	if mode == kernel.ExecutionModeAgent {
		caps = append(caps,
			approval.CapabilityScope{
				Capability: "memory_write", MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls,
			},
			approval.CapabilityScope{
				Capability: "memory_promote", MaxDurationMillis: defaultToolTimeoutMS, MaxCalls: defaultMaxCalls,
			},
		)
		effects = append(effects, "search/write local memory and past transcripts")
		if risk == policy.RiskR0 {
			risk = policy.RiskR1
		}
	} else {
		effects = append(effects, "search local memory and past transcripts")
	}
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

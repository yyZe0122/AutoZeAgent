package chatsession

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"autozeagent.local/autozeagent/internal/agent"
	"autozeagent.local/autozeagent/internal/applicationerror"
	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/audit"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/runlog"
	"autozeagent.local/autozeagent/pkg/providerapi"
)

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
	if s.memory != nil {
		// Hermes-style frozen snapshot: built once per session until /refresh-memory.
		block := s.memory.FrozenSystemBlock(ctx, string(task.SessionID))
		if block != "" {
			messages = append(messages, providerapi.Message{Role: providerapi.RoleSystem, Content: block})
		}
	}
	messages = append(messages, providerapi.Message{Role: providerapi.RoleUser, Content: userText})
	// Plan budget MaxTokens is a run ceiling; model output cap is separate.
	// Prefer a sane output cap so usable window is not zeroed by huge MaxTokens.
	maxOut := plan.Budget.MaxTokens
	if maxOut <= 0 || maxOut > 16_384 {
		maxOut = 8_192
	}
	// Preserve non-interactive actors (scheduler/job) for Broker permission policy (ADR-043).
	runActor := "agent"
	if strings.HasPrefix(string(task.ID), "scheduled_") {
		runActor = "scheduler"
	}
	result, err := s.agent.Run(ctx, agent.RunRequest{
		RunID: string(runID), TaskID: string(task.ID), SessionID: string(task.SessionID),
		PlanID: string(plan.PlanID), PlanHash: planHash, StepID: string(stepID),
		Actor: runActor, TraceID: string(runID),
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
					s.cancelChat(bg, task, runID, stepID)
				}
				// pause: leave run/step running so resume/inspect remain possible.
				slog.Info("chat run interrupted", runlog.Attrs("chatsession", "execute", string(state), runlog.IDs{
					SessionID: string(task.SessionID), TaskID: string(task.ID), RunID: string(runID),
					PlanID: string(plan.PlanID), StepID: string(stepID),
				})...)
				return
			}
			if stateErr != nil {
				s.failChat(bg, task, runID, stepID, errors.Join(err, stateErr))
				s.onError(err)
				return
			}
			// Context canceled without control transition (e.g. budget timeout race): treat as fail.
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("chat execution budget exceeded: %w", err)
		}
		s.failChat(bg, task, runID, stepID, err)
		s.onError(err)
		return
	}
	if err := s.completeChat(context.WithoutCancel(ctx), task, runID, stepID, result); err != nil {
		s.onError(err)
		return
	}
	if s.memory != nil {
		bg := context.WithoutCancel(ctx)
		s.memory.SyncTurn(bg, string(task.SessionID), userText, result.Content)
		// L3 transcript projection for session_search (user + assistant only).
		nowRFC := formatTime(s.now())
		_ = s.memory.IndexTranscriptRecord(bg, string(task.SessionID), string(runID), 0, "user", userText, nowRFC)
		if strings.TrimSpace(result.Content) != "" {
			_ = s.memory.IndexTranscriptRecord(bg, string(task.SessionID), string(runID), 1, "assistant", result.Content, nowRFC)
		}
	}
}

// RefreshMemory invalidates the frozen system memory snapshot for a session
// (empty sessionID clears all). Next chat turn rebuilds the inject block.
func (s *Service) completeChat(ctx context.Context, task kernel.Task, runID kernel.RunID, stepID kernel.StepID, result agent.Result) error {
	taskID := task.ID
	// Control may have paused/cancelled while the model was finishing.
	if state, err := s.taskState(ctx, taskID); err == nil {
		if state == kernel.TaskCancelled {
			s.cancelChat(ctx, task, runID, stepID)
			return nil
		}
		if state == kernel.TaskPaused {
			slog.Info("chat run finished after pause", runlog.Attrs("chatsession", "execute", "paused", runlog.IDs{
				SessionID: string(task.SessionID), TaskID: string(taskID), RunID: string(runID),
			})...)
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
	if err := tx.Commit(); err != nil {
		return err
	}
	s.publishTerminal(task, runID)
	slog.Info("chat run completed", runlog.Attrs("chatsession", "execute", "succeeded", runlog.IDs{
		SessionID: string(task.SessionID), TaskID: string(taskID), RunID: string(runID),
		StepID: string(stepID),
	}, "iterations", result.Iterations, "tool_calls", len(result.ToolCalls))...)
	return nil
}

// cancelChat marks the chat run finished after operator cancel. Task is already cancelled by taskcontrol.
// plan_steps has no cancelled terminal from running; leave step for inspection (run is authoritative).
func (s *Service) cancelChat(ctx context.Context, task kernel.Task, runID kernel.RunID, _ kernel.StepID) {
	taskID := task.ID
	s.cancelIncompleteTools(ctx, runID)
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
	s.publishTerminal(task, runID)
	slog.Info("chat run cancelled", runlog.Attrs("chatsession", "execute", "cancelled", runlog.IDs{
		SessionID: string(task.SessionID), TaskID: string(taskID), RunID: string(runID),
	})...)
}

func (s *Service) failChat(ctx context.Context, task kernel.Task, runID kernel.RunID, stepID kernel.StepID, cause error) {
	taskID := task.ID
	failure := strings.TrimSpace(cause.Error())
	if len(failure) > 2048 {
		failure = failure[:2048]
	}
	s.cancelIncompleteTools(ctx, runID)
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
	s.publishTerminal(task, runID)
	slog.Error("chat run failed", runlog.Attrs("chatsession", "execute", "failed", runlog.IDs{
		SessionID: string(task.SessionID), TaskID: string(taskID), RunID: string(runID),
		StepID: string(stepID),
	}, "error", cause)...)
}

// cancelIncompleteTools asks the Tool Broker to finish still-running tool_calls for runID.
func (s *Service) cancelIncompleteTools(ctx context.Context, runID kernel.RunID) {
	if s == nil || s.toolCalls == nil || runID == "" {
		return
	}
	if _, err := s.toolCalls.CancelIncompleteToolCalls(ctx, string(runID)); err != nil {
		slog.Warn("cancel incomplete tool calls failed", runlog.Attrs("chatsession", "cancel_tools", "failed", runlog.IDs{
			RunID: string(runID),
		}, "error", err)...)
	}
}

func (s *Service) publishTerminal(task kernel.Task, runID kernel.RunID) {
	if s == nil || s.stream == nil {
		return
	}
	s.stream.PublishTerminal(string(task.SessionID), string(task.ID), string(runID))
}

// skillSystemMessage loads the immutable task skill snapshot and builds a dedicated
// system message. Empty selection yields nil. Never re-reads SKILL.md files.
func (s *Service) skillSystemMessage(ctx context.Context, taskID kernel.TaskID) *providerapi.Message {
	if s == nil || s.repository == nil || taskID == "" {
		return nil
	}
	snapshot, err := s.repository.GetTaskSkillSnapshot(ctx, taskID)
	if err != nil {
		slog.Warn("chat skill snapshot load failed", runlog.Attrs("chatsession", "skill_inject", "warning", runlog.IDs{
			TaskID: string(taskID),
		}, "error", err)...)
		return nil
	}
	instructions := strings.TrimSpace(snapshot.Instructions)
	if instructions == "" {
		return nil
	}
	content := skillSystemPreamble + "\n\n" + instructions
	return &providerapi.Message{Role: providerapi.RoleSystem, Content: content}
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
	case errors.Is(err, ErrUnavailable):
		return applicationerror.Wrap(applicationerror.CodeUnavailable, false, err)
	default:
		return err
	}
}

package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"autozeagent.local/autozeagent/internal/coreidentity"
	"autozeagent.local/autozeagent/internal/events"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/pkg/eventapi"
	"autozeagent.local/autozeagent/pkg/sqliteerror"
)

type ApprovalID = coreidentity.ApprovalID
type GrantID = coreidentity.GrantID

type ScopeType string

const (
	ScopePlan ScopeType = "plan"
	ScopeStep ScopeType = "step"
)

type Decision string

const (
	DecisionApproved         Decision = "approved"
	DecisionRejected         Decision = "rejected"
	DecisionChangesRequested Decision = "changes_requested"
)

var (
	ErrInvalidDecision = errors.New("invalid approval decision")
	ErrPlanChanged     = errors.New("plan changed")
	ErrApprovalClosed  = errors.New("approval is no longer pending")
	ErrNotApproved     = errors.New("plan scope is not approved")
	ErrAlreadyExists   = errors.New("approval or grant already exists")
)

type Approval struct {
	ID            ApprovalID
	PlanID        kernel.PlanID
	PlanRevision  uint64
	PlanHash      string
	Scope         ScopeType
	StepID        kernel.StepID
	Decision      Decision
	DecidedBy     string
	Reason        string
	DecidedAt     time.Time
	ExpiresAt     *time.Time
	InvalidatedAt *time.Time
}

func (a Approval) ScopeSummary() string {
	if a.Scope == ScopeStep {
		return fmt.Sprintf("plan %s revision %d step %s hash %s", a.PlanID, a.PlanRevision, a.StepID, a.PlanHash)
	}
	return fmt.Sprintf("plan %s revision %d all steps hash %s", a.PlanID, a.PlanRevision, a.PlanHash)
}

type DecisionInput struct {
	ID        ApprovalID
	Plan      PlanDocument
	Scope     ScopeType
	StepID    kernel.StepID
	Decision  Decision
	DecidedBy string
	Reason    string
	DecidedAt time.Time
	ExpiresAt *time.Time
}

type Repository struct {
	db     *sql.DB
	events *events.Store
}

func NewRepository(db *sql.DB) (*Repository, error) {
	store, err := events.NewStore(db)
	if err != nil {
		return nil, err
	}
	return &Repository{db: db, events: store}, nil
}

func (r *Repository) Decide(ctx context.Context, input DecisionInput) (Approval, error) {
	if ctx == nil {
		return Approval{}, errors.New("approval context is required")
	}
	if r == nil || r.db == nil {
		return Approval{}, errors.New("approval repository is unavailable")
	}
	if strings.TrimSpace(string(input.ID)) == "" || strings.TrimSpace(input.DecidedBy) == "" {
		return Approval{}, fmt.Errorf("%w: approval ID and decision maker are required", ErrInvalidDecision)
	}
	if input.Decision != DecisionApproved && input.Decision != DecisionRejected && input.Decision != DecisionChangesRequested {
		return Approval{}, fmt.Errorf("%w: %q", ErrInvalidDecision, input.Decision)
	}
	if input.Scope != ScopePlan && input.Scope != ScopeStep {
		return Approval{}, fmt.Errorf("%w: scope %q", ErrInvalidDecision, input.Scope)
	}
	if input.Scope == ScopeStep && strings.TrimSpace(string(input.StepID)) == "" {
		return Approval{}, fmt.Errorf("%w: step scope requires a step ID", ErrInvalidDecision)
	}
	if input.Scope == ScopePlan && strings.TrimSpace(string(input.StepID)) != "" {
		return Approval{}, fmt.Errorf("%w: plan scope cannot include a step ID", ErrInvalidDecision)
	}

	planHash, err := input.Plan.Hash()
	if err != nil {
		return Approval{}, err
	}
	decidedAt := normalizeTime(input.DecidedAt)
	var expiresAt *time.Time
	if input.ExpiresAt != nil {
		normalized := input.ExpiresAt.UTC()
		if !normalized.After(decidedAt) {
			return Approval{}, fmt.Errorf("%w: expiration must be after the decision", ErrInvalidDecision)
		}
		expiresAt = &normalized
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Approval{}, fmt.Errorf("begin approval decision: %w", err)
	}
	defer tx.Rollback()

	if err := requireCurrentPlan(ctx, tx, input.Plan, planHash); err != nil {
		return Approval{}, err
	}
	if err := requireApprovalOpen(ctx, tx, input.Plan); err != nil {
		return Approval{}, err
	}
	if input.Scope == ScopeStep {
		if err := requirePlanStep(ctx, tx, input.Plan.PlanID, input.StepID); err != nil {
			return Approval{}, err
		}
	}

	var expires any
	if expiresAt != nil {
		expires = formatTime(*expiresAt)
	}
	_, err = tx.ExecContext(ctx, `
        INSERT INTO approvals (
            approval_id, plan_id, plan_revision, decision, scope_hash,
            decided_by, decided_at, expires_at, scope_type, step_id, reason, invalidated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		input.ID, input.Plan.PlanID, input.Plan.Revision, input.Decision, planHash,
		strings.TrimSpace(input.DecidedBy), formatTime(decidedAt), expires,
		input.Scope, nullableStepID(input.StepID), strings.TrimSpace(input.Reason),
	)
	if err != nil {
		if sqliteerror.IsUniqueConstraint(err) {
			return Approval{}, fmt.Errorf("%w: %s", ErrAlreadyExists, input.ID)
		}
		return Approval{}, fmt.Errorf("insert approval: %w", err)
	}
	if input.Scope == ScopePlan {
		if err := applyPlanDecision(ctx, tx, input.Plan, input.Decision, decidedAt); err != nil {
			return Approval{}, err
		}
	}

	approval := Approval{
		ID: input.ID, PlanID: input.Plan.PlanID, PlanRevision: input.Plan.Revision,
		PlanHash: planHash, Scope: input.Scope, StepID: input.StepID,
		Decision: input.Decision, DecidedBy: strings.TrimSpace(input.DecidedBy),
		Reason: strings.TrimSpace(input.Reason), DecidedAt: decidedAt, ExpiresAt: expiresAt,
	}
	event, err := approvalEvent(approval)
	if err != nil {
		return Approval{}, err
	}
	if _, err := r.events.AppendTx(ctx, tx, event); err != nil {
		return Approval{}, fmt.Errorf("append approval event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Approval{}, fmt.Errorf("commit approval decision: %w", err)
	}
	return approval, nil
}

// IsApproved accepts either a current whole-plan approval or a current approval
// for the requested step. A revision or hash change makes old records unusable.
func (r *Repository) IsApproved(ctx context.Context, plan PlanDocument, stepID kernel.StepID, now time.Time) (bool, error) {
	if ctx == nil {
		return false, errors.New("approval context is required")
	}
	planHash, err := plan.Hash()
	if err != nil {
		return false, err
	}
	now = normalizeTime(now)
	var count int
	err = r.db.QueryRowContext(ctx, `
        SELECT COUNT(*)
        FROM approvals
        WHERE plan_id = ?
          AND plan_revision = ?
          AND scope_hash = ?
          AND decision = ?
          AND invalidated_at IS NULL
          AND (expires_at IS NULL OR expires_at > ?)
          AND (scope_type = ? OR (scope_type = ? AND step_id = ?))`,
		plan.PlanID, plan.Revision, planHash, DecisionApproved, formatTime(now),
		ScopePlan, ScopeStep, nullableStepID(stepID),
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query current approval: %w", err)
	}
	return count > 0, nil
}

// InvalidateForPlanChange records that approvals for older hashes can no longer
// be displayed as current. Authorization remains hash-bound even if this method
// is not called, so safety does not depend on mutable invalidation metadata.
func (r *Repository) InvalidateForPlanChange(ctx context.Context, planID kernel.PlanID, currentHash string, now time.Time) (int64, error) {
	if ctx == nil {
		return 0, errors.New("approval context is required")
	}
	if strings.TrimSpace(string(planID)) == "" || strings.TrimSpace(currentHash) == "" {
		return 0, fmt.Errorf("%w: plan ID and current hash are required", ErrInvalidPlan)
	}
	result, err := r.db.ExecContext(ctx, `
        UPDATE approvals
        SET invalidated_at = ?
        WHERE plan_id = ? AND scope_hash <> ? AND invalidated_at IS NULL`,
		formatTime(normalizeTime(now)), planID, currentHash,
	)
	if err != nil {
		return 0, fmt.Errorf("invalidate superseded approvals: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read invalidated approval count: %w", err)
	}
	return affected, nil
}

func requireCurrentPlan(ctx context.Context, tx *sql.Tx, plan PlanDocument, planHash string) error {
	var taskID, storedHash string
	var revision int64
	err := tx.QueryRowContext(ctx,
		"SELECT task_id, revision, scope_hash FROM plans WHERE plan_id = ?", plan.PlanID,
	).Scan(&taskID, &revision, &storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return kernel.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("query current plan: %w", err)
	}
	if taskID != string(plan.TaskID) || revision != int64(plan.Revision) || storedHash != planHash {
		return fmt.Errorf("%w: stored plan revision or hash differs", ErrPlanChanged)
	}
	return nil
}

func requireApprovalOpen(ctx context.Context, tx *sql.Tx, plan PlanDocument) error {
	var planState, taskState string
	err := tx.QueryRowContext(ctx, `
        SELECT p.state, t.state
        FROM plans p
        JOIN tasks t ON t.task_id = p.task_id
        WHERE p.plan_id = ? AND p.task_id = ?`, plan.PlanID, plan.TaskID,
	).Scan(&planState, &taskState)
	if errors.Is(err, sql.ErrNoRows) {
		return kernel.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("query approval state: %w", err)
	}
	if planState != string(kernel.PlanWaitingApproval) || taskState != string(kernel.TaskWaitingApproval) {
		return fmt.Errorf("%w: plan state %s, task state %s", ErrApprovalClosed, planState, taskState)
	}
	return nil
}

func applyPlanDecision(ctx context.Context, tx *sql.Tx, plan PlanDocument, decision Decision, decidedAt time.Time) error {
	var planState kernel.PlanState
	var taskState kernel.TaskState
	switch decision {
	case DecisionApproved:
		planState = kernel.PlanApproved
		taskState = kernel.TaskApproved
	case DecisionRejected:
		planState = kernel.PlanRejected
		taskState = kernel.TaskCancelled
	case DecisionChangesRequested:
		planState = kernel.PlanDraft
		taskState = kernel.TaskPlanning
	default:
		return fmt.Errorf("%w: %q", ErrInvalidDecision, decision)
	}

	updatedAt := formatTime(decidedAt)
	result, err := tx.ExecContext(ctx, `
        UPDATE plans
        SET state = ?, version = version + 1, updated_at = ?
        WHERE plan_id = ? AND task_id = ? AND state = ?`,
		planState, updatedAt, plan.PlanID, plan.TaskID, kernel.PlanWaitingApproval,
	)
	if err != nil {
		return fmt.Errorf("update plan decision state: %w", err)
	}
	if err := requireOneUpdatedRow(result, "plan"); err != nil {
		return err
	}

	result, err = tx.ExecContext(ctx, `
        UPDATE tasks
        SET state = ?, version = version + 1, updated_at = ?
        WHERE task_id = ? AND state = ?`,
		taskState, updatedAt, plan.TaskID, kernel.TaskWaitingApproval,
	)
	if err != nil {
		return fmt.Errorf("update task decision state: %w", err)
	}
	return requireOneUpdatedRow(result, "task")
}

func requireOneUpdatedRow(result sql.Result, aggregate string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count updated %s rows: %w", aggregate, err)
	}
	if count != 1 {
		return fmt.Errorf("%w: %s state changed while deciding", ErrApprovalClosed, aggregate)
	}
	return nil
}
func requirePlanStep(ctx context.Context, tx *sql.Tx, planID kernel.PlanID, stepID kernel.StepID) error {
	var count int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM plan_steps WHERE plan_id = ? AND step_id = ?", planID, stepID,
	).Scan(&count); err != nil {
		return fmt.Errorf("query plan step: %w", err)
	}
	if count != 1 {
		return kernel.ErrNotFound
	}
	return nil
}

func approvalEvent(approval Approval) (eventapi.Envelope, error) {
	payload, err := json.Marshal(map[string]any{
		"plan_id":       approval.PlanID,
		"plan_revision": approval.PlanRevision,
		"plan_hash":     approval.PlanHash,
		"scope":         approval.Scope,
		"step_id":       approval.StepID,
		"decision":      approval.Decision,
		"decided_by":    approval.DecidedBy,
		"reason":        approval.Reason,
		"expires_at":    approval.ExpiresAt,
	})
	if err != nil {
		return eventapi.Envelope{}, fmt.Errorf("marshal approval event: %w", err)
	}
	return eventapi.Envelope{
		ID:   fmt.Sprintf("approval/%s/v/1", approval.ID),
		Type: "approval.decided", AggregateType: "approval", AggregateID: string(approval.ID),
		AggregateVersion: 1, OccurredAt: approval.DecidedAt, Producer: "approval",
		SchemaVersion: 1, Payload: payload,
	}, nil
}

func nullableStepID(stepID kernel.StepID) any {
	if strings.TrimSpace(string(stepID)) == "" {
		return nil
	}
	return stepID
}

func normalizeTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

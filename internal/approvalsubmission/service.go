// Package approvalsubmission coordinates the user-facing approval decision use case.
// Canonical Plan validation remains separate from HTTP transport concerns, while
// persistence and approval invariants remain owned by the approval repository.
package approvalsubmission

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"autozeagent.local/autozeagent/internal/applicationerror"
	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
)

var (
	ErrInvalidRequest          = errors.New("invalid approval submission")
	ErrPlanChanged             = errors.New("approval submission plan changed")
	ErrPlanDocumentUnavailable = errors.New("stored plan document is unavailable")
)

type PlanDocumentLoader interface {
	LoadPlanDocument(context.Context, kernel.PlanID) (corequery.StoredPlanDocument, error)
}

type Repository interface {
	Decide(context.Context, approval.DecisionInput) (approval.Approval, error)
}

type Config struct {
	Plans      PlanDocumentLoader
	Repository Repository
	Now        func() time.Time
	NewID      func(string) (string, error)
}

type Service struct {
	plans      PlanDocumentLoader
	repository Repository
	now        func() time.Time
	newID      func(string) (string, error)
}

type Request struct {
	ApprovalID approval.ApprovalID
	PlanID     kernel.PlanID
	Revision   uint64
	PlanHash   string
	Scope      approval.ScopeType
	StepID     kernel.StepID
	Decision   approval.Decision
	DecidedBy  string
	Reason     string
	ExpiresAt  *time.Time
}

func New(config Config) (*Service, error) {
	if config.Plans == nil || config.Repository == nil {
		return nil, errors.New("approval submission plan loader and repository are required")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.NewID == nil {
		config.NewID = randomID
	}
	return &Service{
		plans: config.Plans, repository: config.Repository,
		now: config.Now, newID: config.NewID,
	}, nil
}

func (s *Service) Decide(ctx context.Context, request Request) (approval.Approval, error) {
	if ctx == nil {
		return approval.Approval{}, errors.New("approval submission context is required")
	}
	request.PlanID = kernel.PlanID(strings.TrimSpace(string(request.PlanID)))
	request.PlanHash = strings.TrimSpace(request.PlanHash)
	if request.PlanID == "" || request.Revision == 0 || request.PlanHash == "" {
		return approval.Approval{}, applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, fmt.Errorf("%w: plan ID, revision, and hash are required", ErrInvalidRequest))
	}

	stored, err := s.plans.LoadPlanDocument(ctx, request.PlanID)
	if err != nil {
		return approval.Approval{}, classifyError(err)
	}
	plan, err := validateStoredPlan(request.PlanID, stored)
	if err != nil {
		return approval.Approval{}, err
	}
	if err := requireWaitingApproval(stored); err != nil {
		return approval.Approval{}, err
	}
	if request.Revision != stored.Revision || request.PlanHash != stored.Hash {
		return approval.Approval{}, applicationerror.Wrap(applicationerror.CodePlanChanged, false, fmt.Errorf("%w: stored plan revision or hash differs", ErrPlanChanged))
	}
	if request.ApprovalID == "" {
		value, err := s.newID("approval-")
		if err != nil {
			return approval.Approval{}, fmt.Errorf("generate approval ID: %w", err)
		}
		request.ApprovalID = approval.ApprovalID(value)
	}

	decision, err := s.repository.Decide(ctx, approval.DecisionInput{
		ID: request.ApprovalID, Plan: plan, Scope: request.Scope, StepID: request.StepID,
		Decision: request.Decision, DecidedBy: request.DecidedBy, Reason: request.Reason,
		DecidedAt: s.now(), ExpiresAt: request.ExpiresAt,
	})
	return decision, classifyError(err)
}

func classifyError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrInvalidRequest), errors.Is(err, approval.ErrInvalidDecision), errors.Is(err, approval.ErrInvalidPlan):
		return applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, err)
	case errors.Is(err, ErrPlanChanged):
		return applicationerror.Wrap(applicationerror.CodePlanChanged, false, err)
	case errors.Is(err, ErrPlanDocumentUnavailable):
		return applicationerror.Wrap(applicationerror.CodePlanDocumentUnavailable, false, err)
	case errors.Is(err, approval.ErrPlanChanged), errors.Is(err, approval.ErrApprovalClosed), errors.Is(err, approval.ErrAlreadyExists):
		return applicationerror.Wrap(applicationerror.CodeConflict, false, err)
	case errors.Is(err, corequery.ErrNotFound), errors.Is(err, kernel.ErrNotFound):
		return applicationerror.Wrap(applicationerror.CodeNotFound, false, err)
	default:
		return err
	}
}

func requireWaitingApproval(stored corequery.StoredPlanDocument) error {
	if stored.PlanState != string(kernel.PlanWaitingApproval) || stored.TaskState != string(kernel.TaskWaitingApproval) {
		return classifyError(fmt.Errorf("%w: plan state %s, task state %s", approval.ErrApprovalClosed, stored.PlanState, stored.TaskState))
	}
	return nil
}

func validateStoredPlan(id kernel.PlanID, stored corequery.StoredPlanDocument) (approval.PlanDocument, error) {
	var plan approval.PlanDocument
	if err := json.Unmarshal(stored.Document, &plan); err != nil {
		return approval.PlanDocument{}, applicationerror.Wrap(applicationerror.CodePlanDocumentUnavailable, false, fmt.Errorf("%w: decode stored document: %v", ErrPlanDocumentUnavailable, err))
	}
	computed, err := plan.Hash()
	if err != nil {
		return approval.PlanDocument{}, applicationerror.Wrap(applicationerror.CodePlanDocumentUnavailable, false, fmt.Errorf("%w: validate stored document: %v", ErrPlanDocumentUnavailable, err))
	}
	if computed != stored.Hash || plan.Revision != stored.Revision || plan.PlanID != id {
		return approval.PlanDocument{}, applicationerror.Wrap(applicationerror.CodePlanDocumentUnavailable, false, fmt.Errorf("%w: stored metadata does not match canonical document", ErrPlanDocumentUnavailable))
	}
	return plan, nil
}

func randomID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value), nil
}

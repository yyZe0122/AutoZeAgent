package gateway

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
)

func (a *API) handlePlans(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	request, ok := parseListRequest(w, r)
	if !ok {
		return
	}
	items, err := a.queries.ListPlans(r.Context(), corequery.PlanListOptions{
		Page: request.Page, Sort: request.Sort, State: strings.TrimSpace(r.URL.Query().Get("state")),
	})
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": items, "page": request.metadata(len(items))})
}

func (a *API) handlePlan(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	id, ok := pathID(w, r.URL.Path, "/v1/plans/")
	if !ok {
		return
	}
	item, err := a.queries.GetPlan(r.Context(), kernel.PlanID(id))
	if errors.Is(err, corequery.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "plan not found")
		return
	}
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type approvalView struct {
	ID            string  `json:"approval_id"`
	PlanID        string  `json:"plan_id"`
	PlanRevision  uint64  `json:"plan_revision"`
	Decision      string  `json:"decision"`
	ScopeHash     string  `json:"scope_hash"`
	DecidedBy     string  `json:"decided_by"`
	DecidedAt     string  `json:"decided_at"`
	ExpiresAt     *string `json:"expires_at,omitempty"`
	Scope         string  `json:"scope"`
	StepID        *string `json:"step_id,omitempty"`
	Reason        string  `json:"reason"`
	InvalidatedAt *string `json:"invalidated_at,omitempty"`
}

func approvalViewFromDomain(value approval.Approval) approvalView {
	item := approvalView{
		ID: string(value.ID), PlanID: string(value.PlanID), PlanRevision: value.PlanRevision,
		Decision: string(value.Decision), ScopeHash: value.PlanHash, DecidedBy: value.DecidedBy,
		DecidedAt: value.DecidedAt.UTC().Format(time.RFC3339Nano), Scope: string(value.Scope), Reason: value.Reason,
	}
	if value.StepID != "" {
		stepID := string(value.StepID)
		item.StepID = &stepID
	}
	if value.ExpiresAt != nil {
		expiresAt := value.ExpiresAt.UTC().Format(time.RFC3339Nano)
		item.ExpiresAt = &expiresAt
	}
	if value.InvalidatedAt != nil {
		invalidatedAt := value.InvalidatedAt.UTC().Format(time.RFC3339Nano)
		item.InvalidatedAt = &invalidatedAt
	}
	return item
}

type decisionRequest struct {
	ApprovalID   string        `json:"approval_id"`
	PlanID       kernel.PlanID `json:"plan_id"`
	PlanRevision uint64        `json:"plan_revision"`
	PlanHash     string        `json:"plan_hash"`
	StepID       kernel.StepID `json:"step_id"`
	Action       string        `json:"action"`
	DecidedBy    string        `json:"decided_by"`
	Reason       string        `json:"reason"`
	ExpiresAt    *time.Time    `json:"expires_at,omitempty"`
}

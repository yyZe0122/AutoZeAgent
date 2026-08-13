package gateway

import (
	"errors"
	"net/http"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/internal/corequery"
	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
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

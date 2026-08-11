package gateway

import (
	"errors"
	"net/http"
	"strings"

	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
)

func (a *API) handleRuns(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	request, ok := parseListRequest(w, r)
	if !ok {
		return
	}
	items, err := a.queries.ListRuns(r.Context(), corequery.RunListOptions{
		Page: request.Page, Sort: request.Sort, State: strings.TrimSpace(r.URL.Query().Get("state")),
	})
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": items, "page": request.metadata(len(items))})
}

func (a *API) handleRun(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	id, ok := pathID(w, r.URL.Path, "/v1/runs/")
	if !ok {
		return
	}
	item, err := a.queries.GetRun(r.Context(), kernel.RunID(id))
	if errors.Is(err, corequery.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) handleRunUsage(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	basePath := strings.TrimSuffix(r.URL.Path, "/usage")
	id, ok := pathID(w, basePath, "/v1/runs/")
	if !ok {
		return
	}
	usage, err := a.queries.RunUsage(r.Context(), kernel.RunID(id))
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

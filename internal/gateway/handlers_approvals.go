package gateway

import (
	"net/http"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/internal/corequery"
)

func (a *API) handleApprovals(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	request, ok := parseListRequest(w, r)
	if !ok {
		return
	}
	items, err := a.queries.ListApprovals(r.Context(), corequery.ApprovalListOptions{
		Page: request.Page, Sort: request.Sort, Decision: strings.TrimSpace(r.URL.Query().Get("decision")),
	})
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": items, "page": request.metadata(len(items))})
}

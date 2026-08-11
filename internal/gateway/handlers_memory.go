package gateway

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/internal/corequery"
)

func (a *API) handleMemory(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	limit := defaultListLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maximumListLimit {
			writeError(w, http.StatusBadRequest, "invalid_argument", "limit must be between 1 and max page size")
			return
		}
		limit = n
	}
	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_argument", "offset must be >= 0")
			return
		}
		offset = n
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	includeGlobal := true
	if raw := strings.TrimSpace(r.URL.Query().Get("include_global")); raw != "" {
		includeGlobal = raw == "1" || strings.EqualFold(raw, "true")
	}
	items, err := a.queries.ListMemory(r.Context(), corequery.MemoryListOptions{
		Page:          corequery.Page{Limit: limit, Offset: offset},
		SessionID:     sessionID,
		Query:         strings.TrimSpace(r.URL.Query().Get("q")),
		Kind:          strings.TrimSpace(r.URL.Query().Get("kind")),
		IncludeGlobal: includeGlobal,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if items == nil {
		items = []corequery.MemoryEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": items,
		"page":    map[string]any{"limit": limit, "offset": offset},
	})
}

func (a *API) handleMemoryActions(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if a.memoryControl == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "memory control is unavailable")
		return
	}
	var body struct {
		Action    string `json:"action"`
		SessionID string `json:"session_id,omitempty"`
		EntryID   string `json:"entry_id,omitempty"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	action := strings.ToLower(strings.TrimSpace(body.Action))
	switch action {
	case "refresh":
		a.memoryControl.RefreshMemory(strings.TrimSpace(body.SessionID))
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": "refresh"})
	case "forget":
		if strings.TrimSpace(body.EntryID) == "" {
			writeError(w, http.StatusBadRequest, "invalid_argument", "entry_id is required for forget")
			return
		}
		if err := a.memoryControl.ForgetMemory(r.Context(), strings.TrimSpace(body.EntryID)); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": "forget", "entry_id": body.EntryID})
	case "promote":
		if strings.TrimSpace(body.EntryID) == "" {
			writeError(w, http.StatusBadRequest, "invalid_argument", "entry_id is required for promote")
			return
		}
		entry, err := a.memoryControl.PromoteMemory(r.Context(), strings.TrimSpace(body.EntryID))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": "promote", "entry": entry})
	default:
		writeError(w, http.StatusBadRequest, "invalid_argument", "action must be refresh, forget, or promote")
	}
}

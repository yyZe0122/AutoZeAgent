package gateway

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/internal/applicationerror"
	"github.com/yyZe0122/yunmengze-agent/internal/corequery"
	"github.com/yyZe0122/yunmengze-agent/internal/skillcatalog"
)

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if err := a.queries.Check(r.Context()); err != nil {
		writeErrorWithRetryability(w, http.StatusServiceUnavailable, "database_unhealthy", "core database health check failed", true)
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{OK: true, Core: a.core.Status()})
}

func (a *API) handleConfigMCP(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if a.mcp == nil {
		writeJSON(w, http.StatusOK, MCPStatus{})
		return
	}
	writeJSON(w, http.StatusOK, a.mcp.MCPStatus())
}

func (a *API) handleConfigCommands(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	items := []ChatCommandItem{}
	if a.chatCommands != nil {
		if listed := a.chatCommands.ChatCommands(); listed != nil {
			items = listed
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": items})
}

func (a *API) handleConfigModel(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.modelMu.RLock()
		config := a.modelConfig
		loadErr := a.modelConfigError
		switcher := a.modelSwitcher
		a.modelMu.RUnlock()
		models := config.Models
		if models == nil {
			models = []string{}
		}
		// Prefer snapshot Ready/Error (includes ChatBound); fall back to loadErr / defaults.
		errMsg := strings.TrimSpace(loadErr)
		if errMsg == "" {
			errMsg = strings.TrimSpace(config.Error)
		}
		ready := config.Ready && switcher != nil && errMsg == ""
		if !ready && errMsg == "" {
			errMsg = "provider runtime is not configured (check agent.json and ymz config validate)"
		}
		writeJSON(w, http.StatusOK, ModelConfig{
			Model: config.Model, Models: models, ContextWindow: config.ContextWindow,
			Ready: ready, Error: errMsg,
		})
	case http.MethodPut:
		a.modelMu.RLock()
		switcher := a.modelSwitcher
		loadErr := a.modelConfigError
		a.modelMu.RUnlock()
		if switcher == nil {
			msg := loadErr
			if msg == "" {
				msg = "provider runtime is not configured; fix agent.json (ymz config validate) and restart the daemon"
			}
			writeError(w, http.StatusServiceUnavailable, "model_switch_unavailable", msg)
			return
		}
		var request struct {
			Model string `json:"model"`
		}
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		config, err := switcher.SelectModel(r.Context(), request.Model)
		if err != nil {
			if applicationerror.IsCode(err, applicationerror.CodeUnavailable) {
				writeApplicationError(w, err)
				return
			}
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if config.Models == nil {
			config.Models = []string{}
		}
		// Honor switcher Ready/Error (e.g. chat not bound → restart required).
		if !config.Ready && strings.TrimSpace(config.Error) == "" {
			config.Ready = true
			config.Error = ""
		}
		a.modelMu.Lock()
		a.modelConfig = config
		a.modelConfigError = strings.TrimSpace(config.Error)
		a.modelMu.Unlock()
		writeJSON(w, http.StatusOK, config)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPut)
	}
}

type skillMetadataResponse struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Source      skillcatalog.Source `json:"source"`
	Draft       bool                `json:"draft,omitempty"`
	LastUsedAt  string              `json:"last_used_at,omitempty"`
	ArchivedAt  string              `json:"archived_at,omitempty"`
}

func (a *API) handleSkills(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	includeArchived := false
	if raw := strings.TrimSpace(r.URL.Query().Get("include_archived")); raw != "" {
		includeArchived = raw == "1" || strings.EqualFold(raw, "true")
	}
	var available []skillcatalog.Skill
	if a.skills != nil {
		available = a.skills.Skills()
	}
	usage := map[string]SkillUsageView{}
	if a.skillControl != nil {
		if m, err := a.skillControl.SkillUsage(r.Context()); err == nil && m != nil {
			usage = m
		}
	}
	items := make([]skillMetadataResponse, 0, len(available))
	for _, skill := range available {
		u := usage[skill.ID]
		archived := strings.TrimSpace(u.ArchivedAt) != ""
		if includeArchived && !archived {
			continue
		}
		if !includeArchived && archived {
			continue
		}
		items = append(items, skillMetadataResponse{
			ID: skill.ID, Name: skill.Name, Description: skill.Description, Source: skill.Source,
			Draft: skill.HasDraft, LastUsedAt: u.LastUsedAt, ArchivedAt: u.ArchivedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": items})
}

func (a *API) handleSkillEvents(w http.ResponseWriter, r *http.Request) {
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
	items, err := a.queries.ListSkillEvents(r.Context(), corequery.SkillEventListOptions{
		Page:    corequery.Page{Limit: limit},
		SkillID: strings.TrimSpace(r.URL.Query().Get("skill_id")),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if items == nil {
		items = []corequery.SkillEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": items})
}

func (a *API) handleSkillActions(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if a.skillControl == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "skill control is unavailable")
		return
	}
	var body struct {
		Action  string `json:"action"`
		SkillID string `json:"skill_id"`
		Actor   string `json:"actor,omitempty"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	skillID := strings.TrimSpace(body.SkillID)
	if skillID == "" {
		writeError(w, http.StatusBadRequest, "invalid_argument", "skill_id is required")
		return
	}
	actor := strings.TrimSpace(body.Actor)
	if actor == "" {
		actor = "user"
	}
	switch strings.ToLower(strings.TrimSpace(body.Action)) {
	case "apply":
		if err := a.skillControl.ApplySkillDraft(r.Context(), skillID, actor); err != nil {
			writeSkillControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": "apply", "skill_id": skillID})
	case "reject":
		if err := a.skillControl.RejectSkillDraft(r.Context(), skillID, actor); err != nil {
			writeSkillControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": "reject", "skill_id": skillID})
	default:
		writeError(w, http.StatusBadRequest, "invalid_argument", "action must be apply or reject")
	}
}

func writeSkillControlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, skillcatalog.ErrSkillNotFound), errors.Is(err, skillcatalog.ErrNoDraft):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, skillcatalog.ErrSystemSkill):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
	}
}

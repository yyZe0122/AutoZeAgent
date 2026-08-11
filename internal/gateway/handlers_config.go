package gateway

import (
	"net/http"

	"github.com/yyZe0122/yunmengze-agent/internal/applicationerror"
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

func (a *API) handleConfigModel(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.modelMu.RLock()
		config := a.modelConfig
		a.modelMu.RUnlock()
		models := config.Models
		if models == nil {
			models = []string{}
		}
		writeJSON(w, http.StatusOK, ModelConfig{
			Model: config.Model, Models: models, ContextWindow: config.ContextWindow,
		})
	case http.MethodPut:
		if a.modelSwitcher == nil {
			writeError(w, http.StatusServiceUnavailable, "model_switch_unavailable", "model switching is not configured")
			return
		}
		var request struct {
			Model string `json:"model"`
		}
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		config, err := a.modelSwitcher.SelectModel(r.Context(), request.Model)
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
		a.modelMu.Lock()
		a.modelConfig = config
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
}

func (a *API) handleSkills(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	var available []skillcatalog.Skill
	if a.skills != nil {
		available = a.skills.Skills()
	}
	items := make([]skillMetadataResponse, len(available))
	for index, skill := range available {
		items[index] = skillMetadataResponse{
			ID: skill.ID, Name: skill.Name, Description: skill.Description, Source: skill.Source,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": items})
}

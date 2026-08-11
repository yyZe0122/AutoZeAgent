package gateway

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/pkg/schedulerapi"
)

func (a *API) handleJobs(w http.ResponseWriter, r *http.Request) {
	if a.jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "scheduler is unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		includeArchived := r.URL.Query().Get("include_archived") == "true"
		jobs, err := a.jobs.List(r.Context(), includeArchived)
		if err != nil {
			writeInternal(w, err)
			return
		}
		if jobs == nil {
			jobs = []schedulerapi.Job{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
	case http.MethodPost:
		var request schedulerapi.CreateRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		job, err := a.jobs.Create(r.Context(), request)
		if err != nil {
			msg := err.Error()
			switch {
			case strings.Contains(msg, "required"), strings.Contains(msg, "invalid"),
				strings.Contains(msg, "must be"), strings.Contains(msg, "RFC3339"):
				writeError(w, http.StatusBadRequest, "invalid_request", msg)
			case strings.Contains(msg, "session not found"):
				writeError(w, http.StatusNotFound, "not_found", msg)
			default:
				writeInternal(w, err)
			}
			return
		}
		writeJSON(w, http.StatusCreated, job)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (a *API) handleJob(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if a.jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "scheduler is unavailable")
		return
	}
	id, ok := pathID(w, r.URL.Path, "/v1/jobs/")
	if !ok {
		return
	}
	job, err := a.jobs.Get(r.Context(), id)
	if errors.Is(err, schedulerapi.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (a *API) handleJobAction(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if a.jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "scheduler is unavailable")
		return
	}
	basePath := strings.TrimSuffix(r.URL.Path, "/actions")
	id, ok := pathID(w, basePath, "/v1/jobs/")
	if !ok {
		return
	}
	var request jobActionRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	request.Reviewer = strings.TrimSpace(request.Reviewer)
	if request.Reviewer == "" {
		request.Reviewer = "local-user"
	}
	status := ""
	switch request.Action {
	case "pause":
		status = schedulerapi.StatusPaused
	case "resume":
		status = schedulerapi.StatusActive
	case "cancel":
		status = schedulerapi.StatusArchived
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "action must be pause, resume, or cancel")
		return
	}
	job, err := a.jobs.ChangeState(r.Context(), schedulerapi.StateRequest{
		JobID: id, Reviewer: request.Reviewer, Reason: strings.TrimSpace(request.Reason),
	}, status)
	if errors.Is(err, schedulerapi.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, "conflict", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func taskViewFromDomain(task kernel.Task) corequery.Task {
	if task.ID == "" {
		return corequery.Task{}
	}
	sessionID := task.SessionID
	mode := string(task.ExecutionMode)
	if mode == "" {
		mode = string(kernel.ExecutionModeAgent)
	}
	return corequery.Task{
		ID: task.ID, SessionID: &sessionID, Title: task.Title, Objective: task.Objective,
		State: string(task.State), ExecutionMode: mode, Version: task.Version,
		CreatedAt: task.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: task.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

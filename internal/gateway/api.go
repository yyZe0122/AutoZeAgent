package gateway

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"autozeagent.local/autozeagent/internal/app"
	"autozeagent.local/autozeagent/internal/applicationerror"
	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/events"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/modelstream"
	"autozeagent.local/autozeagent/internal/skillcatalog"
	"autozeagent.local/autozeagent/internal/taskcontrol"
	"autozeagent.local/autozeagent/internal/tasksubmission"
	"autozeagent.local/autozeagent/pkg/schedulerapi"
)

const (
	defaultListLimit = 100
	maximumListLimit = corequery.MaxPageSize
)

type QueryService interface {
	Check(context.Context) error
	ListSessions(context.Context, corequery.SessionListOptions) ([]corequery.Session, error)
	GetSession(context.Context, kernel.SessionID) (corequery.Session, error)
	SessionTranscript(context.Context, kernel.SessionID, corequery.TranscriptOptions) ([]corequery.TranscriptMessage, error)
	TaskTranscript(context.Context, kernel.TaskID, corequery.TranscriptOptions) ([]corequery.TranscriptMessage, error)
	ListTasks(context.Context, corequery.TaskListOptions) ([]corequery.Task, error)
	GetTask(context.Context, kernel.TaskID) (corequery.Task, error)
	TaskUsage(context.Context, kernel.TaskID) (corequery.TaskUsage, error)
	TaskContext(context.Context, kernel.TaskID) (corequery.TaskContext, error)
	SessionContext(context.Context, kernel.SessionID) (corequery.TaskContext, error)
	ListPlans(context.Context, corequery.PlanListOptions) ([]corequery.Plan, error)
	GetPlan(context.Context, kernel.PlanID) (corequery.Plan, error)
	ListApprovals(context.Context, corequery.ApprovalListOptions) ([]corequery.Approval, error)
	ListRuns(context.Context, corequery.RunListOptions) ([]corequery.Run, error)
	GetRun(context.Context, kernel.RunID) (corequery.Run, error)
}

type TaskSubmitter interface {
	Submit(context.Context, tasksubmission.Request) (tasksubmission.Result, error)
}

// ApprovalDecider and RunStarter are optional legacy interfaces; interactive
// plan approval and plan-step Start are removed (HTTP 410).
type ApprovalDecider interface{}
type RunStarter interface{}

type TaskController interface {
	ControlTask(context.Context, taskcontrol.TaskActionRequest) (kernel.Task, error)
}

type JobService interface {
	Create(context.Context, schedulerapi.CreateRequest) (schedulerapi.Job, error)
	Get(context.Context, string) (schedulerapi.Job, error)
	List(context.Context, bool) ([]schedulerapi.Job, error)
	ChangeState(context.Context, schedulerapi.StateRequest, string) (schedulerapi.Job, error)
}

// ModelConfig is the model snapshot exposed by GET/PUT /v1/config/model.
// It must never include API keys or other secrets.
type ModelConfig struct {
	Model  string   `json:"model"`
	Models []string `json:"models"`
	// ContextWindow is the selected model's context length in tokens; 0 = unknown.
	ContextWindow int64 `json:"context_window,omitempty"`
}

// ModelSwitcher applies a provider/model selection at runtime.
type ModelSwitcher interface {
	SelectModel(ctx context.Context, ref string) (ModelConfig, error)
}

// MCPStatus is a secret-free MCP snapshot for GET /v1/config/mcp.
type MCPStatus struct {
	Enabled bool `json:"enabled"`
	Total   int  `json:"total"`
	OK      int  `json:"ok"`
	Error   int  `json:"error"`
	Tools   int  `json:"tools"`
}

// MCPStatusProvider supplies live MCP status (optional).
type MCPStatusProvider interface {
	MCPStatus() MCPStatus
}

type APIConfig struct {
	Queries           QueryService
	TaskSubmissions   TaskSubmitter
	ApprovalDecisions ApprovalDecider
	RunStarts         RunStarter
	TaskControls      TaskController
	Jobs              JobService
	Core              interface{ Status() app.Status }
	Events            *events.Store
	Skills            *skillcatalog.Catalog
	ModelConfig       ModelConfig
	ModelSwitcher     ModelSwitcher
	// ModelStream is optional; when set, exposes GET /v1/model-stream.
	ModelStream *modelstream.Hub
	// MCP is optional; when set, exposes GET /v1/config/mcp.
	MCP MCPStatusProvider
}

type API struct {
	queries           QueryService
	taskSubmissions   TaskSubmitter
	approvalDecisions ApprovalDecider
	runStarts         RunStarter
	taskControls      TaskController
	jobs              JobService
	core              interface{ Status() app.Status }
	events            *events.Store
	skills            *skillcatalog.Catalog
	modelMu           sync.RWMutex
	modelConfig       ModelConfig
	modelSwitcher     ModelSwitcher
	modelStream       *modelstream.Hub
	mcp               MCPStatusProvider
}

func NewAPI(config APIConfig) (*API, error) {
	if config.Queries == nil || config.TaskSubmissions == nil || config.Core == nil || config.Events == nil {
		return nil, errors.New("gateway queries, task submission service, core, and event store are required")
	}
	if config.ModelConfig.Models == nil {
		config.ModelConfig.Models = []string{}
	}
	return &API{
		queries: config.Queries, taskSubmissions: config.TaskSubmissions,
		approvalDecisions: config.ApprovalDecisions, runStarts: config.RunStarts, taskControls: config.TaskControls, jobs: config.Jobs, core: config.Core, events: config.Events, skills: config.Skills,
		modelConfig: config.ModelConfig, modelSwitcher: config.ModelSwitcher, modelStream: config.ModelStream,
		mcp: config.MCP,
	}, nil
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case r.URL.Path == "/v1/health":
		a.handleHealth(w, r)
	case r.URL.Path == "/v1/config/model":
		a.handleConfigModel(w, r)
	case r.URL.Path == "/v1/config/mcp":
		a.handleConfigMCP(w, r)
	case r.URL.Path == "/v1/skills":
		a.handleSkills(w, r)
	case r.URL.Path == "/v1/sessions":
		a.handleSessions(w, r)
	case strings.HasSuffix(r.URL.Path, "/messages") && strings.HasPrefix(r.URL.Path, "/v1/sessions/"):
		a.handleSessionMessages(w, r)
	case strings.HasSuffix(r.URL.Path, "/context") && strings.HasPrefix(r.URL.Path, "/v1/sessions/"):
		a.handleSessionContext(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/sessions/"):
		a.handleSession(w, r)
	case r.URL.Path == "/v1/tasks":
		a.handleTasks(w, r)
	case strings.HasSuffix(r.URL.Path, "/actions") && strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
		a.handleTaskAction(w, r)
	case strings.HasSuffix(r.URL.Path, "/usage") && strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
		a.handleTaskUsage(w, r)
	case strings.HasSuffix(r.URL.Path, "/context") && strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
		a.handleTaskContext(w, r)
	case strings.HasSuffix(r.URL.Path, "/messages") && strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
		a.handleTaskMessages(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
		a.handleTask(w, r)
	case r.URL.Path == "/v1/jobs":
		a.handleJobs(w, r)
	case strings.HasSuffix(r.URL.Path, "/actions") && strings.HasPrefix(r.URL.Path, "/v1/jobs/"):
		a.handleJobAction(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/jobs/"):
		a.handleJob(w, r)
	case r.URL.Path == "/v1/plans":
		a.handlePlans(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/plans/"):
		a.handlePlan(w, r)
	case r.URL.Path == "/v1/approvals/prompt":
		a.handleApprovalPrompt(w, r)
	case r.URL.Path == "/v1/approvals":
		a.handleApprovals(w, r)
	case r.URL.Path == "/v1/runs":
		a.handleRuns(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/runs/"):
		a.handleRun(w, r)
	case r.URL.Path == "/v1/events":
		a.handleEvents(w, r)
	case r.URL.Path == "/v1/events/stream":
		a.handleEventStream(w, r)
	case r.URL.Path == "/v1/model-stream":
		a.handleModelStream(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
	}
}

type healthResponse struct {
	OK   bool       `json:"ok"`
	Core app.Status `json:"core"`
}

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

type taskSubmissionRequest struct {
	TaskID        kernel.TaskID        `json:"task_id"`
	SessionID     kernel.SessionID     `json:"session_id"`
	PlanID        kernel.PlanID        `json:"plan_id"`
	Title         string               `json:"title"`
	Objective     string               `json:"objective"`
	SkillIDs      []string             `json:"skill_ids,omitempty"`
	ExecutionMode kernel.ExecutionMode `json:"execution_mode,omitempty"`
}

type taskSubmissionResponse struct {
	Task            corequery.Task         `json:"task"`
	Plan            *approval.PlanDocument `json:"plan,omitempty"`
	PlanID          kernel.PlanID          `json:"plan_id,omitempty"`
	RunID           kernel.RunID           `json:"run_id,omitempty"`
	PlanningPending bool                   `json:"planning_pending,omitempty"`
}

func (a *API) handleSessions(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	request, ok := parseListRequest(w, r)
	if !ok {
		return
	}
	items, err := a.queries.ListSessions(r.Context(), corequery.SessionListOptions{
		Page: request.Page, Sort: request.Sort,
	})
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": items, "page": request.metadata(len(items))})
}

func (a *API) handleSession(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	id, ok := pathID(w, r.URL.Path, "/v1/sessions/")
	if !ok {
		return
	}
	item, err := a.queries.GetSession(r.Context(), kernel.SessionID(id))
	if errors.Is(err, corequery.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) handleSessionMessages(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	// path: /v1/sessions/{id}/messages
	trimmed := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
	id := strings.TrimSuffix(trimmed, "/messages")
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "invalid_request", "session id required")
		return
	}
	request, ok := parseListRequest(w, r)
	if !ok {
		return
	}
	items, err := a.queries.SessionTranscript(r.Context(), kernel.SessionID(id), corequery.TranscriptOptions{
		Page: request.Page,
	})
	if errors.Is(err, corequery.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": items, "page": request.metadata(len(items))})
}

func (a *API) handleTaskMessages(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
	id := strings.TrimSuffix(trimmed, "/messages")
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "invalid_request", "task id required")
		return
	}
	request, ok := parseListRequest(w, r)
	if !ok {
		return
	}
	items, err := a.queries.TaskTranscript(r.Context(), kernel.TaskID(id), corequery.TranscriptOptions{
		Page: request.Page,
	})
	if errors.Is(err, corequery.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": items, "page": request.metadata(len(items))})
}

func (a *API) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.listTasks(w, r)
	case http.MethodPost:
		a.submitTask(w, r)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (a *API) listTasks(w http.ResponseWriter, r *http.Request) {
	request, ok := parseListRequest(w, r)
	if !ok {
		return
	}
	items, err := a.queries.ListTasks(r.Context(), corequery.TaskListOptions{
		Page: request.Page, Sort: request.Sort, State: strings.TrimSpace(r.URL.Query().Get("state")),
	})
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": items, "page": request.metadata(len(items))})
}

func (a *API) submitTask(w http.ResponseWriter, r *http.Request) {
	var request taskSubmissionRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid task submission request")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "task submission request must contain one JSON value")
		return
	}
	allowExisting := strings.TrimSpace(string(request.TaskID)) != ""
	result, err := a.taskSubmissions.Submit(r.Context(), tasksubmission.Request{
		TaskID: request.TaskID, SessionID: request.SessionID, PlanID: request.PlanID,
		Title: request.Title, Objective: request.Objective, SkillIDs: request.SkillIDs,
		ExecutionMode: request.ExecutionMode, EnsureSession: true, AllowExisting: allowExisting,
	})
	response := taskSubmissionResponse{Task: taskViewFromDomain(result.Task), Plan: result.Plan, RunID: result.RunID, PlanID: result.PlanID}
	if applicationerror.IsCode(err, applicationerror.CodePlanningPending) {
		response.PlanningPending = true
		writeJSON(w, http.StatusAccepted, response)
		return
	}
	if err != nil {
		if !writeApplicationError(w, err) {
			writeInternal(w, err)
		}
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (a *API) handleTask(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	id, ok := pathID(w, r.URL.Path, "/v1/tasks/")
	if !ok {
		return
	}
	item, err := a.queries.GetTask(r.Context(), kernel.TaskID(id))
	if errors.Is(err, corequery.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) handleTaskUsage(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	basePath := strings.TrimSuffix(r.URL.Path, "/usage")
	id, ok := pathID(w, basePath, "/v1/tasks/")
	if !ok {
		return
	}
	usage, err := a.queries.TaskUsage(r.Context(), kernel.TaskID(id))
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

func (a *API) handleTaskContext(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	basePath := strings.TrimSuffix(r.URL.Path, "/context")
	id, ok := pathID(w, basePath, "/v1/tasks/")
	if !ok {
		return
	}
	item, err := a.queries.TaskContext(r.Context(), kernel.TaskID(id))
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) handleSessionContext(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	basePath := strings.TrimSuffix(r.URL.Path, "/context")
	id, ok := pathID(w, basePath, "/v1/sessions/")
	if !ok {
		return
	}
	item, err := a.queries.SessionContext(r.Context(), kernel.SessionID(id))
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) handleTaskAction(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if a.taskControls == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "task controls are unavailable until a Provider is configured")
		return
	}
	basePath := strings.TrimSuffix(r.URL.Path, "/actions")
	id, ok := pathID(w, basePath, "/v1/tasks/")
	if !ok {
		return
	}
	var request taskcontrol.TaskActionRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	request.TaskID = kernel.TaskID(id)
	task, err := a.taskControls.ControlTask(r.Context(), request)
	if err != nil {
		if !writeApplicationError(w, err) {
			writeInternal(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, taskViewFromDomain(task))
}

type jobActionRequest struct {
	Action   string `json:"action"`
	Reviewer string `json:"reviewer,omitempty"`
	Reason   string `json:"reason"`
}

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
	if errors.Is(err, sql.ErrNoRows) {
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
	if errors.Is(err, sql.ErrNoRows) {
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

func (a *API) handleApprovalPrompt(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	// Interactive plan approval removed: plan mode is read-only chat (OpenCode-style).
	writeError(w, http.StatusGone, "gone",
		"interactive plan approval was removed; use Tab plan for read-only chat or autozeagent run --execution-mode plan")
}

func (a *API) handleApprovals(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.listApprovals(w, r)
	case http.MethodPost:
		a.decideApproval(w, r)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (a *API) listApprovals(w http.ResponseWriter, r *http.Request) {
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

func (a *API) decideApproval(w http.ResponseWriter, r *http.Request) {
	// Interactive plan approval removed: plan mode is read-only chat (OpenCode-style).
	writeError(w, http.StatusGone, "gone",
		"interactive plan approval was removed; use Tab plan for read-only chat or autozeagent run --execution-mode plan")
}

type runStartRequest struct {
	TaskID       kernel.TaskID `json:"task_id"`
	PlanID       kernel.PlanID `json:"plan_id"`
	PlanRevision uint64        `json:"plan_revision"`
	PlanHash     string        `json:"plan_hash"`
}

func (a *API) handleRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.listRuns(w, r)
	case http.MethodPost:
		a.startRuns(w, r)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (a *API) listRuns(w http.ResponseWriter, r *http.Request) {
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

func (a *API) startRuns(w http.ResponseWriter, r *http.Request) {
	// Plan-step Start removed with Planner; chat starts runs via task submission.
	writeError(w, http.StatusGone, "gone", "plan-step run start was removed; submit a chat task instead")
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

func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	after, ok := parseAfter(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	items, err := a.events.ListAfter(r.Context(), after, limit)
	if err != nil {
		writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": items})
}

func (a *API) handleEventStream(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusNotImplemented, "stream_unavailable", "streaming is unavailable")
		return
	}
	after, valid := parseAfter(w, r)
	if !valid {
		return
	}
	if value := strings.TrimSpace(r.Header.Get("Last-Event-ID")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_after", "Last-Event-ID must be an unsigned integer")
			return
		}
		if parsed > after {
			after = parsed
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		items, err := a.events.ListAfter(r.Context(), after, 100)
		if err != nil {
			return
		}
		for _, event := range items {
			payload, err := json.Marshal(event)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Type, payload); err != nil {
				return
			}
			after = event.Sequence
		}
		if len(items) > 0 {
			flusher.Flush()
			continue
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

// handleModelStream fans out live provider StreamEvents (typewriter).
// Query: session_id (optional filter), run_id (optional filter).
func (a *API) handleModelStream(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if a.modelStream == nil {
		writeError(w, http.StatusServiceUnavailable, "stream_unavailable", "model stream is not configured")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusNotImplemented, "stream_unavailable", "streaming is unavailable")
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	runID := strings.TrimSpace(r.URL.Query().Get("run_id"))
	ch, cancel := a.modelStream.Subscribe(sessionID, runID, 128)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	// Initial comment so clients know the stream is open.
	if _, err := fmt.Fprintf(w, ": model-stream ready\n\n"); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case env, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(env)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: model\ndata: %s\n\n", env.Seq, payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

type listRequest struct {
	Page corequery.Page
	Sort corequery.SortDirection
}

type pageMetadata struct {
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
	Returned int    `json:"returned"`
	Order    string `json:"order"`
}

func (request listRequest) metadata(returned int) pageMetadata {
	return pageMetadata{
		Limit: request.Page.Limit, Offset: request.Page.Offset, Returned: returned, Order: string(request.Sort),
	}
}

func parseListRequest(w http.ResponseWriter, r *http.Request) (listRequest, bool) {
	limit, ok := parseLimit(w, r)
	if !ok {
		return listRequest{}, false
	}
	offset := 0
	if value := strings.TrimSpace(r.URL.Query().Get("offset")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "invalid_offset", "offset must be a non-negative integer")
			return listRequest{}, false
		}
		offset = parsed
	}
	order := corequery.SortDescending
	if value := strings.TrimSpace(r.URL.Query().Get("order")); value != "" {
		order = corequery.SortDirection(strings.ToLower(value))
		if order != corequery.SortAscending && order != corequery.SortDescending {
			writeError(w, http.StatusBadRequest, "invalid_order", "order must be asc or desc")
			return listRequest{}, false
		}
	}
	return listRequest{Page: corequery.Page{Limit: limit, Offset: offset}, Sort: order}, true
}

func parseLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		return defaultListLimit, true
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 || limit > maximumListLimit {
		writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 500")
		return 0, false
	}
	return limit, true
}

func parseAfter(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("after"))
	if value == "" {
		return 0, true
	}
	after, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_after", "after must be an unsigned integer")
		return 0, false
	}
	return after, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid JSON request")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON value")
	}
	return nil
}

func pathID(w http.ResponseWriter, path, prefix string) (string, bool) {
	id := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return "", false
	}
	return id, true
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	methodNotAllowed(w, method)
	return false
}
func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func writeApplicationError(w http.ResponseWriter, err error) bool {
	code, ok := applicationerror.CodeOf(err)
	if !ok {
		return false
	}
	status := 0
	message := ""
	switch code {
	case applicationerror.CodeInvalidRequest:
		status, message = http.StatusBadRequest, "invalid request"
	case applicationerror.CodeNotFound:
		status, message = http.StatusNotFound, "resource not found"
	case applicationerror.CodeConflict:
		status, message = http.StatusConflict, "request conflicts with current state"
	case applicationerror.CodePlanChanged:
		status, message = http.StatusConflict, "stored plan revision or hash differs"
	case applicationerror.CodePlanDocumentUnavailable:
		status, message = http.StatusConflict, "stored plan document is unavailable or no longer matches the plan"
	case applicationerror.CodeUnavailable:
		status, message = http.StatusServiceUnavailable, "service temporarily unavailable"
	default:
		return false
	}
	writeErrorWithRetryability(w, status, string(code), message, applicationerror.IsRetryable(err))
	return true
}

func writeInternal(w http.ResponseWriter, err error) {
	if err != nil {
		slog.Error("gateway internal error", "component", "gateway", "operation", "http", "result", "failed", "error", err)
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeErrorWithRetryability(w, status, code, message, false)
}
func writeErrorWithRetryability(w http.ResponseWriter, status int, code, message string, retryable bool) {
	writeJSON(w, status, errorResponse{Error: errorDetail{Code: code, Message: message, Retryable: retryable}})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func randomID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value), nil
}

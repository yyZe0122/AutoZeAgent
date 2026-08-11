// Package agent runs the provider tool loop through the mandatory Tool Broker.
// Chat dual-track orchestration lives in chatsession (ADR-038); child Runs via
// the task tool are ADR-039 (implementation backlog).
package agent

import (
	"context"
	"errors"
	"strings"
	"sync"

	"autozeagent.local/autozeagent/internal/contextpack"
	"autozeagent.local/autozeagent/pkg/providerapi"
	"autozeagent.local/autozeagent/pkg/toolapi"
)

var (
	ErrInvalidRequest      = errors.New("invalid agent request")
	ErrInvalidToolCall     = errors.New("invalid provider tool call")
	ErrUnadvertisedTool    = errors.New("provider requested an unadvertised tool")
	ErrMaxIterations       = errors.New("agent maximum iterations reached")
	ErrEmptyResponse       = errors.New("provider returned no final response")
	ErrTokenBudgetExceeded = errors.New("agent token budget exceeded")
	ErrCostBudgetExceeded  = errors.New("agent cost budget exceeded")
)

type StreamingProvider interface {
	Stream(context.Context, providerapi.CompletionRequest, providerapi.StreamHandler) error
}

type ToolBroker interface {
	Definitions() []toolapi.Definition
	Execute(context.Context, toolapi.Request) (toolapi.Response, error)
}

// StreamObserver receives provider stream events for UI fan-out (optional).
type StreamObserver interface {
	Publish(sessionID, taskID, runID string, event providerapi.StreamEvent)
}

// RoleEndpoint is a provider+model pair for a model role (ADR-045).
type RoleEndpoint struct {
	Provider      StreamingProvider
	Model         string
	ContextWindow int64
}

type Config struct {
	Provider      StreamingProvider
	Broker        ToolBroker
	Records       *RecordStore
	Model         string
	MaxIterations int
	// MaxToolResultRunes caps tool/assistant Content length on provider
	// requests only. Zero uses DefaultMaxToolResultRunes. Records stay full.
	MaxToolResultRunes int
	// ContextWindow is the model context length in tokens; 0 = unknown (pack still L1-trims).
	ContextWindow int64
	// Roles maps optional role names (subagent, compact) to endpoints.
	// Unset roles fall back to main Provider/Model/ContextWindow.
	Roles map[string]RoleEndpoint
	// Stream is optional; when set, CollectStream events are teed for local UI.
	Stream StreamObserver
	// Context is optional; when set, each successful provider iteration updates pressure snapshot.
	Context *contextpack.Store
	// Calibrator is optional; when set, post-flight usage calibrates estimates.
	Calibrator *contextpack.Calibrator
}

type Runner struct {
	mu                 sync.RWMutex
	provider           StreamingProvider
	broker             ToolBroker
	records            *RecordStore
	model              string
	maxIterations      int
	maxToolResultRunes int
	contextWindow      int64
	roles              map[string]RoleEndpoint
	stream             StreamObserver
	contextStore       *contextpack.Store
	calibrator         *contextpack.Calibrator
}

type RunRequest struct {
	RunID              string
	TaskID             string
	SessionID          string
	PlanID             string
	PlanHash           string
	StepID             string
	CapabilityGrantID  string
	CapabilityGrantIDs map[string][]string
	Actor              string
	TraceID            string
	// Messages are persisted as this run's input_message prefix (Prepare).
	Messages []providerapi.Message
	// History is prior session turns for the provider only — not written to records.
	History           []providerapi.Message
	AllowedTools      []string
	MaxOutputTokens   int64
	MaxTotalTokens    int64
	MaxCostMicros     int64
	Temperature       *float64
	ToolTimeoutMillis int64
	// ContextWindow overrides runner default when > 0.
	ContextWindow int64
	// Depth is 0 for top-level chat runs; child task tools increment (ADR-039).
	Depth int
	// Role selects model endpoint (ADR-045): empty/main → main; subagent/compact when configured.
	Role string
}

type Result struct {
	Content    string
	ToolCalls  []toolapi.Response
	Usage      providerapi.Usage
	Iterations int
}

func New(config Config) (*Runner, error) {
	if config.Provider == nil || config.Broker == nil || config.Records == nil {
		return nil, errors.New("agent provider, tool broker, and record store are required")
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		return nil, errors.New("agent model is required")
	}
	maxIterations := config.MaxIterations
	if maxIterations == 0 {
		maxIterations = 16
	}
	if maxIterations < 1 || maxIterations > 64 {
		return nil, errors.New("agent maximum iterations must be between 1 and 64")
	}
	maxToolResultRunes := config.MaxToolResultRunes
	if maxToolResultRunes <= 0 {
		maxToolResultRunes = DefaultMaxToolResultRunes
	}
	cal := config.Calibrator
	if cal == nil {
		cal = contextpack.NewCalibrator()
	}
	roles := config.Roles
	if len(roles) > 0 {
		roles = make(map[string]RoleEndpoint, len(config.Roles))
		for k, v := range config.Roles {
			roles[k] = v
		}
	}
	return &Runner{
		provider: config.Provider, broker: config.Broker, records: config.Records,
		model: model, maxIterations: maxIterations, maxToolResultRunes: maxToolResultRunes,
		contextWindow: config.ContextWindow, roles: roles, stream: config.Stream,
		contextStore: config.Context, calibrator: cal,
	}, nil
}

// SetContextWindow updates the model context length used for packing.
func (r *Runner) SetContextWindow(n int64) {
	if n < 0 {
		n = 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contextWindow = n
}

// ContextWindow returns the configured model context length (0 = unknown).
func (r *Runner) ContextWindow() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.contextWindow
}

// SetProvider replaces the main streaming provider used by subsequent runs.
// Role endpoints (subagent/compact) are unchanged; reload config + restart to update them.
func (r *Runner) SetProvider(provider StreamingProvider) error {
	if provider == nil {
		return errors.New("agent provider is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.provider = provider
	return nil
}

// SetModel replaces the main model id used by subsequent provider requests.
func (r *Runner) SetModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("agent model is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.model = model
	return nil
}

// Model returns the currently selected main model id.
func (r *Runner) Model() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.model
}

// SetRoleEndpoint sets or replaces a non-main role endpoint (ADR-045). Empty role or main is rejected.
func (r *Runner) SetRoleEndpoint(role string, ep RoleEndpoint) error {
	role = strings.TrimSpace(role)
	if role == "" || role == "main" {
		return errors.New("role endpoint must be a non-main role name")
	}
	if ep.Provider == nil || strings.TrimSpace(ep.Model) == "" {
		return errors.New("role endpoint provider and model are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.roles == nil {
		r.roles = make(map[string]RoleEndpoint)
	}
	r.roles[role] = ep
	return nil
}

func (r *Runner) snapshot() (StreamingProvider, string) {
	p, m, _ := r.snapshotForRole("")
	return p, m
}

// snapshotForRole returns provider, model id, and context window for the role.
// Empty role, "main", or unconfigured role falls back to main.
func (r *Runner) snapshotForRole(role string) (StreamingProvider, string, int64) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	role = strings.TrimSpace(role)
	if role != "" && role != "main" {
		if ep, ok := r.roles[role]; ok && ep.Provider != nil && strings.TrimSpace(ep.Model) != "" {
			cw := ep.ContextWindow
			if cw < 0 {
				cw = 0
			}
			return ep.Provider, ep.Model, cw
		}
	}
	return r.provider, r.model, r.contextWindow
}

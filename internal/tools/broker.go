// Package tools contains the mandatory Tool Broker and built-in typed tools.
// Executors are reachable only through registered Tool handlers.
package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/artifacts"
	"autozeagent.local/autozeagent/internal/audit"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/policy"
	"autozeagent.local/autozeagent/pkg/toolapi"
)

var (
	ErrUnknownTool        = errors.New("unknown tool")
	ErrDuplicateTool      = errors.New("duplicate tool")
	ErrInvalidTool        = errors.New("invalid tool")
	ErrToolDenied         = toolapi.ErrDenied
	ErrToolTimeout        = errors.New("tool call timed out")
	ErrToolOutputTooLarge = errors.New("tool output too large")
)

type Authorization struct {
	Capability    string
	Path          string
	Command       string
	Arguments     []string
	NetworkDomain string
}

type Tool interface {
	Definition() toolapi.Definition
	Authorization(json.RawMessage) (Authorization, error)
	Execute(context.Context, json.RawMessage) (json.RawMessage, error)
}

type Config struct {
	DB                *sql.DB
	Approvals         *approval.Repository
	Policy            *policy.Evaluator
	Artifacts         *artifacts.Store
	ArtifactThreshold int
	MaximumTimeout    time.Duration
	Now               func() time.Time
}

type Broker struct {
	db                *sql.DB
	approvals         *approval.Repository
	policy            *policy.Evaluator
	audit             *audit.Store
	artifacts         *artifacts.Store
	artifactThreshold int
	maximumTimeout    time.Duration
	now               func() time.Time
	mu                sync.RWMutex
	registry          map[string]Tool
}

func NewBroker(config Config) (*Broker, error) {
	if config.DB == nil || config.Approvals == nil || config.Policy == nil || config.Artifacts == nil {
		return nil, errors.New("tool broker database, approval, policy, and artifact stores are required")
	}
	auditStore, err := audit.NewStore(config.DB)
	if err != nil {
		return nil, err
	}
	if config.ArtifactThreshold <= 0 {
		config.ArtifactThreshold = 64 * 1024
	}
	if config.MaximumTimeout <= 0 {
		config.MaximumTimeout = 10 * time.Minute
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Broker{
		db: config.DB, approvals: config.Approvals, policy: config.Policy,
		audit: auditStore, artifacts: config.Artifacts,
		artifactThreshold: config.ArtifactThreshold, maximumTimeout: config.MaximumTimeout,
		now: config.Now, registry: make(map[string]Tool),
	}, nil
}

func (b *Broker) Register(tool Tool) error {
	if tool == nil {
		return fmt.Errorf("%w: nil handler", ErrInvalidTool)
	}
	definition := tool.Definition()
	definition.Name = strings.TrimSpace(definition.Name)
	if definition.Name == "" || strings.TrimSpace(definition.Description) == "" {
		return fmt.Errorf("%w: name and description are required", ErrInvalidTool)
	}
	if !toolapi.ValidName(definition.Name) {
		return fmt.Errorf("%w: name %q must match ^[a-zA-Z0-9_-]+$", ErrInvalidTool, definition.Name)
	}
	if definition.DefaultTimeoutMillis <= 0 {
		return fmt.Errorf("%w: default timeout must be positive", ErrInvalidTool)
	}
	if !policy.RiskLevel(definition.Risk).Valid() {
		return fmt.Errorf("%w: invalid risk %q", ErrInvalidTool, definition.Risk)
	}
	if err := validateInputSchema(definition.InputSchema); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTool, err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.registry[definition.Name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateTool, definition.Name)
	}
	b.registry[definition.Name] = tool
	return nil
}

func validateInputSchema(raw json.RawMessage) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return errors.New("input schema must be valid JSON")
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil || schema == nil {
		return errors.New("input schema must be a JSON object")
	}
	kind, ok := schema["type"].(string)
	if !ok || kind != "object" {
		return errors.New("input schema type must be object")
	}
	if properties, exists := schema["properties"]; exists {
		if _, ok := properties.(map[string]any); !ok {
			return errors.New("input schema properties must be an object")
		}
	}
	if required, exists := schema["required"]; exists {
		items, ok := required.([]any)
		if !ok {
			return errors.New("input schema required must be an array")
		}
		for _, item := range items {
			if _, ok := item.(string); !ok {
				return errors.New("input schema required entries must be strings")
			}
		}
	}
	return nil
}
func (b *Broker) Definitions() []toolapi.Definition {
	b.mu.RLock()
	definitions := make([]toolapi.Definition, 0, len(b.registry))
	for _, tool := range b.registry {
		definitions = append(definitions, tool.Definition())
	}
	b.mu.RUnlock()
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions
}

func (b *Broker) Execute(ctx context.Context, request toolapi.Request) (toolapi.Response, error) {
	if ctx == nil {
		return toolapi.Response{}, errors.New("tool context is required")
	}
	startedAt := b.now().UTC()
	actor := strings.TrimSpace(request.Actor)
	if actor == "" {
		actor = "core"
	}
	if err := validateRequest(request); err != nil {
		b.recordDenied(request, actor, err)
		return toolapi.Response{}, err
	}
	b.mu.RLock()
	tool, exists := b.registry[request.Tool]
	b.mu.RUnlock()
	if !exists {
		err := fmt.Errorf("%w: %s", ErrUnknownTool, request.Tool)
		b.recordDenied(request, actor, err)
		return toolapi.Response{}, err
	}
	definition := tool.Definition()
	timeout, err := b.resolveTimeout(request.TimeoutMillis, definition.DefaultTimeoutMillis)
	if err != nil {
		b.recordDenied(request, actor, err)
		return toolapi.Response{}, err
	}
	authorization, err := tool.Authorization(request.Arguments)
	if err != nil {
		err = fmt.Errorf("%w: invalid %s arguments: %v", ErrToolDenied, request.Tool, err)
		b.recordDenied(request, actor, err)
		return toolapi.Response{}, err
	}
	if strings.TrimSpace(authorization.Capability) == "" {
		authorization.Capability = request.Tool
	}
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return toolapi.Response{}, fmt.Errorf("begin tool call: %w", err)
	}
	defer tx.Rollback()
	grantID, err := b.authorize(ctx, tx, request, definition, authorization, timeout)
	if err != nil {
		_ = tx.Rollback()
		b.recordDenied(request, actor, err)
		return toolapi.Response{}, err
	}
	request.CapabilityGrantID = grantID
	if err := b.audit.RecordTx(ctx, tx, audit.Entry{
		OccurredAt: startedAt, Actor: actor, Action: "tool.execute", ResourceType: "tool_call",
		ResourceID: request.CallID, Outcome: "started", TraceID: request.TraceID,
		Details: map[string]any{"tool": request.Tool, "run_id": request.RunID, "step_id": request.StepID},
	}); err != nil {
		return toolapi.Response{}, fmt.Errorf("write pre-execution audit: %w", err)
	}
	if err := b.insertToolCall(ctx, tx, request, startedAt); err != nil {
		return toolapi.Response{}, err
	}
	if err := tx.Commit(); err != nil {
		return toolapi.Response{}, fmt.Errorf("commit tool call start: %w", err)
	}
	slog.Info("tool execution started", "component", "tool_broker", "operation", "execute", "result", "started", "tool", request.Tool, "tool_call_id", request.CallID, "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "timeout_ms", timeout.Milliseconds())

	executionContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, executeErr := tool.Execute(executionContext, request.Arguments)
	finishedAt := b.now().UTC()
	response := toolapi.Response{CallID: request.CallID, Tool: request.Tool, StartedAt: startedAt, FinishedAt: finishedAt}
	if len(output) > 0 {
		if !json.Valid(output) {
			executeErr = errors.Join(executeErr, errors.New("tool returned invalid JSON output"))
		} else if len(output) > b.artifactThreshold {
			artifactContext, cancelArtifact := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			artifact, artifactErr := b.artifacts.Put(artifactContext, "application/json", output, map[string]any{
				"tool_call_id": request.CallID, "tool": request.Tool,
			})
			cancelArtifact()
			if artifactErr != nil {
				executeErr = errors.Join(executeErr, artifactErr)
			} else {
				response.Artifact = &artifact
			}
		} else {
			response.Output = output
		}
	}
	state := "succeeded"
	if executeErr != nil {
		state = "failed"
		if errors.Is(executionContext.Err(), context.DeadlineExceeded) {
			state = "timed_out"
			executeErr = fmt.Errorf("%w: %s", ErrToolTimeout, request.Tool)
		} else if errors.Is(executionContext.Err(), context.Canceled) || errors.Is(executeErr, context.Canceled) {
			state = "cancelled"
		}
	}
	if err := b.finishToolCall(request.CallID, state, response, executeErr, finishedAt); err != nil && executeErr == nil {
		executeErr = err
	}
	if err := b.recordFinished(request, actor, state, executeErr, finishedAt); err != nil && executeErr == nil {
		executeErr = err
	}
	if executeErr != nil {
		slog.Error("tool execution finished", "component", "tool_broker", "operation", "execute", "result", state, "tool", request.Tool, "tool_call_id", request.CallID, "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "duration_ms", finishedAt.Sub(startedAt).Milliseconds(), "error", executeErr)
	} else {
		slog.Info("tool execution finished", "component", "tool_broker", "operation", "execute", "result", state, "tool", request.Tool, "tool_call_id", request.CallID, "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "duration_ms", finishedAt.Sub(startedAt).Milliseconds())
	}
	return response, executeErr
}

func (b *Broker) authorize(ctx context.Context, tx *sql.Tx, request toolapi.Request, definition toolapi.Definition, scope Authorization, timeout time.Duration) (string, error) {
	risk := policy.RiskLevel(definition.Risk)
	// Authority is Policy + Capability Grant, not task execution_mode.
	// Plan mode only changes when Start is allowed (after human approval); grants still fail closed.
	result := b.policy.Evaluate(risk)
	if result.Action == policy.ActionDeny {
		return "", fmt.Errorf("%w: %s", ErrToolDenied, result.Reason)
	}
	candidates := grantCandidates(request)
	if result.RequiresApproval && len(candidates) == 0 {
		return "", fmt.Errorf("%w: capability grant is required for %s", ErrToolDenied, definition.Risk)
	}
	if len(candidates) == 0 {
		return "", nil
	}
	var lastErr error
	for _, candidate := range candidates {
		err := b.approvals.AuthorizeAndConsumeTx(ctx, tx, approval.GrantRequest{
			GrantID: approval.GrantID(candidate),
			TaskID:  kernel.TaskID(request.TaskID), PlanID: kernel.PlanID(request.PlanID),
			StepID: kernel.StepID(request.StepID), PlanHash: request.PlanHash,
			Capability: scope.Capability, Path: scope.Path, Command: scope.Command,
			Arguments: scope.Arguments, NetworkDomain: scope.NetworkDomain,
			Duration: timeout, Now: b.now().UTC(),
		})
		if err == nil {
			return candidate, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("%w: no candidate capability grant matched: %v", ErrToolDenied, lastErr)
}

func grantCandidates(request toolapi.Request) []string {
	values := make([]string, 0, len(request.CapabilityGrantIDs)+1)
	seen := make(map[string]struct{}, len(request.CapabilityGrantIDs)+1)
	for _, value := range append([]string{request.CapabilityGrantID}, request.CapabilityGrantIDs...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func validateRequest(request toolapi.Request) error {
	if strings.TrimSpace(request.CallID) == "" || strings.TrimSpace(request.RunID) == "" ||
		strings.TrimSpace(request.TaskID) == "" || strings.TrimSpace(request.PlanID) == "" ||
		strings.TrimSpace(request.StepID) == "" || strings.TrimSpace(request.Tool) == "" {
		return fmt.Errorf("%w: call, run, task, plan, step, and tool IDs are required", ErrInvalidTool)
	}
	if len(request.Arguments) == 0 || !json.Valid(request.Arguments) {
		return fmt.Errorf("%w: arguments must be valid JSON", ErrInvalidTool)
	}
	return nil
}

func (b *Broker) resolveTimeout(requestedMillis, defaultMillis int64) (time.Duration, error) {
	millis := requestedMillis
	if millis <= 0 {
		millis = defaultMillis
	}
	timeout := time.Duration(millis) * time.Millisecond
	if timeout <= 0 || timeout > b.maximumTimeout {
		return 0, fmt.Errorf("%w: timeout must be between 1ms and %s", ErrToolDenied, b.maximumTimeout)
	}
	return timeout, nil
}

func (b *Broker) insertToolCall(ctx context.Context, tx *sql.Tx, request toolapi.Request, startedAt time.Time) error {
	encoded, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal tool call request: %w", err)
	}
	var grant any
	if strings.TrimSpace(request.CapabilityGrantID) != "" {
		grant = request.CapabilityGrantID
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO tool_calls (
			tool_call_id, run_id, step_id, grant_id, tool_name, state, request, started_at
		) VALUES (?, ?, ?, ?, ?, 'running', ?, ?)`,
		request.CallID, request.RunID, request.StepID, grant, request.Tool, string(encoded), startedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert tool call: %w", err)
	}
	return nil
}

func (b *Broker) finishToolCall(callID, state string, response toolapi.Response, executeErr error, finishedAt time.Time) error {
	payload := map[string]any{"response": response}
	if executeErr != nil {
		payload["error"] = executeErr.Error()
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal tool response: %w", err)
	}
	result, err := b.db.Exec(`
		UPDATE tool_calls SET state = ?, response = ?, finished_at = ? WHERE tool_call_id = ?`,
		state, string(encoded), finishedAt.Format(time.RFC3339Nano), callID,
	)
	if err != nil {
		return fmt.Errorf("finish tool call: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("finish tool call: call %s not found", callID)
	}
	return nil
}

func (b *Broker) recordDenied(request toolapi.Request, actor string, cause error) {
	resourceID := strings.TrimSpace(request.CallID)
	if resourceID == "" {
		resourceID = "unidentified"
	}
	slog.Warn("tool execution denied", "component", "tool_broker", "operation", "authorize", "result", "denied", "tool", request.Tool, "tool_call_id", resourceID, "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "error", cause)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = b.audit.Record(ctx, audit.Entry{
		Actor: actor, Action: "tool.execute", ResourceType: "tool_call", ResourceID: resourceID,
		Outcome: "denied", TraceID: request.TraceID,
		Details: map[string]any{"tool": request.Tool, "error": cause.Error()},
	})
}

func (b *Broker) recordFinished(request toolapi.Request, actor, outcome string, executeErr error, occurredAt time.Time) error {
	details := map[string]any{"tool": request.Tool, "run_id": request.RunID, "step_id": request.StepID}
	if executeErr != nil {
		details["error"] = executeErr.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return b.audit.Record(ctx, audit.Entry{
		OccurredAt: occurredAt, Actor: actor, Action: "tool.execute", ResourceType: "tool_call",
		ResourceID: request.CallID, Outcome: outcome, TraceID: request.TraceID, Details: details,
	})
}

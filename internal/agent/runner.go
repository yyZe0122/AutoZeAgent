// Package agent executes provider tool calls through the mandatory Tool Broker.
// Planning remains owned by internal/planner; this package only executes an
// already selected plan step.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

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

type Config struct {
	Provider      StreamingProvider
	Broker        ToolBroker
	Records       *RecordStore
	Model         string
	MaxIterations int
	// Stream is optional; when set, CollectStream events are teed for local UI.
	Stream StreamObserver
}

type Runner struct {
	mu            sync.RWMutex
	provider      StreamingProvider
	broker        ToolBroker
	records       *RecordStore
	model         string
	maxIterations int
	stream        StreamObserver
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
		maxIterations = 8
	}
	if maxIterations < 1 || maxIterations > 64 {
		return nil, errors.New("agent maximum iterations must be between 1 and 64")
	}
	return &Runner{
		provider: config.Provider, broker: config.Broker, records: config.Records,
		model: model, maxIterations: maxIterations, stream: config.Stream,
	}, nil
}

// SetProvider replaces the streaming provider used by subsequent runs.
func (r *Runner) SetProvider(provider StreamingProvider) error {
	if provider == nil {
		return errors.New("agent provider is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.provider = provider
	return nil
}

// SetModel replaces the model id used by subsequent provider requests.
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

// Model returns the currently selected model id.
func (r *Runner) Model() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.model
}

func (r *Runner) snapshot() (StreamingProvider, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.provider, r.model
}

func (r *Runner) Run(ctx context.Context, request RunRequest) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("%w: context is required", ErrInvalidRequest)
	}
	if err := validateRunRequest(request); err != nil {
		return Result{}, err
	}
	startedAt := time.Now()
	slog.Info("agent run started", "component", "agent", "operation", "run", "result", "started", "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "allowed_tools", len(request.AllowedTools))
	definitions, advertised, err := selectDefinitions(r.broker.Definitions(), request.AllowedTools)
	if err != nil {
		slog.Error("agent tool selection failed", "component", "agent", "operation", "select_tools", "result", "failed", "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "error", err)
		return Result{}, err
	}
	records, err := r.records.Prepare(ctx, request.RunID, request.Messages)
	if err != nil {
		slog.Error("agent history preparation failed", "component", "agent", "operation", "prepare_history", "result", "failed", "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "error", err)
		return Result{}, err
	}
	messages, seenCallIDs, result, completed, err := r.restore(
		ctx, request.RunID, records, advertised, request.MaxTotalTokens, request.MaxCostMicros,
	)
	if err != nil {
		slog.Error("agent history recovery failed", "component", "agent", "operation", "restore", "result", "failed", "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "record_count", len(records), "error", err)
		return result, err
	}
	// Prefix prior session turns for the provider; they are not part of this run's records.
	if len(request.History) > 0 {
		messages = append(append([]providerapi.Message(nil), request.History...), messages...)
	}
	slog.Info("agent history restored", "component", "agent", "operation", "restore", "result", "succeeded", "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "record_count", len(records), "completed", completed)
	if completed {
		slog.Info("agent run completed from recovery", "component", "agent", "operation", "run", "result", "succeeded", "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "iterations", result.Iterations, "tool_calls", len(result.ToolCalls), "duration_ms", time.Since(startedAt).Milliseconds())
		return result, nil
	}

	provider, model := r.snapshot()
	for result.Iterations < r.maxIterations {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := budgetExhausted(result.Usage, request.MaxTotalTokens, request.MaxCostMicros); err != nil {
			return result, err
		}
		maxOutputTokens := request.MaxOutputTokens
		if request.MaxTotalTokens > 0 {
			remaining := request.MaxTotalTokens - int64(result.Usage.TotalTokens)
			if maxOutputTokens <= 0 || maxOutputTokens > remaining {
				maxOutputTokens = remaining
			}
		}
		providerStartedAt := time.Now()
		response, attempts, err := r.collectProviderResponse(ctx, provider, providerapi.CompletionRequest{
			Model:           model,
			Messages:        messages,
			Tools:           definitions,
			MaxOutputTokens: maxOutputTokens,
			Temperature:     request.Temperature,
		}, request)
		result.Iterations++
		addUsage(&result.Usage, response.Usage)
		durationMillis := time.Since(providerStartedAt).Milliseconds()
		if err != nil {
			logArgs := []any{
				"component", "provider", "operation", "stream", "result",
				"failed", "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID,
				"iteration", result.Iterations, "provider_attempts", attempts, "model", model, "duration_ms", durationMillis,
			}
			var providerErr *providerapi.ProviderError
			if errors.As(err, &providerErr) {
				logArgs = append(logArgs,
					"provider", providerErr.Provider, "error_kind", providerErr.Kind, "status_code", providerErr.StatusCode,
					"retryable", providerErr.Retryable, "retry_after_ms", providerErr.RetryAfter.Milliseconds(),
				)
			}
			logArgs = append(logArgs, "error", err)
			slog.Error("provider iteration failed", logArgs...)
			return result, err
		}
		slog.Info("provider iteration completed",
			"component", "provider", "operation", "stream", "result", "succeeded",
			"run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID,
			"iteration", result.Iterations, "provider_attempts", attempts, "model", model, "duration_ms", durationMillis,
			"input_tokens", response.Usage.InputTokens, "output_tokens", response.Usage.OutputTokens, "total_tokens", response.Usage.TotalTokens,
			"tool_calls", len(response.ToolCalls), "finish_reason", response.FinishReason,
		)

		assistant := providerapi.Message{
			Role: providerapi.RoleAssistant, Content: response.Content,
			Thinking:  response.Thinking,
			ToolCalls: append([]providerapi.ToolCall(nil), response.ToolCalls...),
		}
		if len(response.ToolCalls) == 0 {
			if strings.TrimSpace(response.Content) == "" && strings.TrimSpace(response.Thinking) == "" {
				slog.Warn("provider returned empty final response", "component", "provider", "operation", "stream", "result", "failed", "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "iteration", result.Iterations, "model", model, "error", ErrEmptyResponse)
				return result, ErrEmptyResponse
			}
			if strings.TrimSpace(response.Content) == "" {
				// Thinking-only is not a user-visible final answer.
				slog.Warn("provider returned empty final response", "component", "provider", "operation", "stream", "result", "failed", "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "iteration", result.Iterations, "model", model, "error", ErrEmptyResponse)
				return result, ErrEmptyResponse
			}
			if _, err := r.records.AppendAssistant(ctx, request.RunID, assistant, response.Usage, response.FinishReason); err != nil {
				return result, err
			}
			if err := budgetExceeded(result.Usage, request.MaxTotalTokens, request.MaxCostMicros); err != nil {
				return result, err
			}
			result.Content = response.Content
			slog.Info("agent run completed", "component", "agent", "operation", "run", "result", "succeeded", "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "iterations", result.Iterations, "tool_calls", len(result.ToolCalls), "duration_ms", time.Since(startedAt).Milliseconds())
			return result, nil
		}

		if err := validateToolCalls(response.ToolCalls, advertised, seenCallIDs); err != nil {
			return result, err
		}
		if _, err := r.records.AppendAssistant(ctx, request.RunID, assistant, response.Usage, response.FinishReason); err != nil {
			return result, err
		}
		if err := budgetExceeded(result.Usage, request.MaxTotalTokens, request.MaxCostMicros); err != nil {
			return result, err
		}
		messages = append(messages, assistant)
		for _, call := range response.ToolCalls {
			toolResponse, err := r.broker.Execute(ctx, toolapi.Request{
				CallID: call.ID, RunID: request.RunID, TaskID: request.TaskID,
				PlanID: request.PlanID, PlanHash: request.PlanHash, StepID: request.StepID,
				CapabilityGrantID: request.CapabilityGrantID, CapabilityGrantIDs: append([]string(nil), request.CapabilityGrantIDs[call.Name]...),
				Actor: request.Actor, TraceID: request.TraceID,
				Tool: call.Name, Arguments: json.RawMessage(call.Arguments), TimeoutMillis: request.ToolTimeoutMillis,
			})
			if err != nil {
				if !errors.Is(err, toolapi.ErrDenied) {
					return result, err
				}
				content := toolDeniedContent(call, err)
				toolResponse = toolapi.Response{
					CallID: call.ID, Tool: call.Name, Output: json.RawMessage(content),
				}
				result.ToolCalls = append(result.ToolCalls, toolResponse)
				toolMessage := providerapi.Message{Role: providerapi.RoleTool, ToolCallID: call.ID, Content: content}
				if _, err := r.records.AppendToolResult(ctx, request.RunID, toolMessage); err != nil {
					return result, err
				}
				messages = append(messages, toolMessage)
				slog.Info("tool call denied; feeding result back to provider",
					"component", "agent", "operation", "tool_denied", "result", "continued",
					"run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID,
					"step_id", request.StepID, "trace_id", request.TraceID,
					"tool", call.Name, "tool_call_id", call.ID, "error", err,
				)
				continue
			}
			result.ToolCalls = append(result.ToolCalls, toolResponse)
			content, err := toolResultContent(toolResponse)
			if err != nil {
				return result, err
			}
			toolMessage := providerapi.Message{Role: providerapi.RoleTool, ToolCallID: call.ID, Content: content}
			if _, err := r.records.AppendToolResult(ctx, request.RunID, toolMessage); err != nil {
				return result, err
			}
			messages = append(messages, toolMessage)
		}
	}
	slog.Warn("agent iteration limit reached", "component", "agent", "operation", "run", "result", "failed", "run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID, "step_id", request.StepID, "trace_id", request.TraceID, "iterations", result.Iterations, "error", ErrMaxIterations)
	return result, ErrMaxIterations
}

func (r *Runner) restore(
	ctx context.Context,
	runID string,
	records []RunRecord,
	advertised map[string]struct{},
	maxTotalTokens int64,
	maxCostMicros int64,
) ([]providerapi.Message, map[string]struct{}, Result, bool, error) {
	messages := make([]providerapi.Message, 0, len(records))
	seenCallIDs := make(map[string]struct{})
	pending := make([]providerapi.ToolCall, 0)
	result := Result{}
	generated := false
	var storedToolCalls map[string]storedToolCall
	loadToolCall := func(callID string) (toolapi.Response, error) {
		if storedToolCalls == nil {
			var err error
			storedToolCalls, err = r.records.loadToolCalls(ctx, runID)
			if err != nil {
				return toolapi.Response{}, err
			}
		}
		stored, ok := storedToolCalls[callID]
		if !ok {
			return toolapi.Response{}, fmt.Errorf("%w: tool call %s has no durable execution", ErrRecoveryBlocked, callID)
		}
		return decodeSucceededToolCall(callID, stored)
	}

	for index, record := range records {
		switch record.Type {
		case RecordInputMessage:
			if generated {
				return nil, nil, result, false, fmt.Errorf("%w: input record after generated output", ErrCorruptHistory)
			}
			messages = append(messages, cloneMessage(record.Message))
		case RecordAssistantMessage:
			generated = true
			if len(pending) != 0 {
				return nil, nil, result, false, fmt.Errorf("%w: assistant record before prior tool results", ErrCorruptHistory)
			}
			result.Iterations++
			addUsage(&result.Usage, record.Usage)
			if err := budgetExceeded(result.Usage, maxTotalTokens, maxCostMicros); err != nil {
				return nil, nil, result, false, err
			}
			if len(record.Message.ToolCalls) == 0 {
				if strings.TrimSpace(record.Message.Content) == "" || index != len(records)-1 {
					return nil, nil, result, false, fmt.Errorf("%w: invalid final response position", ErrCorruptHistory)
				}
				result.Content = record.Message.Content
				messages = append(messages, cloneMessage(record.Message))
				return messages, seenCallIDs, result, true, nil
			}
			if err := validateToolCalls(record.Message.ToolCalls, advertised, seenCallIDs); err != nil {
				return nil, nil, result, false, fmt.Errorf("%w: %v", ErrCorruptHistory, err)
			}
			pending = append(pending[:0], record.Message.ToolCalls...)
			messages = append(messages, cloneMessage(record.Message))
		case RecordToolResult:
			generated = true
			if len(pending) == 0 || record.Message.ToolCallID != pending[0].ID {
				return nil, nil, result, false, fmt.Errorf("%w: unexpected tool result %s", ErrCorruptHistory, record.Message.ToolCallID)
			}
			response, err := loadToolCall(record.Message.ToolCallID)
			if err != nil {
				if !errors.Is(err, ErrRecoveryBlocked) || !isDeniedToolResult(record.Message.Content) {
					return nil, nil, result, false, err
				}
				// Recoverable deny: history is authoritative; no succeeded tool_calls row.
				response = toolapi.Response{
					CallID: record.Message.ToolCallID, Tool: pending[0].Name,
					Output: json.RawMessage(record.Message.Content),
				}
			} else {
				matches, matchErr := toolResultMatches(response, record.Message.Content)
				if matchErr != nil {
					return nil, nil, result, false, matchErr
				}
				if !matches {
					return nil, nil, result, false, fmt.Errorf("%w: tool result %s differs from execution record", ErrCorruptHistory, record.Message.ToolCallID)
				}
			}
			result.ToolCalls = append(result.ToolCalls, response)
			messages = append(messages, cloneMessage(record.Message))
			pending = pending[1:]
		default:
			return nil, nil, result, false, fmt.Errorf("%w: unknown record type %q", ErrCorruptHistory, record.Type)
		}
	}

	for _, call := range pending {
		response, err := loadToolCall(call.ID)
		if err != nil {
			return nil, nil, result, false, err
		}
		content, err := toolResultContent(response)
		if err != nil {
			return nil, nil, result, false, err
		}
		toolMessage := providerapi.Message{Role: providerapi.RoleTool, ToolCallID: call.ID, Content: content}
		if _, err := r.records.AppendToolResult(ctx, runID, toolMessage); err != nil {
			return nil, nil, result, false, err
		}
		result.ToolCalls = append(result.ToolCalls, response)
		messages = append(messages, toolMessage)
	}
	return messages, seenCallIDs, result, false, nil
}

func validateRunRequest(request RunRequest) error {
	if strings.TrimSpace(request.RunID) == "" || strings.TrimSpace(request.TaskID) == "" ||
		strings.TrimSpace(request.PlanID) == "" || strings.TrimSpace(request.PlanHash) == "" ||
		strings.TrimSpace(request.StepID) == "" || len(request.Messages) == 0 {
		return fmt.Errorf("%w: run, task, plan, plan hash, step, and messages are required", ErrInvalidRequest)
	}
	if request.MaxTotalTokens < 0 || request.MaxCostMicros < 0 {
		return fmt.Errorf("%w: token and cost budgets cannot be negative", ErrInvalidRequest)
	}
	return nil
}

func selectDefinitions(all []toolapi.Definition, allowed []string) ([]providerapi.ToolDefinition, map[string]struct{}, error) {
	available := make(map[string]toolapi.Definition, len(all))
	for _, definition := range all {
		available[definition.Name] = definition
	}
	advertised := make(map[string]struct{}, len(allowed))
	definitions := make([]providerapi.ToolDefinition, 0, len(allowed))
	for _, rawName := range allowed {
		name := strings.TrimSpace(rawName)
		definition, exists := available[name]
		if name == "" || !exists {
			return nil, nil, fmt.Errorf("%w: unknown allowed tool %q", ErrInvalidRequest, rawName)
		}
		if _, duplicate := advertised[name]; duplicate {
			return nil, nil, fmt.Errorf("%w: duplicate allowed tool %q", ErrInvalidRequest, name)
		}
		advertised[name] = struct{}{}
		definitions = append(definitions, providerapi.ToolDefinition{
			Name: definition.Name, Description: definition.Description, InputSchema: append(json.RawMessage(nil), definition.InputSchema...),
		})
	}
	return definitions, advertised, nil
}

func validateToolCalls(calls []providerapi.ToolCall, advertised, seen map[string]struct{}) error {
	batch := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" || !json.Valid([]byte(call.Arguments)) {
			return ErrInvalidToolCall
		}
		if _, exists := advertised[call.Name]; !exists {
			return fmt.Errorf("%w: %s", ErrUnadvertisedTool, call.Name)
		}
		if _, duplicate := seen[call.ID]; duplicate {
			return fmt.Errorf("%w: duplicate call ID %s", ErrInvalidToolCall, call.ID)
		}
		if _, duplicate := batch[call.ID]; duplicate {
			return fmt.Errorf("%w: duplicate call ID %s", ErrInvalidToolCall, call.ID)
		}
		batch[call.ID] = struct{}{}
	}
	for callID := range batch {
		seen[callID] = struct{}{}
	}
	return nil
}

func toolResultMatches(response toolapi.Response, content string) (bool, error) {
	if len(response.Output) > 0 {
		return string(response.Output) == content, nil
	}
	persisted, err := toolResultContent(response)
	if err != nil {
		return false, err
	}
	return persisted == content, nil
}

func toolResultContent(response toolapi.Response) (string, error) {
	if len(response.Output) > 0 {
		return string(response.Output), nil
	}
	if response.Artifact != nil {
		encoded, err := json.Marshal(map[string]any{"artifact": response.Artifact})
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	}
	return `{}`, nil
}

func toolDeniedContent(call providerapi.ToolCall, err error) string {
	payload := map[string]any{
		"error":   "tool_denied",
		"tool":    call.Name,
		"message": err.Error(),
		"hint":    "Do not retry the same arguments. Fix the tool input: prefer absolute paths under configured workspace roots; relative paths are resolved against the workspace root; keep duration within the approved grant; use only allowed tools and paths.",
	}
	encoded, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return `{"error":"tool_denied","message":"tool call denied"}`
	}
	return string(encoded)
}

func isDeniedToolResult(content string) bool {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return false
	}
	return payload.Error == "tool_denied"
}

func addUsage(total *providerapi.Usage, next providerapi.Usage) {
	total.InputTokens += next.InputTokens
	total.OutputTokens += next.OutputTokens
	total.TotalTokens += next.TotalTokens
	if total.Cost.Currency == "" || total.Cost.Currency == next.Cost.Currency {
		if total.Cost.Currency == "" {
			total.Cost.Currency = next.Cost.Currency
		}
		total.Cost.Micros += next.Cost.Micros
	}
}

const (
	maxProviderAttempts  = 3
	maxProviderRetryWait = 5 * time.Second
)

func (r *Runner) collectProviderResponse(ctx context.Context, provider StreamingProvider, req providerapi.CompletionRequest, runReq RunRequest) (providerapi.CompletionResponse, int, error) {
	for attempt := 1; attempt <= maxProviderAttempts; attempt++ {
		response, err := r.collectOnce(ctx, provider, req, runReq)
		if err == nil {
			return response, attempt, nil
		}
		delay, retry := providerRetryDelay(err, attempt)
		if !retry {
			return response, attempt, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return providerapi.CompletionResponse{}, attempt, ctx.Err()
		case <-timer.C:
		}
	}
	return providerapi.CompletionResponse{}, maxProviderAttempts, errors.New("provider retry loop exhausted")
}

func (r *Runner) collectOnce(ctx context.Context, provider StreamingProvider, req providerapi.CompletionRequest, runReq RunRequest) (providerapi.CompletionResponse, error) {
	if r.stream == nil {
		return providerapi.CollectStream(ctx, provider, req)
	}
	var accumulator providerapi.StreamAccumulator
	side := func(event providerapi.StreamEvent) error {
		r.stream.Publish(runReq.SessionID, runReq.TaskID, runReq.RunID, event)
		return nil
	}
	handler := providerapi.TeeStreamHandler(accumulator.Add, side)
	if err := provider.Stream(ctx, req, handler); err != nil {
		return providerapi.CompletionResponse{}, err
	}
	return accumulator.Response()
}

func providerRetryDelay(err error, attempt int) (time.Duration, bool) {
	var providerErr *providerapi.ProviderError
	if !errors.As(err, &providerErr) || !providerErr.Retryable || attempt >= maxProviderAttempts {
		return 0, false
	}
	delay := providerErr.RetryAfter
	if delay <= 0 {
		delay = 100 * time.Millisecond * time.Duration(1<<(attempt-1))
	}
	if delay > maxProviderRetryWait {
		delay = maxProviderRetryWait
	}
	return delay, true
}

func budgetExhausted(usage providerapi.Usage, maxTotalTokens, maxCostMicros int64) error {
	if maxTotalTokens > 0 && int64(usage.TotalTokens) >= maxTotalTokens {
		return ErrTokenBudgetExceeded
	}
	if maxCostMicros > 0 && usage.Cost.Micros >= maxCostMicros {
		return ErrCostBudgetExceeded
	}
	return nil
}

func budgetExceeded(usage providerapi.Usage, maxTotalTokens, maxCostMicros int64) error {
	if maxTotalTokens > 0 && int64(usage.TotalTokens) > maxTotalTokens {
		return ErrTokenBudgetExceeded
	}
	if maxCostMicros > 0 && usage.Cost.Micros > maxCostMicros {
		return ErrCostBudgetExceeded
	}
	return nil
}

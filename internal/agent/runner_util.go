package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
	"github.com/yyZe0122/yunmengze-agent/pkg/toolapi"
)

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
		encoded, err := json.Marshal(map[string]any{
			"truncated":   true,
			"artifact_id": response.Artifact.ID,
			"artifact":    response.Artifact,
		})
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
	total.CacheReadTokens += next.CacheReadTokens
	total.CacheWriteTokens += next.CacheWriteTokens
	if total.Cost.Currency == "" || total.Cost.Currency == next.Cost.Currency {
		if total.Cost.Currency == "" {
			total.Cost.Currency = next.Cost.Currency
		}
		total.Cost.Micros += next.Cost.Micros
	}
}

func cloneGrantMap(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
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

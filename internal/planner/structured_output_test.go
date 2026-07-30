package planner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"autozeagent.local/autozeagent/pkg/providerapi"
)

type scriptedProvider struct {
	responses []providerapi.CompletionResponse
	errors    []error
	requests  []providerapi.CompletionRequest
}

func (p *scriptedProvider) Name() string { return "scripted" }

func (p *scriptedProvider) Complete(context.Context, providerapi.CompletionRequest) (providerapi.CompletionResponse, error) {
	return providerapi.CompletionResponse{}, errors.New("not used")
}

func (p *scriptedProvider) Stream(_ context.Context, request providerapi.CompletionRequest, handler providerapi.StreamHandler) error {
	p.requests = append(p.requests, request)
	index := len(p.requests) - 1
	if index < len(p.errors) && p.errors[index] != nil {
		return p.errors[index]
	}
	if index >= len(p.responses) {
		return errors.New("unexpected provider request")
	}
	return providerapi.EmitResponse(p.responses[index], handler)
}

func (p *scriptedProvider) Health(context.Context) providerapi.HealthStatus {
	return providerapi.HealthStatus{Healthy: true}
}

func TestGenerateIncludesPlanSchemaAndSucceedsWithoutRepair(t *testing.T) {
	provider := &scriptedProvider{responses: []providerapi.CompletionResponse{{Content: structuredValidPlanJSON}}}
	planner, err := New(Config{Provider: provider, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := planner.Generate(context.Background(), structuredGenerateRequest())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Objective != "Inspect workspace" {
		t.Fatalf("objective = %q", plan.Objective)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	messages := provider.requests[0].Messages
	if len(messages) == 0 || messages[0].Role != providerapi.RoleSystem {
		t.Fatalf("first message = %+v, want system message", messages)
	}
	if !strings.Contains(messages[0].Content, string(planner.Schema())) {
		t.Fatal("planner system message does not contain the canonical plan schema")
	}
}

func TestGenerateRepairsInvalidOutputOnce(t *testing.T) {
	invalid := `{"budget":{"max_tokens":100,"max_cost_micros":0,"max_duration_ms":1000},"steps":[]}`
	provider := &scriptedProvider{responses: []providerapi.CompletionResponse{
		{Content: invalid},
		{Content: structuredValidPlanJSON},
	}}
	planner, err := New(Config{Provider: provider, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := planner.Generate(context.Background(), structuredGenerateRequest()); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(provider.requests))
	}
	repairMessages := provider.requests[1].Messages
	if !containsMessage(repairMessages, providerapi.RoleAssistant, invalid) {
		t.Fatal("repair request does not include the previous assistant output")
	}
	if !containsMessageFragment(repairMessages, providerapi.RoleUser, "missing required property \"objective\"") {
		t.Fatalf("repair request does not include the local validation error: %+v", repairMessages)
	}
}

func TestGenerateDoesNotRetryProviderError(t *testing.T) {
	providerErr := errors.New("provider unavailable")
	provider := &scriptedProvider{errors: []error{providerErr}}
	planner, err := New(Config{Provider: provider, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = planner.Generate(context.Background(), structuredGenerateRequest())
	if !errors.Is(err, providerErr) {
		t.Fatalf("error = %v, want provider error", err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
}

func TestGenerateDoesNotRetryToolCall(t *testing.T) {
	provider := &scriptedProvider{responses: []providerapi.CompletionResponse{{
		ToolCalls: []providerapi.ToolCall{{ID: "call-1", Name: "fs_read", Arguments: `{}`}},
	}}}
	planner, err := New(Config{Provider: provider, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = planner.Generate(context.Background(), structuredGenerateRequest())
	if !errors.Is(err, ErrProviderToolCall) {
		t.Fatalf("error = %v, want ErrProviderToolCall", err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
}

func containsMessage(messages []providerapi.Message, role providerapi.Role, content string) bool {
	for _, message := range messages {
		if message.Role == role && message.Content == content {
			return true
		}
	}
	return false
}

func containsMessageFragment(messages []providerapi.Message, role providerapi.Role, fragment string) bool {
	for _, message := range messages {
		if message.Role == role && strings.Contains(message.Content, fragment) {
			return true
		}
	}
	return false
}

func structuredGenerateRequest() GenerateRequest {
	return GenerateRequest{
		TaskID:    "task-structured-output",
		PlanID:    "plan-structured-output",
		Revision:  1,
		Objective: "Inspect workspace",
	}
}

const structuredValidPlanJSON = `{
	"objective": "Inspect workspace",
	"budget": {
		"max_tokens": 4096,
		"max_cost_micros": 0,
		"max_duration_ms": 120000
	},
	"steps": [
		{
			"title": "Inspect",
			"risk": "R0",
			"expected_side_effects": [],
			"rollback": "No changes were made",
			"timeout_ms": 30000,
			"capabilities": []
		}
	]
}`

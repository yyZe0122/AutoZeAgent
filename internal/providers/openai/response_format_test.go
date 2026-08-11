package openai

import (
	"encoding/json"
	"testing"

	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
)

func TestRequestBodyDefaultsToJSONSchema(t *testing.T) {
	provider, err := New(Config{BaseURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := provider.requestBody(structuredResponseRequest(), false)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ResponseFormat struct {
			Type       string          `json:"type"`
			JSONSchema json.RawMessage `json:"json_schema"`
		} `json:"response_format"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ResponseFormat.Type != ResponseFormatJSONSchema {
		t.Fatalf("response format type = %q", payload.ResponseFormat.Type)
	}
	if len(payload.ResponseFormat.JSONSchema) == 0 {
		t.Fatal("json_schema payload is missing")
	}
}

func TestRequestBodySupportsJSONObject(t *testing.T) {
	provider, err := New(Config{BaseURL: "https://example.com", ResponseFormat: ResponseFormatJSONObject})
	if err != nil {
		t.Fatal(err)
	}
	body, err := provider.requestBody(structuredResponseRequest(), true)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	format, ok := payload["response_format"].(map[string]any)
	if !ok {
		t.Fatal("response_format payload is missing")
	}
	if format["type"] != ResponseFormatJSONObject {
		t.Fatalf("response format type = %v", format["type"])
	}
	if _, exists := format["json_schema"]; exists {
		t.Fatal("json_object mode must omit json_schema")
	}

	if payload["stream"] != true {
		t.Fatal("stream flag is missing")
	}
}

func TestNewRejectsUnsupportedResponseFormat(t *testing.T) {
	if _, err := New(Config{BaseURL: "https://example.com", ResponseFormat: "yaml"}); err == nil {
		t.Fatal("expected unsupported response format error")
	}
}

func structuredResponseRequest() providerapi.CompletionRequest {
	return providerapi.CompletionRequest{
		Model:    "test-model",
		Messages: []providerapi.Message{{Role: providerapi.RoleUser, Content: "Return JSON."}},
		ResponseSchema: &providerapi.JSONSchema{
			Name:   "answer",
			Strict: true,
			Schema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"}},"required":["x"]}`),
		},
	}
}

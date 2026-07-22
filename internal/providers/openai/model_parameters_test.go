package openai

import (
	"encoding/json"
	"testing"
)

func TestRequestBodyIncludesModelParameters(t *testing.T) {
	provider, err := New(Config{BaseURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	temperature := 0.25
	request := structuredResponseRequest()
	request.MaxOutputTokens = 2048
	request.Temperature = &temperature
	request.ReasoningEffort = "high"
	body, err := provider.requestBody(request, false)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		MaxTokens       int64   `json:"max_tokens"`
		Temperature     float64 `json:"temperature"`
		ReasoningEffort string  `json:"reasoning_effort"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.MaxTokens != 2048 || payload.Temperature != temperature || payload.ReasoningEffort != "high" {
		t.Fatalf("payload = %+v", payload)
	}
}

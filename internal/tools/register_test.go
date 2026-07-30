package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"autozeagent.local/autozeagent/internal/policy"
	"autozeagent.local/autozeagent/pkg/toolapi"
)

type namedTool struct {
	name string
}

func (t *namedTool) Definition() toolapi.Definition {
	return toolapi.Definition{
		Name:                 t.name,
		Description:          "test tool",
		InputSchema:          json.RawMessage(`{"type":"object","properties":{}}`),
		Risk:                 string(policy.RiskR0),
		DefaultTimeoutMillis: 100,
	}
}

func (t *namedTool) Authorization(json.RawMessage) (Authorization, error) {
	return Authorization{Capability: t.name}, nil
}

func (t *namedTool) Execute(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"ok":true}`), nil
}

func TestRegisterRejectsInvalidToolName(t *testing.T) {
	broker := &Broker{registry: make(map[string]Tool)}
	err := broker.Register(&namedTool{name: "bad.name"})
	if !errors.Is(err, ErrInvalidTool) {
		t.Fatalf("Register() error = %v, want ErrInvalidTool", err)
	}
	err = broker.Register(&namedTool{name: "good_name-1"})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
}

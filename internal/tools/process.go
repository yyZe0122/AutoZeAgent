package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"autozeagent.local/autozeagent/internal/policy"
	"autozeagent.local/autozeagent/internal/tools/internal/executor"
	"autozeagent.local/autozeagent/pkg/toolapi"
)

type processTool struct {
	guard  *PathGuard
	runner *executor.Runner
}

type processInput struct {
	Command     string            `json:"command"`
	Arguments   []string          `json:"arguments,omitempty"`
	Directory   string            `json:"directory"`
	Environment map[string]string `json:"environment,omitempty"`
}

func newProcessTool(roots []string, runner *executor.Runner) (Tool, error) {
	if runner == nil {
		return nil, errors.New("process runner is required")
	}
	guard, err := NewPathGuard(roots)
	if err != nil {
		return nil, err
	}
	return &processTool{guard: guard, runner: runner}, nil
}

func (t *processTool) Definition() toolapi.Definition {
	return toolapi.Definition{
		Name: "process_exec", Description: "Execute an approved command in an approved working directory. directory must be an absolute path under an approved grant path; command and arguments must match the approved grant exactly.",
		Risk: string(policy.RiskR2), DefaultTimeoutMillis: 30000,
		InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["command","directory"],"properties":{"command":{"type":"string"},"arguments":{"type":"array","items":{"type":"string"}},"directory":{"type":"string"},"environment":{"type":"object","additionalProperties":{"type":"string"}}}}`),
	}
}

func (t *processTool) Authorization(raw json.RawMessage) (Authorization, error) {
	var input processInput
	if err := decodeStrict(raw, &input); err != nil {
		return Authorization{}, err
	}
	input.Command = strings.TrimSpace(input.Command)
	if input.Command == "" {
		return Authorization{}, errors.New("process command is required")
	}
	directory, err := t.guard.Resolve(input.Directory)
	if err != nil {
		return Authorization{}, err
	}
	return Authorization{Capability: "process_exec", Path: directory, Command: input.Command, Arguments: append([]string(nil), input.Arguments...)}, nil
}

func (t *processTool) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input processInput
	if err := decodeStrict(raw, &input); err != nil {
		return nil, err
	}
	input.Command = strings.TrimSpace(input.Command)
	if input.Command == "" {
		return nil, errors.New("process command is required")
	}
	directory, err := t.guard.Resolve(input.Directory)
	if err != nil {
		return nil, err
	}
	result, runErr := t.runner.Run(ctx, executor.Request{
		Command: input.Command, Arguments: input.Arguments, Directory: directory, Environment: input.Environment,
	})
	encoded, encodeErr := encodeResult(result)
	if encodeErr != nil {
		return nil, encodeErr
	}
	return encoded, runErr
}

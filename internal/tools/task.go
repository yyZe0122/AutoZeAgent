package tools

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/agent"
	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	"github.com/yyZe0122/yunmengze-agent/internal/policy"
	"github.com/yyZe0122/yunmengze-agent/internal/runmeta"
	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
	"github.com/yyZe0122/yunmengze-agent/pkg/toolapi"
)

const (
	// DefaultMaxTaskDepth: top-level is 0; spawn denied when parent Depth >= max.
	DefaultMaxTaskDepth = 2

	taskSystemPrompt = "You are a sub-agent of YunmengZe. Complete the delegated task. " +
		"Reply helpfully in the user's language. Prefer absolute paths under the workspace. " +
		"Do not claim tool success without evidence."
)

// SubagentRunner runs a child agent loop. Implemented by *agent.Runner.
type SubagentRunner interface {
	Run(context.Context, agent.RunRequest) (agent.Result, error)
}

// TaskToolConfig wires the ADR-039 task builtin.
type TaskToolConfig struct {
	DB       *sql.DB
	Runner   SubagentRunner
	MaxDepth int
	Now      func() time.Time
}

type taskTool struct {
	db       *sql.DB
	mu       sync.RWMutex
	runner   SubagentRunner
	maxDepth int
	now      func() time.Time
}

// NewTaskTool builds the task tool. Call SetRunner after agent construction if needed.
func NewTaskTool(config TaskToolConfig) (*taskTool, error) {
	if config.DB == nil {
		return nil, errors.New("task tool database is required")
	}
	maxDepth := config.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxTaskDepth
	}
	now := config.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &taskTool{db: config.DB, runner: config.Runner, maxDepth: maxDepth, now: now}, nil
}

// SetRunner attaches the agent runner (composition root may set after agent.New).
func (t *taskTool) SetRunner(runner SubagentRunner) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.runner = runner
}

func (t *taskTool) Definition() toolapi.Definition {
	return toolapi.Definition{
		Name:                 "task",
		Description:          "Delegate work to a synchronous sub-agent run (same task, inherited tools/grants). Returns the sub-agent final reply.",
		Risk:                 string(policy.RiskR1),
		DefaultTimeoutMillis: 30 * 60 * 1000,
		InputSchema: json.RawMessage(`{
			"type":"object",
			"additionalProperties":false,
			"required":["prompt"],
			"properties":{
				"prompt":{"type":"string","description":"Instructions for the sub-agent"},
				"tools":{"type":"array","items":{"type":"string"},"description":"Optional subset of parent allowed tools"}
			}
		}`),
	}
}

type taskInput struct {
	Prompt string   `json:"prompt"`
	Tools  []string `json:"tools,omitempty"`
}

func (t *taskTool) Authorization(raw json.RawMessage) (Authorization, error) {
	var input taskInput
	if err := decodeStrict(raw, &input); err != nil {
		return Authorization{}, err
	}
	if strings.TrimSpace(input.Prompt) == "" {
		return Authorization{}, errors.New("prompt is required")
	}
	return Authorization{Capability: "task"}, nil
}

func (t *taskTool) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	parent, ok := runmeta.From(ctx)
	if !ok || strings.TrimSpace(parent.RunID) == "" {
		return nil, fmt.Errorf("%w: task requires parent run context", ErrToolDenied)
	}
	t.mu.RLock()
	runner := t.runner
	t.mu.RUnlock()
	if runner == nil {
		return nil, fmt.Errorf("%w: task sub-agent runner is not configured", ErrToolDenied)
	}
	if parent.Depth >= t.maxDepth {
		return encodeResult(map[string]any{
			"error":   "max_depth_exceeded",
			"depth":   parent.Depth,
			"max":     t.maxDepth,
			"message": "cannot spawn nested task: maximum depth reached",
		})
	}

	var input taskInput
	if err := decodeStrict(raw, &input); err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return nil, errors.New("prompt is required")
	}

	allowed := filterChildTools(parent.AllowedTools, input.Tools)
	childRunID := childRunID(parent.RunID, prompt)
	if callID := strings.TrimSpace(parent.CallID); callID != "" {
		childRunID = childRunIDFromCall(parent.RunID, callID)
	}

	if err := t.insertChildRun(ctx, childRunID, parent); err != nil {
		return nil, err
	}

	req := agent.RunRequest{
		RunID: childRunID, TaskID: parent.TaskID, SessionID: parent.SessionID,
		PlanID: parent.PlanID, PlanHash: parent.PlanHash, StepID: parent.StepID,
		Actor: parent.Actor, TraceID: parent.TraceID,
		Messages: []providerapi.Message{
			{Role: providerapi.RoleSystem, Content: taskSystemPrompt},
			{Role: providerapi.RoleUser, Content: prompt},
		},
		AllowedTools:       allowed,
		CapabilityGrantIDs: parent.CapabilityGrantIDs,
		MaxOutputTokens:    parent.MaxOutputTokens,
		MaxTotalTokens:     parent.MaxTotalTokens,
		MaxCostMicros:      parent.MaxCostMicros,
		ToolTimeoutMillis:  parent.ToolTimeoutMillis,
		Depth:              parent.Depth + 1,
		Role:               "subagent",
	}

	result, err := runner.Run(ctx, req)
	finished := t.now().UTC()
	if err != nil {
		_ = t.finishChildRun(context.WithoutCancel(ctx), childRunID, kernel.RunFailed, err.Error(), finished)
		return encodeResult(map[string]any{
			"run_id":  childRunID,
			"state":   "failed",
			"error":   err.Error(),
			"content": result.Content,
		})
	}
	_ = t.finishChildRun(context.WithoutCancel(ctx), childRunID, kernel.RunCompleted, "", finished)
	return encodeResult(map[string]any{
		"run_id":     childRunID,
		"state":      "completed",
		"content":    result.Content,
		"iterations": result.Iterations,
		"usage": map[string]any{
			"input_tokens":  result.Usage.InputTokens,
			"output_tokens": result.Usage.OutputTokens,
			"total_tokens":  result.Usage.TotalTokens,
		},
	})
}

// filterChildTools returns allowed tools for the child: parent set, optionally
// intersected with requested names. Empty requested → full parent set.
func filterChildTools(parentAllowed, requested []string) []string {
	parentSet := make(map[string]struct{}, len(parentAllowed))
	parentOrder := make([]string, 0, len(parentAllowed))
	for _, name := range parentAllowed {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, exists := parentSet[name]; exists {
			continue
		}
		parentSet[name] = struct{}{}
		parentOrder = append(parentOrder, name)
	}
	if len(requested) == 0 {
		return parentOrder
	}
	out := make([]string, 0, len(requested))
	seen := make(map[string]struct{})
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := parentSet[name]; !ok {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func (t *taskTool) insertChildRun(ctx context.Context, childRunID string, parent runmeta.Context) error {
	now := t.now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	_, err := t.db.ExecContext(ctx, `
		INSERT INTO runs (run_id, task_id, plan_id, state, started_at, finished_at, error, version, updated_at, step_id, parent_run_id)
		VALUES (?, ?, ?, ?, ?, NULL, NULL, 1, ?, NULL, ?)`,
		childRunID, parent.TaskID, parent.PlanID, kernel.RunRunning, stamp, stamp, parent.RunID,
	)
	if err != nil {
		return fmt.Errorf("insert child run: %w", err)
	}
	return nil
}

func (t *taskTool) finishChildRun(ctx context.Context, runID string, state kernel.RunState, failure string, finished time.Time) error {
	stamp := finished.UTC().Format(time.RFC3339Nano)
	if len(failure) > 2048 {
		failure = failure[:2048]
	}
	var errCol any
	if failure != "" {
		errCol = failure
	}
	_, err := t.db.ExecContext(ctx, `
		UPDATE runs SET state = ?, version = version + 1, updated_at = ?, finished_at = ?, error = ?
		WHERE run_id = ?`,
		state, stamp, stamp, errCol, runID,
	)
	return err
}

func childRunID(parentRunID, prompt string) string {
	return "c" + shortHash("child-run", parentRunID, prompt)[:31]
}

func childRunIDFromCall(parentRunID, callID string) string {
	return "c" + shortHash("child-run", parentRunID, callID)[:31]
}

func shortHash(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

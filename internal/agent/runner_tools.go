package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"

	"github.com/yyZe0122/yunmengze-agent/internal/runmeta"
	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
	"github.com/yyZe0122/yunmengze-agent/pkg/toolapi"
)

// readOnlyParallelTools may run in the same provider step concurrently.
// Broker dbMu serializes grant consume + tool_calls writes; tool bodies overlap.
var readOnlyParallelTools = map[string]struct{}{
	"fs_read": {}, "fs_list": {}, "fs_stat": {}, "fs_glob": {}, "fs_grep": {},
}

func allReadOnlyParallel(calls []providerapi.ToolCall) bool {
	if len(calls) <= 1 {
		return false
	}
	for _, call := range calls {
		if _, ok := readOnlyParallelTools[call.Name]; !ok {
			return false
		}
	}
	return true
}

func (r *Runner) executeToolCalls(ctx context.Context, request RunRequest, calls []providerapi.ToolCall) ([]providerapi.Message, []toolapi.Response, error) {
	if len(calls) == 0 {
		return nil, nil, nil
	}
	if allReadOnlyParallel(calls) {
		return r.executeToolCallsParallel(ctx, request, calls)
	}
	return r.executeToolCallsSerial(ctx, request, calls)
}

func (r *Runner) executeOneToolCall(ctx context.Context, request RunRequest, call providerapi.ToolCall) (providerapi.Message, toolapi.Response, error) {
	toolCtx := runmeta.With(ctx, runmeta.Context{
		RunID: request.RunID, TaskID: request.TaskID, SessionID: request.SessionID,
		PlanID: request.PlanID, PlanHash: request.PlanHash, StepID: request.StepID,
		Actor: request.Actor, TraceID: request.TraceID,
		AllowedTools:       append([]string(nil), request.AllowedTools...),
		CapabilityGrantIDs: cloneGrantMap(request.CapabilityGrantIDs),
		MaxOutputTokens:    request.MaxOutputTokens, MaxTotalTokens: request.MaxTotalTokens,
		MaxCostMicros: request.MaxCostMicros, ToolTimeoutMillis: request.ToolTimeoutMillis,
		Depth: request.Depth, CallID: call.ID,
	})
	toolResponse, err := r.broker.Execute(toolCtx, toolapi.Request{
		CallID: call.ID, RunID: request.RunID, TaskID: request.TaskID,
		PlanID: request.PlanID, PlanHash: request.PlanHash, StepID: request.StepID,
		CapabilityGrantID: request.CapabilityGrantID, CapabilityGrantIDs: append([]string(nil), request.CapabilityGrantIDs[call.Name]...),
		Actor: request.Actor, TraceID: request.TraceID,
		Tool: call.Name, Arguments: json.RawMessage(call.Arguments), TimeoutMillis: request.ToolTimeoutMillis,
	})
	if err != nil {
		if !errors.Is(err, toolapi.ErrDenied) {
			return providerapi.Message{}, toolapi.Response{}, err
		}
		content := toolDeniedContent(call, err)
		toolResponse = toolapi.Response{
			CallID: call.ID, Tool: call.Name, Output: json.RawMessage(content),
		}
		toolMessage := providerapi.Message{Role: providerapi.RoleTool, ToolCallID: call.ID, Content: content}
		slog.Info("tool call denied; feeding result back to provider",
			"component", "agent", "operation", "tool_denied", "result", "continued",
			"run_id", request.RunID, "task_id", request.TaskID, "plan_id", request.PlanID,
			"step_id", request.StepID, "trace_id", request.TraceID,
			"tool", call.Name, "tool_call_id", call.ID, "error", err,
		)
		return toolMessage, toolResponse, nil
	}
	content, err := toolResultContent(toolResponse)
	if err != nil {
		return providerapi.Message{}, toolapi.Response{}, err
	}
	toolMessage := providerapi.Message{Role: providerapi.RoleTool, ToolCallID: call.ID, Content: content}
	return toolMessage, toolResponse, nil
}

func (r *Runner) executeToolCallsSerial(ctx context.Context, request RunRequest, calls []providerapi.ToolCall) ([]providerapi.Message, []toolapi.Response, error) {
	outMsgs := make([]providerapi.Message, 0, len(calls))
	outResps := make([]toolapi.Response, 0, len(calls))
	for _, call := range calls {
		toolMessage, toolResponse, err := r.executeOneToolCall(ctx, request, call)
		if err != nil {
			return nil, nil, err
		}
		if _, err := r.records.AppendToolResult(ctx, request.RunID, toolMessage); err != nil {
			return nil, nil, err
		}
		outMsgs = append(outMsgs, toolMessage)
		outResps = append(outResps, toolResponse)
	}
	return outMsgs, outResps, nil
}

// executeToolCallsParallel runs read-only tools concurrently. Broker dbMu serializes
// grant TX / tool_calls writes; tool bodies (disk IO) overlap. Results keep call order.
func (r *Runner) executeToolCallsParallel(ctx context.Context, request RunRequest, calls []providerapi.ToolCall) ([]providerapi.Message, []toolapi.Response, error) {
	type result struct {
		msg  providerapi.Message
		resp toolapi.Response
		err  error
	}
	results := make([]result, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(i int, call providerapi.ToolCall) {
			defer wg.Done()
			msg, resp, err := r.executeOneToolCall(ctx, request, call)
			results[i] = result{msg: msg, resp: resp, err: err}
		}(i, call)
	}
	wg.Wait()
	outMsgs := make([]providerapi.Message, 0, len(calls))
	outResps := make([]toolapi.Response, 0, len(calls))
	for i := range results {
		if results[i].err != nil {
			return nil, nil, results[i].err
		}
		if _, err := r.records.AppendToolResult(ctx, request.RunID, results[i].msg); err != nil {
			return nil, nil, err
		}
		outMsgs = append(outMsgs, results[i].msg)
		outResps = append(outResps, results[i].resp)
	}
	return outMsgs, outResps, nil
}

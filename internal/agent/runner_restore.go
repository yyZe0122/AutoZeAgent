package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
	"github.com/yyZe0122/yunmengze-agent/pkg/toolapi"
)

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
			if generated && record.Message.Role != providerapi.RoleUser {
				return nil, nil, result, false, fmt.Errorf("%w: input record after generated output", ErrCorruptHistory)
			}
			if generated && len(pending) != 0 {
				return nil, nil, result, false, fmt.Errorf("%w: user record before prior tool results", ErrCorruptHistory)
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
				if strings.TrimSpace(record.Message.Content) == "" {
					return nil, nil, result, false, fmt.Errorf("%w: invalid final response position", ErrCorruptHistory)
				}
				result.Content = record.Message.Content
				messages = append(messages, cloneMessage(record.Message))
				if index == len(records)-1 {
					return messages, seenCallIDs, result, true, nil
				}
				continue
			}
			if _, err := classifyToolCalls(record.Message.ToolCalls, advertised, seenCallIDs); err != nil {
				// Unadvertised / invalid calls may already have observation results in history (ADR-052).
				if !hasToolCallIDs(record.Message.ToolCalls) {
					return nil, nil, result, false, fmt.Errorf("%w: %v", ErrCorruptHistory, err)
				}
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
				if !errors.Is(err, ErrRecoveryBlocked) || !isObservationToolResult(record.Message.Content) {
					return nil, nil, result, false, err
				}
				// Recoverable observation (deny/fail/unadvertised): history is authoritative.
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

func messagesFromRecords(records []RunRecord) []providerapi.Message {
	if len(records) == 0 {
		return nil
	}
	out := make([]providerapi.Message, 0, len(records))
	for _, rec := range records {
		out = append(out, cloneMessage(rec.Message))
	}
	return out
}

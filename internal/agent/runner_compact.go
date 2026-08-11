package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"autozeagent.local/autozeagent/internal/contextpack"
	"autozeagent.local/autozeagent/pkg/providerapi"
)

// CompactSummaryPrompt is the structured head-summary system prompt (OpenCode/Hermes-style).
const CompactSummaryPrompt = `Output exactly the Markdown structure shown inside <template> and keep the section order unchanged. Do not include the <template> tags in your response.
<template>
## Objective
- [one or two brief sentences describing what the user is trying to accomplish]

## Important Details
- [constraints/preferences, decisions and why, important facts/assumptions, exact context needed to continue, or "(none)"]

## Work State
### Completed
- [finished work, verified facts, or changes made; otherwise "(none)"]

### Active
- [current work, partial changes, or investigation state; otherwise "(none)"]

### Blocked
- [blockers, failing commands, or unknowns; otherwise "(none)"]

## Next Move
1. [immediate concrete action, or "(none)"]
2. [next action if known, or "(none)"]

## Relevant Files
- [file or directory path: why it matters, or "(none)"]
</template>

Rules:
- Keep every section, even when empty.
- Use terse bullets, not prose paragraphs.
- Preserve exact file paths, symbols, commands, error strings, URLs, and identifiers when known.
- Do not mention the summary process or that context was compacted.`

const maxStepsPrompt = `CRITICAL - MAXIMUM STEPS REACHED
The maximum number of tool-loop steps allowed for this task has been reached. Tools are disabled until next user input. Respond with text only.
Any attempt to use tools is a critical violation. Respond with text ONLY.`

const loopDetectedPrompt = `CRITICAL - TOOL LOOP DETECTED
The same tool calls (name + arguments) repeated too many times in a short window. Tools are disabled for this turn. Summarize what you tried, what failed, and ask the user how to proceed. Respond with text only.`

// CompactSummary asks the model for a structured head summary (no tools). Used by chat compaction.
// Uses the compact model role when configured (ADR-045); otherwise main.
func (r *Runner) CompactSummary(ctx context.Context, head []providerapi.Message) (string, error) {
	return r.CompactSummaryWithPrevious(ctx, head, "")
}

// CompactSummaryWithPrevious merges previousSummary when non-empty (anchor update).
func (r *Runner) CompactSummaryWithPrevious(ctx context.Context, head []providerapi.Message, previousSummary string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("%w: context is required", ErrInvalidRequest)
	}
	if len(head) == 0 && strings.TrimSpace(previousSummary) == "" {
		return "", nil
	}
	provider, model, _ := r.snapshotForRole("compact")
	body := contextpack.ExtractiveSummary(head, 12_000)
	user := body
	if prev := strings.TrimSpace(previousSummary); prev != "" {
		user = "Update the anchored summary below using the conversation history above.\n" +
			"Preserve still-true details, remove stale details, and merge in the new facts.\n" +
			"<previous-summary>\n" + prev + "\n</previous-summary>\n\n" + body
	} else {
		user = "Create a new anchored summary from the conversation history.\n\n" + body
	}
	req := providerapi.CompletionRequest{
		Model: model,
		Messages: []providerapi.Message{
			{Role: providerapi.RoleSystem, Content: CompactSummaryPrompt},
			{Role: providerapi.RoleUser, Content: user},
		},
		MaxOutputTokens: 2048,
	}
	var content strings.Builder
	err := provider.Stream(ctx, req, func(ev providerapi.StreamEvent) error {
		if ev.Type == providerapi.StreamDelta {
			content.WriteString(ev.ContentDelta)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(content.String())
	if out == "" {
		return contextpack.ExtractiveSummary(head, 4_000), nil
	}
	return out, nil
}

// maybeCompactMidTurn shrinks the in-memory transcript when pressure is high after tools.
// Durable session compaction is left to chatsession packSessionHistory / ForceCompact.
func (r *Runner) maybeCompactMidTurn(ctx context.Context, messages []providerapi.Message, request RunRequest, model string) []providerapi.Message {
	if len(messages) < 4 {
		return messages
	}
	window := request.ContextWindow
	if window <= 0 {
		r.mu.RLock()
		window = r.contextWindow
		r.mu.RUnlock()
	}
	if window <= 0 {
		// Unknown window: still reclaim if raw estimate is huge.
		if contextpack.EstimateMessages(messages) < 32_000 {
			return messages
		}
		slog.Info("agent mid-turn compact (unknown window, large estimate)",
			"component", "agent", "operation", "mid_turn_compact", "result", "succeeded",
			"run_id", request.RunID, "task_id", request.TaskID)
		return r.compactMessagesForOverflow(ctx, messages)
	}
	maxOut := request.MaxOutputTokens
	if maxOut <= 0 {
		maxOut = 8_192
	}
	usable := contextpack.UsableWindow(window, maxOut, 0)
	raw := contextpack.EstimateMessages(messages)
	est := raw
	if r.calibrator != nil {
		est = r.calibrator.Apply(model, raw)
	}
	// Pack with a representative budget (tools still advertised next iter).
	budget := usable
	if budget > 0 {
		budget = usable * 85 / 100
		if budget < 1024 {
			budget = 1024
		}
	}
	packed := contextpack.Pack(messages, contextpack.PackOptions{Budget: budget, Model: model, MaxToolResultRunes: r.maxToolResultRunes})
	if !contextpack.ShouldCompact(est, usable, packed.OverBudget) {
		return messages
	}
	slog.Info("agent mid-turn compact",
		"component", "agent", "operation", "mid_turn_compact", "result", "succeeded",
		"run_id", request.RunID, "task_id", request.TaskID,
		"estimate_tokens", est, "usable_tokens", usable)
	return r.compactMessagesForOverflow(ctx, messages)
}

// compactMessagesForOverflow shrinks the in-memory provider transcript after a
// context-window error: L2/L3 pack, optional LLM head summary, tool-pair safe split.
func (r *Runner) compactMessagesForOverflow(ctx context.Context, messages []providerapi.Message) []providerapi.Message {
	if len(messages) == 0 {
		return messages
	}
	// Force reclaim of older tool bodies and drop old turns under a tight budget.
	packed := contextpack.Pack(messages, contextpack.PackOptions{
		Budget:             8_000,
		MaxToolResultRunes: 4_096,
		ToolProtectTokens:  8_000,
		ToolReclaimMin:     1_000,
	})
	out := packed.Messages
	if len(out) > 6 {
		head, tail := contextpack.SplitHeadTail(out, 2)
		if len(head) > 0 {
			sum, err := r.CompactSummary(ctx, head)
			if err != nil || strings.TrimSpace(sum) == "" {
				sum = contextpack.ExtractiveSummary(head, 4_000)
			}
			out = append([]providerapi.Message{{
				Role:    providerapi.RoleSystem,
				Content: "[Prior context — compacted after provider overflow]\n" + sum,
			}}, stripLeadingSystemMsgs(tail)...)
		}
	}
	return out
}

func stripLeadingSystemMsgs(msgs []providerapi.Message) []providerapi.Message {
	i := 0
	for i < len(msgs) && msgs[i].Role == providerapi.RoleSystem {
		i++
	}
	if i == 0 {
		return msgs
	}
	return msgs[i:]
}

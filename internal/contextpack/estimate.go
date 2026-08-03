// Package contextpack estimates tokens and packs provider message views
// without mutating durable SQLite transcripts.
package contextpack

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"autozeagent.local/autozeagent/pkg/providerapi"
)

// CharsPerToken is the default heuristic (industry ~4 chars/token).
const CharsPerToken = 4

// PerMessageOverhead accounts for role/framing tokens not in content text.
const PerMessageOverhead int64 = 6

// EstimateText returns a non-zero token estimate for plain text.
func EstimateText(text string) int64 {
	n := int64(utf8.RuneCountInString(text))
	if n <= 0 {
		return 0
	}
	est := (n + CharsPerToken - 1) / CharsPerToken
	if est < 1 {
		return 1
	}
	return est
}

// EstimateMessage estimates one provider message (content + tool calls + thinking).
func EstimateMessage(msg providerapi.Message) int64 {
	n := PerMessageOverhead + EstimateText(msg.Content) + EstimateText(msg.Thinking)
	if msg.ToolCallID != "" {
		n += EstimateText(msg.ToolCallID)
	}
	for _, tc := range msg.ToolCalls {
		n += EstimateText(tc.ID) + EstimateText(tc.Name) + EstimateText(tc.Arguments) + 4
	}
	return n
}

// EstimateMessages sums message estimates.
func EstimateMessages(messages []providerapi.Message) int64 {
	var total int64
	for _, msg := range messages {
		total += EstimateMessage(msg)
	}
	return total
}

// EstimateTools estimates tool definition payload size.
func EstimateTools(tools []providerapi.ToolDefinition) int64 {
	if len(tools) == 0 {
		return 0
	}
	raw, err := json.Marshal(tools)
	if err != nil {
		var n int64
		for _, t := range tools {
			n += EstimateText(t.Name) + EstimateText(t.Description) + int64(len(t.InputSchema))/CharsPerToken + 8
		}
		return n
	}
	return EstimateText(string(raw))
}

// ShouldCompact reports whether LLM/extractive head summary should run.
// Trigger: pressure >= 75% of usable and packing still over budget (or no budget left for tail).
func ShouldCompact(estimate, usable int64, overBudget bool) bool {
	if usable <= 0 {
		return overBudget
	}
	if overBudget {
		return true
	}
	return float64(estimate) >= DefaultPressureTrigger*float64(usable)
}

// SplitHeadTail keeps the last keepUserTurns user turns as tail; rest is head (excluding leading system).
func SplitHeadTail(messages []providerapi.Message, keepUserTurns int) (head, tail []providerapi.Message) {
	if keepUserTurns < 1 {
		keepUserTurns = 2
	}
	if len(messages) == 0 {
		return nil, nil
	}
	sysEnd := 0
	for sysEnd < len(messages) && messages[sysEnd].Role == providerapi.RoleSystem {
		sysEnd++
	}
	// Find user turn starts from the end.
	var userIdx []int
	for i := sysEnd; i < len(messages); i++ {
		if messages[i].Role == providerapi.RoleUser {
			userIdx = append(userIdx, i)
		}
	}
	if len(userIdx) <= keepUserTurns {
		return nil, cloneMessages(messages)
	}
	cut := userIdx[len(userIdx)-keepUserTurns]
	head = cloneMessages(messages[sysEnd:cut])
	tail = cloneMessages(append(append([]providerapi.Message{}, messages[:sysEnd]...), messages[cut:]...))
	return head, tail
}

// ExtractiveSummary builds a non-LLM summary from head messages (fallback).
func ExtractiveSummary(head []providerapi.Message, maxRunes int) string {
	if maxRunes < 256 {
		maxRunes = 256
	}
	var b strings.Builder
	b.WriteString("Session context summary (local extractive; prior turns compacted):\n")
	for _, msg := range head {
		role := string(msg.Role)
		line := role + ": " + truncateRunes(msg.Content, 240)
		if msg.Role == providerapi.RoleAssistant && len(msg.ToolCalls) > 0 {
			names := make([]string, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				names = append(names, tc.Name)
			}
			line += " tools=" + strings.Join(names, ",")
		}
		b.WriteString(line)
		b.WriteByte('\n')
		if b.Len() > maxRunes {
			break
		}
	}
	return truncateRunes(b.String(), maxRunes)
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// UsableWindow is context capacity left for prompt after output and reserve.
// Zero contextWindow means unknown → no hard pack limit (caller may still trim).
func UsableWindow(contextWindow, maxOutput, reserve int64) int64 {
	if contextWindow <= 0 {
		return 0
	}
	if maxOutput < 0 {
		maxOutput = 0
	}
	if reserve < 0 {
		reserve = 0
	}
	if reserve == 0 {
		reserve = defaultReserve(maxOutput)
	}
	usable := contextWindow - maxOutput - reserve
	if usable < 1024 {
		usable = 1024
	}
	return usable
}

func defaultReserve(maxOutput int64) int64 {
	r := maxOutput
	if r < 1024 {
		r = 1024
	}
	if r > 8192 {
		r = 8192
	}
	return r
}

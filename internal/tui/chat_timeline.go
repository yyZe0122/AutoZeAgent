package tui

import (
	"fmt"
	"strings"

	"autozeagent.local/autozeagent/internal/gatewayclient"
)

// buildChatTimeline prefers transcript messages (OpenCode/Crush style).
// Falls back to the workflow shell when no messages are loaded yet.
func buildChatTimeline(
	messages []gatewayclient.TranscriptMessage,
	task *gatewayclient.Task,
	plan *gatewayclient.Plan,
	runs []gatewayclient.Run,
) []timelineItem {
	if len(messages) > 0 {
		toolNames := toolNameByCallID(messages)
		items := make([]timelineItem, 0, len(messages)+4)
		for _, msg := range messages {
			items = append(items, transcriptToItem(msg, toolNames))
		}
		// Status footer when task is mid-flight.
		if task != nil {
			switch task.State {
			case gatewayclient.TaskStatePlanning, gatewayclient.TaskStateWaitingApproval, gatewayclient.TaskStateApproved:
				items = append(items, timelineItem{
					Kind: tlSystem, At: task.UpdatedAt, Title: "legacy: " + task.State, State: task.State,
				})
			case gatewayclient.TaskStateRunning:
				title := "running"
				items = append(items, timelineItem{
					Kind: tlSystem, At: task.UpdatedAt, Title: title, State: task.State,
				})
			}
		}
		return items
	}
	return buildTimeline(task, plan, runs)
}

// runningStatusTitle returns the mid-flight status line (permission-aware).
func runningStatusTitle(task *gatewayclient.Task, pendingPerms int) string {
	if task == nil {
		return ""
	}
	if task.State != gatewayclient.TaskStateRunning {
		return string(task.State)
	}
	if pendingPerms > 0 {
		return fmt.Sprintf("waiting permission (%d) · 1–4 or /perm", pendingPerms)
	}
	return "running"
}

// patchRunningStatus updates the last system "running" row title when present.
func patchRunningStatus(items []timelineItem, title string) []timelineItem {
	if title == "" || len(items) == 0 {
		return items
	}
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Kind == tlSystem && (items[i].State == gatewayclient.TaskStateRunning ||
			strings.HasPrefix(items[i].Title, "running") ||
			strings.HasPrefix(items[i].Title, "waiting permission")) {
			items[i].Title = title
			break
		}
	}
	return items
}

// appendLiveDraft adds an in-progress assistant bubble for typewriter UI.
func appendLiveDraft(items []timelineItem, thinking, content string) []timelineItem {
	if strings.TrimSpace(thinking) == "" && strings.TrimSpace(content) == "" {
		return items
	}
	var b strings.Builder
	if think := strings.TrimSpace(thinking); think != "" {
		b.WriteString("thinking: ")
		b.WriteString(think)
		b.WriteByte('\n')
	}
	b.WriteString(content)
	if content != "" {
		b.WriteString("▌")
	}
	return append(items, timelineItem{
		Kind: tlRun, Title: "assistant…", Body: b.String(), State: "streaming",
	})
}

func transcriptToItem(msg gatewayclient.TranscriptMessage, toolNames map[string]string) timelineItem {
	switch strings.ToLower(msg.Role) {
	case "user":
		return timelineItem{
			Kind: tlUser, At: msg.CreatedAt, Title: "you", Body: msg.Content,
		}
	case "assistant":
		var b strings.Builder
		if think := strings.TrimSpace(msg.Thinking); think != "" {
			b.WriteString("thinking: ")
			b.WriteString(foldBody(think))
			b.WriteByte('\n')
		}
		if msg.Content != "" {
			b.WriteString(msg.Content)
		}
		if len(msg.ToolCalls) > 0 {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			for _, tc := range msg.ToolCalls {
				b.WriteString(formatToolCallLine(tc))
				b.WriteByte('\n')
			}
		}
		body := strings.TrimRight(b.String(), "\n")
		if body == "" {
			body = "(empty assistant message)"
		}
		return timelineItem{
			Kind: tlRun, At: msg.CreatedAt, Title: "assistant", Body: foldBody(body),
		}
	case "tool":
		name := ""
		if toolNames != nil {
			name = toolNames[msg.ToolCallID]
		}
		title := formatToolResultTitle(msg.ToolCallID, name)
		return timelineItem{
			Kind: tlSystem, At: msg.CreatedAt, Title: title, Body: foldBody(msg.Content),
		}
	default:
		return timelineItem{
			Kind: tlSystem, At: msg.CreatedAt, Title: msg.Role, Body: foldBody(msg.Content),
		}
	}
}

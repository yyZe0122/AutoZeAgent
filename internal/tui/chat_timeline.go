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
	prompt *gatewayclient.Prompt,
	runs []gatewayclient.Run,
) []timelineItem {
	if len(messages) > 0 {
		items := make([]timelineItem, 0, len(messages)+4)
		for _, msg := range messages {
			items = append(items, transcriptToItem(msg))
		}
		// Status footer when task is mid-flight.
		if task != nil {
			switch task.State {
			case gatewayclient.TaskStatePlanning:
				items = append(items, timelineItem{
					Kind: tlSystem, At: task.UpdatedAt, Title: "planning…", State: task.State,
				})
			case gatewayclient.TaskStateWaitingApproval:
				items = append(items, timelineItem{
					Kind: tlSystem, At: task.UpdatedAt, Title: "waiting approval", State: task.State,
				})
			case gatewayclient.TaskStateRunning:
				items = append(items, timelineItem{
					Kind: tlSystem, At: task.UpdatedAt, Title: "running", State: task.State,
				})
			}
			if prompt != nil {
				items = append(items, timelineItem{
					Kind: tlPlan, Title: fmt.Sprintf("plan %s", prompt.PlanID),
					Body:  fmt.Sprintf("rev=%d · %d step(s)", prompt.Revision, len(prompt.Steps)),
					State: "ready",
				})
			}
		}
		return items
	}
	return buildTimeline(task, plan, prompt, runs)
}

// appendLiveDraft adds an in-progress assistant bubble for typewriter UI.
func appendLiveDraft(items []timelineItem, thinking, content string) []timelineItem {
	if strings.TrimSpace(thinking) == "" && strings.TrimSpace(content) == "" {
		return items
	}
	var b strings.Builder
	if think := strings.TrimSpace(thinking); think != "" {
		b.WriteString("💭 ")
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

func transcriptToItem(msg gatewayclient.TranscriptMessage) timelineItem {
	switch strings.ToLower(msg.Role) {
	case "user":
		return timelineItem{
			Kind: tlUser, At: msg.CreatedAt, Title: "you", Body: msg.Content,
		}
	case "assistant":
		var b strings.Builder
		if think := strings.TrimSpace(msg.Thinking); think != "" {
			b.WriteString("💭 ")
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
				b.WriteString(fmt.Sprintf("⚙ %s(%s)\n", tc.Name, truncate(tc.Arguments, 120)))
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
		title := "tool"
		if msg.ToolCallID != "" {
			title = "tool " + shortID(msg.ToolCallID)
		}
		return timelineItem{
			Kind: tlSystem, At: msg.CreatedAt, Title: title, Body: foldBody(msg.Content),
		}
	default:
		return timelineItem{
			Kind: tlSystem, At: msg.CreatedAt, Title: msg.Role, Body: foldBody(msg.Content),
		}
	}
}

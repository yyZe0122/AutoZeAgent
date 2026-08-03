package tui

import (
	"fmt"
	"strings"
)

// renderSessionView is the sole main-area body: chat transcript (or empty welcome).
func renderSessionView(m *model) string {
	if m.sessionID == "" && m.task == nil && len(m.messages) == 0 {
		return renderEmptySession(m)
	}
	var b strings.Builder
	if m.sessionID != "" && m.sessionID != "…" {
		b.WriteString(styleTitle.Render("Session " + shortID(string(m.sessionID))))
	} else if m.task != nil {
		b.WriteString(styleTitle.Render("Task " + shortID(string(m.task.ID))))
	} else {
		b.WriteString(styleTitle.Render("Session"))
	}
	if m.task != nil {
		b.WriteString("  ")
		b.WriteString(stateBadge(m.task.State))
		if m.task.ExecutionMode != "" {
			b.WriteString("  ")
			b.WriteString(styleDim.Render(m.task.ExecutionMode))
		}
	}
	b.WriteString("\n")
	if m.task != nil && m.task.Title != "" {
		b.WriteString(styleDim.Render(truncate(m.task.Title, 80)) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(m.tlCache.render(m.timeline))
	return b.String()
}

func renderEmptySession(m *model) string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Session") + "\n")
	b.WriteString(styleDim.Render("Type to chat. /new opens a fresh session.") + "\n")
	b.WriteString(styleDim.Render("/sessions lists past chats · Tab toggles agent (build) | plan (read-only).") + "\n")
	if len(m.sessions) > 0 {
		b.WriteString("\n")
		b.WriteString(styleMuted.Render("Recent sessions") + "\n")
		limit := min(5, len(m.sessions))
		for i := 0; i < limit; i++ {
			s := m.sessions[i]
			title := s.Title
			if title == "" {
				title = string(s.ID)
			}
			b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
				shortID(string(s.ID)), stateBadge(s.LatestState), styleDim.Render(truncate(title, 48))))
		}
	} else if len(m.tasks) > 0 {
		b.WriteString("\n")
		b.WriteString(styleMuted.Render("Recent tasks") + "\n")
		limit := min(5, len(m.tasks))
		for i := 0; i < limit; i++ {
			t := m.tasks[i]
			b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
				shortID(string(t.ID)), stateBadge(t.State), styleDim.Render(truncate(t.Title, 48))))
		}
	}
	return b.String()
}

// renderAgentView kept as alias for any remaining callers/tests.
func renderAgentView(m *model) string {
	return renderSessionView(m)
}

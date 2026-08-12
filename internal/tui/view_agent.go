package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
	opts := renderOpts{Width: bubbleWidth(m.width), Theme: m.theme}
	b.WriteString(m.tlCache.render(m.timeline, m.expand, opts))
	return b.String()
}

func renderEmptySession(m *model) string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Ready") + "\n\n")
	b.WriteString(styleDim.Render("Type a message to start · /new for a fresh session") + "\n\n")
	chips := lipgloss.JoinHorizontal(lipgloss.Top,
		renderChip("Tab · mode"),
		"  ",
		renderChip("/sessions"),
		"  ",
		renderChip("e expand"),
		"  ",
		renderChip("/help"),
	)
	b.WriteString(chips + "\n")
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

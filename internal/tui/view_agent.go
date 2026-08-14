package tui

import (
	"fmt"
	"strings"
)

func renderSessionView(m *model) string {
	if m.sessionID == "" && m.task == nil && len(m.messages) == 0 {
		return renderEmptySession(m)
	}
	opts := renderOpts{Width: bubbleWidth(m.viewport.Width), Theme: m.theme, Stream: &m.streamMD}
	if opts.Width < 36 {
		opts.Width = bubbleWidth(m.width)
	}
	return m.tlCache.render(m.timeline, m.expand, opts)
}

func renderEmptySession(m *model) string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("ready") + "\n")
	b.WriteString(styleDim.Render("type a message to start · ") + paintKeywords("/new") + styleDim.Render(" for a fresh session") + "\n")
	b.WriteString(styleMuted.Render("Tab mode  ·  ") + paintKeywords("/sessions") + styleMuted.Render("  ·  e expand  ·  ") + paintKeywords("/help") + "\n")
	if len(m.sessions) > 0 {
		b.WriteString("\n")
		b.WriteString(styleMuted.Render("recent") + "\n")
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
		b.WriteString(styleMuted.Render("recent") + "\n")
		limit := min(5, len(m.tasks))
		for i := 0; i < limit; i++ {
			t := m.tasks[i]
			b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
				shortID(string(t.ID)), stateBadge(t.State), styleDim.Render(truncate(t.Title, 48))))
		}
	}
	return b.String()
}

func renderAgentView(m *model) string {
	return renderSessionView(m)
}

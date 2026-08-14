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
	return m.tlCache.render(m.timeline, m.expand, opts)
}

func renderEmptySession(m *model) string {
	w := m.viewport.Width
	if w < 1 {
		w = m.innerWidth()
	}
	if m.width < 1 && w < 8 {
		w = 80
	}
	var b strings.Builder
	b.WriteString(truncate(styleTitle.Render("ready"), w) + "\n")
	b.WriteString(truncate(styleDim.Render("type a message to start · ")+paintKeywords("/sessions")+styleDim.Render("  ·  ")+paintKeywords("/help"), w) + "\n")
	b.WriteString(truncate(styleMuted.Render("Tab mode  ·  e expand"), w) + "\n")
	if len(m.sessions) > 0 {
		b.WriteString("\n")
		b.WriteString(truncate(styleMuted.Render("recent"), w) + "\n")
		limit := min(5, len(m.sessions))
		for i := 0; i < limit; i++ {
			s := m.sessions[i]
			title := s.Title
			if title == "" {
				title = string(s.ID)
			}
			b.WriteString(truncate(fmt.Sprintf("  %s  %s  %s",
				shortID(string(s.ID)), stateBadge(s.LatestState), styleDim.Render(truncate(title, max(8, w-16)))), w) + "\n")
		}
	} else if len(m.tasks) > 0 {
		b.WriteString("\n")
		b.WriteString(truncate(styleMuted.Render("recent"), w) + "\n")
		limit := min(5, len(m.tasks))
		for i := 0; i < limit; i++ {
			t := m.tasks[i]
			b.WriteString(truncate(fmt.Sprintf("  %s  %s  %s",
				shortID(string(t.ID)), stateBadge(t.State), styleDim.Render(truncate(t.Title, max(8, w-16)))), w) + "\n")
		}
	}
	return b.String()
}

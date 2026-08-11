package tui

import "strings"

// timelineRenderCache avoids re-styling the full timeline when items are unchanged
// (Crush cachedMessageItem pattern, simplified for our single-string viewport).
// Finished rows use a stable fingerprint so live drafts do not invalidate them.
type timelineRenderCache struct {
	key    string
	output string
}

func (c *timelineRenderCache) render(items []timelineItem) string {
	key := timelineCacheKey(items)
	if c != nil && key == c.key && c.output != "" {
		return c.output
	}
	out := renderTimelineUncached(items)
	if c != nil {
		c.key = key
		c.output = out
	}
	return out
}

func timelineCacheKey(items []timelineItem) string {
	var b strings.Builder
	b.Grow(len(items) * 40)
	for _, it := range items {
		b.WriteString(string(it.Kind))
		b.WriteByte('|')
		b.WriteString(it.Title)
		b.WriteByte('|')
		b.WriteString(it.State)
		b.WriteByte('|')
		b.WriteString(it.At)
		b.WriteByte('|')
		// Live / unfinished rows: full body length so typewriter invalidates.
		// Finished rows: length + short prefix only (stable once transcript settles).
		if timelineItemFinished(it) {
			b.WriteString("F")
			b.WriteString(it.Body[:min(24, len(it.Body))])
			b.WriteByte('#')
			b.WriteString(itoa(len(it.Body)))
		} else {
			b.WriteString("L")
			b.WriteString(it.Body[:min(48, len(it.Body))])
			b.WriteByte('#')
			b.WriteString(itoa(len(it.Body)))
		}
		b.WriteByte(';')
	}
	return b.String()
}

// timelineItemFinished is true for settled transcript rows (not live typewriter draft).
func timelineItemFinished(it timelineItem) bool {
	if it.State == "streaming" || it.Title == "assistant…" {
		return false
	}
	if it.Kind == tlUser || it.Kind == tlPlan {
		return true
	}
	switch it.State {
	case "completed", "failed", "cancelled":
		return true
	case "running", "paused":
		return false
	default:
		// Empty state + system/error rows are treated finished.
		return it.Kind == tlSystem || it.Kind == tlError || it.State == ""
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

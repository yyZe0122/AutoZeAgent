package tui

import "strings"

// timelineRenderCache avoids re-styling the full timeline when items are unchanged
// (Crush cachedMessageItem pattern, simplified for our single-string viewport).
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
	b.Grow(len(items) * 32)
	for _, it := range items {
		b.WriteString(string(it.Kind))
		b.WriteByte('|')
		b.WriteString(it.Title)
		b.WriteByte('|')
		b.WriteString(it.State)
		b.WriteByte('|')
		b.WriteString(it.At)
		b.WriteByte('|')
		// Body may be large; use length + prefix fingerprint.
		b.WriteString(it.Body[:min(48, len(it.Body))])
		b.WriteByte('#')
		b.WriteString(itoa(len(it.Body)))
		b.WriteByte(';')
	}
	return b.String()
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

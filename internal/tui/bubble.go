package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderOpts controls bubble width and markdown theme for one paint.
type renderOpts struct {
	Width int
	Theme ThemeName
}

func defaultRenderOpts() renderOpts {
	return renderOpts{Width: 72, Theme: ThemeNight}
}

func bubbleWidth(termW int) int {
	if termW <= 0 {
		termW = 80
	}
	w := termW - 2
	if w < 36 {
		w = 36
	}
	return w
}

func cardStyle(border lipgloss.Color, width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(width)
}

// wrapBody wraps plain text to width (rune-aware, soft break on spaces).
func wrapBody(s string, width int) string {
	if width < 8 {
		width = 8
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		out = append(out, wrapLine(line, width)...)
	}
	return strings.Join(out, "\n")
}

func wrapLine(line string, width int) []string {
	runes := []rune(line)
	if len(runes) <= width {
		return []string{line}
	}
	var lines []string
	for len(runes) > width {
		cut := width
		for i := width; i > width/2; i-- {
			if runes[i-1] == ' ' {
				cut = i
				break
			}
		}
		lines = append(lines, string(runes[:cut]))
		runes = runes[cut:]
		for len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}
	if len(runes) > 0 {
		lines = append(lines, string(runes))
	}
	return lines
}

func renderBubbleCard(title, body string, border lipgloss.Color, titleStyle, bodyStyle lipgloss.Style, width int, zoneID string) string {
	innerW := width - 4
	if innerW < 12 {
		innerW = 12
	}
	head := titleStyle.Render(title)
	var content string
	if body != "" {
		if strings.Contains(body, "\x1b[") {
			content = head + "\n" + body
		} else {
			wrapped := wrapBody(body, innerW)
			var b strings.Builder
			b.WriteString(head)
			for _, ln := range strings.Split(wrapped, "\n") {
				b.WriteByte('\n')
				b.WriteString(bodyStyle.Render(ln))
			}
			content = b.String()
		}
	} else {
		content = head
	}
	card := cardStyle(border, width).Render(content)
	if zoneID != "" {
		card = zoneMark(zoneID, card)
	}
	return card
}

func renderLeftBar(body string, bar lipgloss.Color, width int, zoneID string) string {
	innerW := width - 2
	if innerW < 12 {
		innerW = 12
	}
	var content string
	if strings.Contains(body, "\x1b[") {
		content = body
	} else {
		content = wrapBody(body, innerW)
	}
	out := lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.Border{Left: "│"}).
		BorderForeground(bar).
		PaddingLeft(1).
		Width(width).
		Render(content)
	if zoneID != "" {
		out = zoneMark(zoneID, out)
	}
	return out
}

func renderPlainBlock(body string, bodyStyle lipgloss.Style, width int, zoneID string) string {
	innerW := width
	if innerW < 12 {
		innerW = 12
	}
	var content string
	if strings.Contains(body, "\x1b[") {
		content = body
	} else {
		var b strings.Builder
		for i, ln := range strings.Split(wrapBody(body, innerW), "\n") {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(bodyStyle.Render(ln))
		}
		content = b.String()
	}
	if zoneID != "" {
		content = zoneMark(zoneID, content)
	}
	return content
}

func renderDoneBanner(title string, state string, width int) string {
	if title == "" {
		title = "done"
	}
	label := title
	if state != "" && state != title {
		label += " · " + state
	}
	barW := max(8, min(width-lipgloss.Width(label)-3, 48))
	if barW < 4 {
		barW = 4
	}
	bar := styleDone.Render(strings.Repeat("─", barW))
	return bar + "  " + styleDone.Render(label)
}

func renderChip(label string) string {
	return styleMuted.Render(label)
}

func renderSystemLine(prefix, title, state string, titleStyle lipgloss.Style) string {
	line := titleStyle.Render(prefix + " " + title)
	if state != "" {
		line += "  " + stateBadge(state)
	}
	return line
}

func blockTitleThinking(lines int, folded, live bool) string {
	if live {
		return fmt.Sprintf("thinking · live · last %d lines", thinkingLiveMaxLines)
	}
	if folded {
		return fmt.Sprintf("thinking · %d lines · e expand", lines)
	}
	return "thinking"
}

func blockTitleTool(name, preview string) string {
	if name == "" {
		name = "tool"
	}
	if preview != "" {
		return "· " + name + " · " + preview
	}
	return "· " + name
}

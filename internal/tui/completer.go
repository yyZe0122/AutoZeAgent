package tui

import (
	"strings"
)

// completer filters slash commands as the user types "/…".
type completer struct {
	active  bool
	query   string
	items   []slashCommand
	cursor  int
	visible bool
}

func (c *completer) update(input string) {
	input = strings.TrimRight(input, " ")
	if !strings.HasPrefix(input, "/") {
		c.active = false
		c.visible = false
		c.items = nil
		c.cursor = 0
		c.query = ""
		return
	}
	// Only complete the command token (before first space).
	token := input
	if i := strings.IndexByte(input, ' '); i >= 0 {
		// Already has args — hide completer.
		c.active = false
		c.visible = false
		c.items = nil
		return
	}
	c.active = true
	c.query = strings.ToLower(token)
	c.items = filterCommands(c.query)
	if c.cursor >= len(c.items) {
		c.cursor = 0
	}
	c.visible = len(c.items) > 0
}

func filterCommands(query string) []slashCommand {
	if query == "" || query == "/" {
		out := make([]slashCommand, len(slashCommands))
		copy(out, slashCommands)
		return out
	}
	var out []slashCommand
	for _, cmd := range slashCommands {
		name := strings.ToLower(cmd.Name)
		if strings.HasPrefix(name, query) {
			out = append(out, cmd)
		}
	}
	// Also match aliases listed in Help when name doesn't prefix-match.
	if len(out) == 0 {
		for _, cmd := range slashCommands {
			if strings.Contains(strings.ToLower(cmd.Help), query[1:]) {
				out = append(out, cmd)
			}
		}
	}
	return out
}

func (c *completer) move(delta int) {
	if !c.visible || len(c.items) == 0 {
		return
	}
	c.cursor += delta
	if c.cursor < 0 {
		c.cursor = len(c.items) - 1
	}
	if c.cursor >= len(c.items) {
		c.cursor = 0
	}
}

// selectedName returns the highlighted command name without dismissing.
func (c *completer) selectedName() string {
	if !c.visible || len(c.items) == 0 {
		return ""
	}
	if c.cursor < 0 || c.cursor >= len(c.items) {
		return c.items[0].Name
	}
	return c.items[c.cursor].Name
}

// accept returns the selected command name (e.g. "/new") and clears the popup.
func (c *completer) accept() string {
	name := c.selectedName()
	if name == "" {
		return ""
	}
	c.active = false
	c.visible = false
	c.items = nil
	return name
}

// inputIsCompleteCommand reports whether trim(input) already names the selected
// slash command (or an alias that canonicalizes to it).
func inputIsCompleteCommand(input, selectedName string) bool {
	input = strings.TrimSpace(input)
	selectedName = strings.TrimSpace(selectedName)
	if input == "" || selectedName == "" {
		return false
	}
	return canonicalSlash(input) == canonicalSlash(selectedName)
}

func (c *completer) dismiss() {
	c.active = false
	c.visible = false
	c.items = nil
	c.cursor = 0
}

func (c *completer) render(max int) string {
	if !c.visible || len(c.items) == 0 {
		return ""
	}
	if max <= 0 {
		max = 6
	}
	start := 0
	if c.cursor >= max {
		start = c.cursor - max + 1
	}
	end := start + max
	if end > len(c.items) {
		end = len(c.items)
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		cmd := c.items[i]
		line := cmd.Name + "  " + cmd.Desc
		if i == c.cursor {
			b.WriteString(styleCompSel.Render("› " + line))
		} else {
			b.WriteString(styleComp.Render("  " + line))
		}
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

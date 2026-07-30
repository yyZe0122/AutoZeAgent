package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m model) View() string {
	if m.quitting {
		return ""
	}
	width := m.width
	if width < 40 {
		width = 80
	}

	header := m.renderHeader()
	main := m.viewport.View()
	if m.showContextPanel() {
		ctx := m.renderMetricsPanel(m.viewport.Height)
		panelW := contextPanelWidth
		mainW := max(20, width-panelW-4)
		left := styleBorder.Width(mainW).Height(m.viewport.Height + 2).Render(main)
		right := styleBorder.Width(panelW).Height(m.viewport.Height + 2).Render(ctx)
		main = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	} else {
		main = styleBorder.Width(width - 2).Render(main)
	}

	// Floating layers above the input (do not replace viewport content).
	var floatParts []string
	if ov := renderPickerOverlay(&m, width); ov != "" {
		floatParts = append(floatParts, ov)
	}
	if m.completer.visible {
		if c := m.completer.render(6); c != "" {
			floatParts = append(floatParts, stylePickerBox.Width(max(40, width-2)).Render(c))
		}
	}
	if ov := renderApprovalOverlay(&m, width); ov != "" {
		floatParts = append(floatParts, ov)
	}

	strip := m.renderContextStrip()
	status := m.renderStatusLine()
	inputBox := m.renderInputBox(width)

	parts := []string{header, main}
	parts = append(parts, floatParts...)
	parts = append(parts, strip, status, inputBox)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m model) renderHeader() string {
	left := styleTitle.Render("autozeagent") + "  " +
		styleDim.Render(string(m.mode)) + "  " +
		sseDot(m.sseState) + styleDim.Render(" "+m.sseState)
	themeHint := styleMuted.Render("  " + string(m.theme))
	taskHint := ""
	if m.task != nil {
		taskHint = "\n" + styleDim.Render("  ") + shortID(string(m.task.ID)) + " " + stateBadge(m.task.State)
	}
	return left + themeHint + taskHint
}

func (m model) renderInputBox(width int) string {
	modeColor := colorModeAgent
	modeLabel := "agent"
	modeStyle := styleModeAgent
	hint := "Tab · agent (chat + tools)"
	if m.draftMode == modePlan {
		modeColor = colorModePlan
		modeLabel = "plan"
		modeStyle = styleModePlan
		hint = "Tab · plan (plan → approve → execute)"
	}
	busy := ""
	if m.busy {
		busy = styleWarn.Render(" …")
	}
	innerW := max(20, width-4)
	m.input.Width = max(10, innerW-4)
	line1 := styleInput.Render("› ") + m.input.View() + busy
	line2 := modeStyle.Render(modeLabel+" ▸") + "  " + styleMuted.Render(hint)
	body := line1 + "\n" + line2
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(modeColor).
		Padding(0, 1).
		Width(innerW)
	return box.Render(body)
}

func (m model) renderContextStrip() string {
	width := m.width - 2
	if width < 20 {
		width = 40
	}
	parts := []string{}

	modelName := m.modelName
	if modelName == "" {
		modelName = "—"
	}
	parts = append(parts, modelName)

	cwd := m.cwd
	if cwd == "" {
		cwd = "—"
	}
	parts = append(parts, truncate(cwd, 40))

	ctxPart := "ctx —"
	if n, ok := m.metrics().ContextWindow(); ok {
		ctxPart = fmt.Sprintf("ctx %s", formatTokens(int64(n)))
	}
	parts = append(parts, ctxPart)

	active := m.runActivity() == activityActive
	wave := styleHeart.Render(heartbeatWave(active, m.animFrame))
	label := m.activityLabel()
	if active {
		if el := formatDuration(m.activityElapsed()); el != "" {
			label += " " + el
		}
	}
	parts = append(parts, wave+" "+label)

	if !m.showContextPanel() {
		if used, maxTok, ok := m.metrics().TaskTokenUsage(); ok {
			if maxTok > 0 {
				parts = append(parts, fmt.Sprintf("tok %s/%s", formatTokens(used), formatTokens(maxTok)))
			} else {
				parts = append(parts, fmt.Sprintf("tok %s", formatTokens(used)))
			}
		}
	}

	return styleStatus.Render(truncate(strings.Join(parts, "  ·  "), width))
}

func (m model) renderMetricsPanel(height int) string {
	var b strings.Builder
	b.WriteString(styleMetricsTitle.Render("Metrics") + "\n\n")

	b.WriteString(stylePanelLabel.Render("tokens") + "\n")
	if used, maxTok, ok := m.metrics().TaskTokenUsage(); ok {
		if maxTok > 0 {
			b.WriteString(fmt.Sprintf("  %s / %s\n", formatTokens(used), formatTokens(maxTok)))
		} else {
			b.WriteString(fmt.Sprintf("  %s\n", formatTokens(used)))
		}
		if m.usage.InputTokens > 0 || m.usage.OutputTokens > 0 {
			b.WriteString(styleDim.Render(fmt.Sprintf("  in %s · out %s\n",
				formatTokens(m.usage.InputTokens), formatTokens(m.usage.OutputTokens))))
		}
	} else {
		b.WriteString(styleDim.Render("  —") + "\n")
	}
	b.WriteString("\n")

	b.WriteString(stylePanelLabel.Render("cache") + "\n")
	if rate, ok := m.metrics().CacheHitRate(); ok {
		b.WriteString(fmt.Sprintf("  %.0f%%\n", rate*100))
	} else {
		b.WriteString(styleDim.Render("  —") + "\n")
	}
	b.WriteString("\n")

	b.WriteString(stylePanelLabel.Render("MCP") + "\n")
	mcp := m.metrics().MCPStatus()
	if !mcp.Enabled {
		b.WriteString(styleDim.Render("  —") + "\n")
	} else {
		b.WriteString(fmt.Sprintf("  %d ok · %d err · %d total\n", mcp.OK, mcp.Error, mcp.Total))
	}

	content := strings.TrimRight(b.String(), "\n")
	lines := strings.Split(content, "\n")
	if height > 0 && len(lines) > height {
		content = strings.Join(lines[:height], "\n")
	}
	return content
}

func (m model) renderContextPanel(height int) string {
	return m.renderMetricsPanel(height)
}

func (m model) budgetSummary() string {
	if m.prompt == nil {
		return ""
	}
	b := m.prompt.Budget
	if b.MaxTokens == 0 && b.MaxCostMicros == 0 && b.MaxDurationMillis == 0 {
		return ""
	}
	return fmt.Sprintf("tok=%d cost_µ=%d dur_ms=%d", b.MaxTokens, b.MaxCostMicros, b.MaxDurationMillis)
}

func (m model) renderStatusLine() string {
	width := m.width - 4
	if width < 20 {
		width = 40
	}
	if m.errMsg != "" {
		return styleError.Render(truncate(m.errMsg, width))
	}
	if m.statusMsg != "" {
		line := m.statusMsg
		if idx := strings.IndexByte(line, '\n'); idx >= 0 {
			line = line[:idx] + "…"
		}
		return styleStatus.Render(truncate(line, width))
	}
	return styleStatus.Render("Tab mode · / help · /theme · a/r approve · PgUp scroll")
}

// syncViewport rebuilds the main conversation content.
// force=true skips the content-equality short-circuit (resize/help/theme).
func (m *model) syncViewport(force bool) {
	var content string
	if m.helpOpen {
		content = styleHelpBox.Width(max(40, m.width-8)).Render(strings.TrimSpace(helpText()))
	} else {
		content = renderSessionView(m)
	}
	if !force && content == m.viewportContent {
		if m.stickBottom {
			m.viewport.GotoBottom()
		}
		return
	}
	m.viewportContent = content
	m.viewport.SetContent(content)
	if m.stickBottom {
		m.viewport.GotoBottom()
	}
}

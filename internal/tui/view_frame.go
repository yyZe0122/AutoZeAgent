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
	chat := m.viewport.View()
	if m.showContextPanel() {
		ctx := m.renderMetricsPanel(m.viewport.Height)
		chat = lipgloss.JoinHorizontal(lipgloss.Top,
			chat,
			lipgloss.NewStyle().Foreground(colorBorder).Render("│"),
			lipgloss.NewStyle().Width(contextPanelWidth).MaxHeight(m.viewport.Height).Render(ctx),
		)
	}

	var floatParts []string
	if m.helpOpen {
		floatParts = append(floatParts, m.renderHelpOverlay(width))
	}
	if ov := renderPickerOverlay(&m, width); ov != "" {
		floatParts = append(floatParts, ov)
	}
	if m.completer.visible {
		if c := m.completer.render(6); c != "" {
			floatParts = append(floatParts, stylePickerBox.Width(max(40, width-2)).Render(c))
		}
	}

	footer := m.renderFooter()
	inputBox := m.renderInputBox(width)

	parts := []string{header, chat}
	parts = append(parts, floatParts...)
	parts = append(parts, footer, inputBox)
	return zoneScan(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func (m model) renderHeader() string {
	parts := []string{styleTitle.Render("ymz")}
	if m.modelName != "" {
		parts = append(parts, styleMuted.Render(truncate(m.modelName, 28)))
	}
	parts = append(parts, sseDot(m.sseState))
	if m.task != nil {
		parts = append(parts, styleDim.Render(shortID(string(m.task.ID))), stateBadge(m.task.State))
	}
	return strings.Join(parts, "  ·  ")
}

func (m model) renderInputBox(width int) string {
	modeColor := colorModeAgent
	modeLabel := "agent"
	modeStyle := styleModeAgent
	if m.draftMode == modePlan {
		modeColor = colorModePlan
		modeLabel = "plan"
		modeStyle = styleModePlan
	}
	busy := ""
	if m.busy || m.runActivity() == activityActive {
		busy = " " + styleWarn.Render(m.spinner.View())
	}
	innerW := max(20, width-2)
	m.input.Width = max(10, innerW-4)
	m.input.PlaceholderStyle = styleMuted
	if strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/") {
		m.input.TextStyle = styleKeyword
	} else {
		m.input.TextStyle = styleInput
	}
	prompt := styleInput.Render("› ")
	if strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/") {
		prompt = styleKeyword.Render("› ")
	}
	line := prompt + m.input.View() + busy
	rule := lipgloss.NewStyle().Foreground(modeColor).Render(strings.Repeat("─", max(8, width)))
	meta := modeStyle.Render(modeLabel)
	if m.sessionID == "" && m.task == nil && len(m.messages) == 0 {
		meta += "  " + styleMuted.Render("Tab mode · type to start")
	}
	return rule + "\n" + line + "\n" + meta
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
		if p, okP := m.metrics().ContextPressure(); okP {
			ctxPart = fmt.Sprintf("ctx %s %d%%", formatTokens(int64(n)), int(p*100+0.5))
		}
	}
	parts = append(parts, ctxPart)

	active := m.runActivity() == activityActive
	pulse := heartbeatWave(active, m.animFrame)
	if active {
		pulse = styleHeart.Render(pulse)
	} else {
		pulse = styleMuted.Render(pulse)
	}
	label := m.activityLabel()
	if active {
		if el := formatDuration(m.activityElapsed()); el != "" {
			label += " " + el
		}
	}
	parts = append(parts, pulse+" "+label)

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

func (m model) renderFooter() string {
	width := m.width - 2
	if width < 20 {
		width = 40
	}
	if m.errMsg != "" {
		return styleError.Render(truncate(m.errMsg, width))
	}
	if m.pendingPermCount > 0 {
		line := fmt.Sprintf("%d tool permission(s) pending · 1–4 decide · /perm", m.pendingPermCount)
		if m.statusMsg != "" {
			line = m.statusMsg
			if idx := strings.IndexByte(line, '\n'); idx >= 0 {
				line = line[:idx] + "…"
			}
		}
		return styleError.Render(truncate(line, width))
	}
	if m.statusMsg != "" && m.statusMsg != "daemon ok" {
		line := m.statusMsg
		if idx := strings.IndexByte(line, '\n'); idx >= 0 {
			line = line[:idx] + "…"
		}
		return styleStatus.Render(truncate(line, width))
	}
	return m.renderContextStrip()
}

func (m model) renderStatusLine() string {
	return m.renderFooter()
}

func (m model) renderMetricsPanel(height int) string {
	var b strings.Builder
	b.WriteString(styleMetricsTitle.Render("context") + "\n\n")

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
		if m.usage.CostMicros > 0 {
			b.WriteString(styleDim.Render(fmt.Sprintf("  cost %dµ\n", m.usage.CostMicros)))
		}
		if m.runUsageOK && m.runUsage.ChildRunCount > 0 {
			b.WriteString(styleDim.Render(fmt.Sprintf("  parent %s · children %s (%d)\n",
				formatTokens(m.runUsage.Self.TotalTokens),
				formatTokens(m.runUsage.Children.TotalTokens),
				m.runUsage.ChildRunCount)))
		}
	} else {
		b.WriteString(styleDim.Render("  —") + "\n")
	}
	if p, ok := m.metrics().ContextPressure(); ok {
		b.WriteString(styleDim.Render(fmt.Sprintf("  window %d%%", int(p*100+0.5))))
		if m.contextOK && m.taskContext.LastPromptTokens > 0 {
			b.WriteString(styleDim.Render(fmt.Sprintf(" · prompt %s", formatTokens(m.taskContext.LastPromptTokens))))
		}
		if m.contextOK && m.taskContext.Compacted {
			b.WriteString(styleDim.Render(" · compacted"))
		}
		b.WriteString("\n")
	}

	if summary := m.budgetSummary(); summary != "" {
		b.WriteString("\n")
		b.WriteString(stylePanelLabel.Render("budget") + "\n")
		b.WriteString("  " + summary + "\n")
	}
	if m.dataDir != "" {
		b.WriteString("\n")
		b.WriteString(stylePanelLabel.Render("data") + "\n")
		b.WriteString("  " + truncate(m.dataDir, 36) + "\n")
	}

	if rate, ok := m.metrics().CacheHitRate(); ok {
		b.WriteString("\n")
		b.WriteString(stylePanelLabel.Render("cache") + "\n")
		b.WriteString(fmt.Sprintf("  hit %.0f%%\n", rate*100))
		if m.usage.CacheReadTokens > 0 || m.usage.CacheWriteTokens > 0 {
			b.WriteString(styleDim.Render(fmt.Sprintf("  read %s · write %s\n",
				formatTokens(m.usage.CacheReadTokens), formatTokens(m.usage.CacheWriteTokens))))
		}
	}
	if mcp := m.metrics().MCPStatus(); mcp.Enabled {
		b.WriteString("\n")
		b.WriteString(stylePanelLabel.Render("MCP") + "\n")
		line := fmt.Sprintf("  %d ok · %d err · %d total", mcp.OK, mcp.Error, mcp.Total)
		if mcp.Detail != "" {
			line += " · " + mcp.Detail
		}
		b.WriteString(line + "\n")
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
	b, ok := planBudgetOf(m.plan)
	if !ok {
		return ""
	}
	return fmt.Sprintf("tok=%d cost_µ=%d dur_ms=%d", b.MaxTokens, b.MaxCostMicros, b.MaxDurationMillis)
}

func (m model) renderHelpOverlay(width int) string {
	text := strings.TrimSpace(helpText())
	lines := strings.Split(text, "\n")
	if len(lines) > helpOverlayMax {
		lines = append(lines[:helpOverlayMax], styleMuted.Render("… Esc to close"))
		text = strings.Join(lines, "\n")
	}
	boxW := max(40, width-2)
	return styleHelpBox.Width(boxW).Render(text)
}

func (m *model) syncViewport(force bool) {
	content := renderSessionView(m)
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

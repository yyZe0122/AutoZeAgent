package tui

import (
	"fmt"
	"strings"
)

// renderPlanDetailsInline expands plan capabilities into the timeline area
// when /details is on during waiting_approval (no separate Plan tab).
func renderPlanDetailsInline(m *model) string {
	if m.prompt == nil {
		return ""
	}
	p := m.prompt
	var b strings.Builder
	b.WriteString(styleTitle.Render(fmt.Sprintf("Plan %s", shortID(string(p.PlanID)))))
	b.WriteString(styleDim.Render(fmt.Sprintf("  rev=%d", p.Revision)) + "\n")
	b.WriteString(fmt.Sprintf("objective: %s\n", p.Objective))
	b.WriteString(styleDim.Render(fmt.Sprintf("budget: tokens=%d cost_µ=%d duration_ms=%d",
		p.Budget.MaxTokens, p.Budget.MaxCostMicros, p.Budget.MaxDurationMillis)) + "\n\n")
	for _, step := range p.Steps {
		risk := string(step.Risk)
		b.WriteString(fmt.Sprintf("Step %d: %s  ", step.Position+1, step.Title))
		b.WriteString(riskStyle(risk).Render(risk))
		b.WriteString("\n")
		if len(step.Capabilities) == 0 {
			b.WriteString(styleDim.Render("  capabilities: none") + "\n")
		} else {
			for _, cap := range step.Capabilities {
				b.WriteString(fmt.Sprintf("    - %s calls=%d one_time=%t\n", cap.Tool, cap.MaxCalls, cap.OneTime))
				if len(cap.Paths) > 0 {
					b.WriteString("      paths: " + strings.Join(cap.Paths, ", ") + "\n")
				}
			}
		}
	}
	return b.String()
}

// renderApprovalOverlay is a floating approval panel above the input.
func renderApprovalOverlay(m *model, width int) string {
	if !m.waitingApproval() || !m.approvalOpen {
		return ""
	}
	p := m.prompt
	var b strings.Builder
	b.WriteString(styleWarn.Render("waiting approval") + "\n")
	if p != nil {
		b.WriteString(styleDim.Render(fmt.Sprintf("plan %s · rev %d · %d step(s)",
			shortID(string(p.PlanID)), p.Revision, len(p.Steps))) + "\n")
		if p.Objective != "" {
			b.WriteString(truncate(p.Objective, max(20, width-8)) + "\n")
		}
	}
	b.WriteString(styleDim.Render("[a] allow_plan   [r] reject   /approve …   /details   Esc hide") + "\n")
	b.WriteString(styleMuted.Render("PgUp/PgDn scroll conversation"))
	boxW := max(40, width-2)
	return styleOverlay.Width(boxW).Render(b.String())
}

// renderPlanView kept for compatibility; plan content lives in timeline + overlay.
func renderPlanView(m *model) string {
	return renderSessionView(m)
}

package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent          lipgloss.Color
	colorDim             lipgloss.Color
	colorOK              lipgloss.Color
	colorWarn            lipgloss.Color
	colorErr             lipgloss.Color
	colorMuted           lipgloss.Color
	colorBorder          lipgloss.Color
	colorHeart           lipgloss.Color
	colorTitle           lipgloss.Color
	colorInput           lipgloss.Color
	colorSurface         lipgloss.Color
	colorModeAgent       lipgloss.Color
	colorModePlan        lipgloss.Color
	colorBubbleUser      lipgloss.Color
	colorBubbleAssistant lipgloss.Color
	colorBubbleThinking  lipgloss.Color
	colorBubbleTool      lipgloss.Color
	colorKeyword         lipgloss.Color

	styleTitle        lipgloss.Style
	styleDim          lipgloss.Style
	styleMuted        lipgloss.Style
	styleError        lipgloss.Style
	styleOK           lipgloss.Style
	styleWarn         lipgloss.Style
	styleBadge        lipgloss.Style
	styleStatus       lipgloss.Style
	styleInput        lipgloss.Style
	styleKeyword      lipgloss.Style
	styleCompSel      lipgloss.Style
	styleComp         lipgloss.Style
	styleHelpBox      lipgloss.Style
	styleRiskHi       lipgloss.Style
	styleRiskMed      lipgloss.Style
	styleRiskLo       lipgloss.Style
	styleTLUser       lipgloss.Style
	styleTLSys        lipgloss.Style
	styleTLPlan       lipgloss.Style
	styleTLRun        lipgloss.Style
	styleTLErr        lipgloss.Style
	styleTLTool       lipgloss.Style
	styleTLThinking   lipgloss.Style
	styleTLReply      lipgloss.Style
	styleTLBody       lipgloss.Style
	styleTLJourney    lipgloss.Style
	styleDone         lipgloss.Style
	styleHeart        lipgloss.Style
	styleMetricsTitle lipgloss.Style
	stylePanelLabel   lipgloss.Style
	styleModeAgent    lipgloss.Style
	styleModePlan     lipgloss.Style
	stylePickerBox    lipgloss.Style
)

func init() {
	applyTheme(nightTheme)
}

func sseDot(state string) string {
	switch state {
	case "ok":
		return styleOK.Render("●")
	case "reconnecting", "connecting":
		return styleWarn.Render("○")
	default:
		return styleError.Render("●")
	}
}

func stateBadge(state string) string {
	switch state {
	case "completed", "approved":
		return styleOK.Render(state)
	case "failed", "cancelled":
		return styleError.Render(state)
	case "paused":
		return styleWarn.Render(state)
	case "running":
		return styleBadge.Render(state)
	default:
		return styleDim.Render(state)
	}
}

func riskStyle(risk string) lipgloss.Style {
	switch risk {
	case "high", "critical":
		return styleRiskHi
	case "medium", "moderate":
		return styleRiskMed
	default:
		return styleRiskLo
	}
}

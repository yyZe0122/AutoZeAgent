package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"

	"github.com/yyZe0122/yunmengze-agent/internal/platform/paths"
)

type ThemeName string

const (
	ThemeDay   ThemeName = "day"
	ThemeNight ThemeName = "night"

	tuiPrefsFilename = "tui.json"
	defaultTheme     = ThemeNight
)

// Theme is a named lipgloss palette for TUI chrome.
type Theme struct {
	Name ThemeName

	Accent    lipgloss.Color
	Dim       lipgloss.Color
	Muted     lipgloss.Color
	Border    lipgloss.Color
	OK        lipgloss.Color
	Warn      lipgloss.Color
	Err       lipgloss.Color
	Input     lipgloss.Color
	Title     lipgloss.Color
	Surface   lipgloss.Color
	Heart     lipgloss.Color
	ModeAgent lipgloss.Color
	ModePlan  lipgloss.Color
	Thinking  lipgloss.Color
	Tool      lipgloss.Color
	Reply     lipgloss.Color
	Journey   lipgloss.Color
	Done      lipgloss.Color
	Keyword   lipgloss.Color
}

// Day: 泽昼 — moon-white paper, deep mist-teal, cinnabar seal.
var dayTheme = Theme{
	Name:      ThemeDay,
	Accent:    lipgloss.Color("#2F6B62"),
	Dim:       lipgloss.Color("#4A4F48"),
	Muted:     lipgloss.Color("#7A8078"),
	Border:    lipgloss.Color("#C4C8BC"),
	OK:        lipgloss.Color("#2A5A40"),
	Warn:      lipgloss.Color("#8A6418"),
	Err:       lipgloss.Color("#8B3A30"),
	Input:     lipgloss.Color("#161814"),
	Title:     lipgloss.Color("#161814"),
	Surface:   lipgloss.Color("#E6E8E2"),
	Heart:     lipgloss.Color("#B8322C"),
	ModeAgent: lipgloss.Color("#2F6B62"),
	ModePlan:  lipgloss.Color("#8A6418"),
	Thinking:  lipgloss.Color("#5A5C56"),
	Tool:      lipgloss.Color("#3A524E"),
	Reply:     lipgloss.Color("#161814"),
	Journey:   lipgloss.Color("#4A444C"),
	Done:      lipgloss.Color("#1A7A58"),
	Keyword:   lipgloss.Color("#A56B12"),
}

// Night: 泽夜 — xuan ink, mist teal, reed gold; Heart/Done are the seal.
var nightTheme = Theme{
	Name:      ThemeNight,
	Accent:    lipgloss.Color("#9EC9B8"),
	Dim:       lipgloss.Color("#B4BAAF"),
	Muted:     lipgloss.Color("#7A8278"),
	Border:    lipgloss.Color("#2C3228"),
	OK:        lipgloss.Color("#7AAD90"),
	Warn:      lipgloss.Color("#D4B46A"),
	Err:       lipgloss.Color("#D07060"),
	Input:     lipgloss.Color("#F4F5EE"),
	Title:     lipgloss.Color("#F4F5EE"),
	Surface:   lipgloss.Color("#121410"),
	Heart:     lipgloss.Color("#C73E3A"),
	ModeAgent: lipgloss.Color("#9EC9B8"),
	ModePlan:  lipgloss.Color("#D4B46A"),
	Thinking:  lipgloss.Color("#8A9094"),
	Tool:      lipgloss.Color("#8AA098"),
	Reply:     lipgloss.Color("#F4F5EE"),
	Journey:   lipgloss.Color("#9A8E9C"),
	Done:      lipgloss.Color("#3AA88A"),
	Keyword:   lipgloss.Color("#F0D78A"),
}

type tuiPrefs struct {
	Theme ThemeName `json:"theme"`
}

func themeByName(name ThemeName) Theme {
	switch name {
	case ThemeDay:
		return dayTheme
	case ThemeNight:
		return nightTheme
	default:
		return nightTheme
	}
}

func toggleTheme(name ThemeName) ThemeName {
	if name == ThemeDay {
		return ThemeNight
	}
	return ThemeDay
}

func applyTheme(t Theme) {
	colorAccent = t.Accent
	colorDim = t.Dim
	colorOK = t.OK
	colorWarn = t.Warn
	colorErr = t.Err
	colorMuted = t.Muted
	colorBorder = t.Border
	colorHeart = t.Heart
	colorTitle = t.Title
	colorInput = t.Input
	colorSurface = t.Surface
	colorModeAgent = t.ModeAgent
	colorModePlan = t.ModePlan
	colorBubbleUser = t.Accent
	colorBubbleAssistant = t.OK
	colorBubbleThinking = t.Thinking
	colorBubbleTool = t.Tool
	colorKeyword = t.Keyword

	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(colorTitle)
	styleDim = lipgloss.NewStyle().Foreground(colorDim)
	styleMuted = lipgloss.NewStyle().Foreground(colorMuted)
	styleError = lipgloss.NewStyle().Foreground(colorErr)
	styleOK = lipgloss.NewStyle().Foreground(colorOK)
	styleWarn = lipgloss.NewStyle().Foreground(colorWarn)
	styleBadge = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleStatus = lipgloss.NewStyle().Foreground(colorDim)
	styleInput = lipgloss.NewStyle().Foreground(colorInput)
	styleKeyword = lipgloss.NewStyle().Foreground(colorKeyword).Bold(true)
	styleCompSel = lipgloss.NewStyle().Foreground(colorKeyword).Bold(true)
	styleComp = lipgloss.NewStyle().Foreground(colorDim)
	styleHelpBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Background(colorSurface).Padding(0, 1)
	styleRiskHi = lipgloss.NewStyle().Foreground(colorErr).Bold(true)
	styleRiskMed = lipgloss.NewStyle().Foreground(colorWarn)
	styleRiskLo = lipgloss.NewStyle().Foreground(colorOK)
	styleTLUser = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleTLSys = lipgloss.NewStyle().Foreground(colorDim)
	styleTLPlan = lipgloss.NewStyle().Foreground(colorWarn)
	styleTLRun = lipgloss.NewStyle().Foreground(colorOK)
	styleTLErr = lipgloss.NewStyle().Foreground(colorErr)
	styleTLTool = lipgloss.NewStyle().Foreground(t.Tool)
	styleTLThinking = lipgloss.NewStyle().Foreground(t.Thinking)
	styleTLReply = lipgloss.NewStyle().Foreground(t.Reply)
	styleTLBody = lipgloss.NewStyle().Foreground(colorDim)
	styleTLJourney = lipgloss.NewStyle().Foreground(t.Journey)
	styleDone = lipgloss.NewStyle().Foreground(t.Done).Bold(true)
	styleHeart = lipgloss.NewStyle().Foreground(colorHeart)
	styleMetricsTitle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	stylePanelLabel = lipgloss.NewStyle().Foreground(colorMuted)
	styleModeAgent = lipgloss.NewStyle().Foreground(colorModeAgent).Bold(true)
	styleModePlan = lipgloss.NewStyle().Foreground(colorModePlan).Bold(true)
	stylePickerBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Background(colorSurface).Padding(0, 1)
}

func tuiPrefsPath(mode paths.Mode) (string, error) {
	layout, err := paths.Resolve(mode)
	if err != nil {
		return "", err
	}
	return filepath.Join(layout.ConfigDir, tuiPrefsFilename), nil
}

func loadTheme(mode paths.Mode) ThemeName {
	path, err := tuiPrefsPath(mode)
	if err != nil {
		return defaultTheme
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return defaultTheme
	}
	var prefs tuiPrefs
	if err := json.Unmarshal(raw, &prefs); err != nil {
		return defaultTheme
	}
	switch prefs.Theme {
	case ThemeDay, ThemeNight:
		return prefs.Theme
	default:
		return defaultTheme
	}
}

func saveTheme(mode paths.Mode, name ThemeName) error {
	if name != ThemeDay && name != ThemeNight {
		return fmt.Errorf("unknown theme %q", name)
	}
	path, err := tuiPrefsPath(mode)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(tuiPrefs{Theme: name}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

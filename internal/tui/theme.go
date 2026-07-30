package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"

	"autozeagent.local/autozeagent/internal/platform/paths"
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
	ModeAgent lipgloss.Color // input border / chip for agent (build) mode
	ModePlan  lipgloss.Color // input border / chip for plan mode
}

// Day: gold + marble.
var dayTheme = Theme{
	Name:      ThemeDay,
	Accent:    lipgloss.Color("#C9A227"),
	Dim:       lipgloss.Color("#6B5E4E"),
	Muted:     lipgloss.Color("#A89880"),
	Border:    lipgloss.Color("#C4B8A8"),
	OK:        lipgloss.Color("#2F6B4F"),
	Warn:      lipgloss.Color("#B8860B"),
	Err:       lipgloss.Color("#A63D2F"),
	Input:     lipgloss.Color("#2C2416"),
	Title:     lipgloss.Color("#3D2E1A"),
	Surface:   lipgloss.Color("#F5F0E6"),
	Heart:     lipgloss.Color("#E85A8C"),
	ModeAgent: lipgloss.Color("#2F6B4F"),
	ModePlan:  lipgloss.Color("#C9A227"),
}

// Night: near-white accent + dark separators.
var nightTheme = Theme{
	Name:      ThemeNight,
	Accent:    lipgloss.Color("#E8E8E8"),
	Dim:       lipgloss.Color("#9A9A9A"),
	Muted:     lipgloss.Color("#5A5A5A"),
	Border:    lipgloss.Color("#3A3A3A"),
	OK:        lipgloss.Color("#5ECF8A"),
	Warn:      lipgloss.Color("#E0B040"),
	Err:       lipgloss.Color("#FF6B6B"),
	Input:     lipgloss.Color("#F0F0F0"),
	Title:     lipgloss.Color("#FFFFFF"),
	Surface:   lipgloss.Color("#121212"),
	Heart:     lipgloss.Color("#FF6B9D"),
	ModeAgent: lipgloss.Color("#3DDC97"),
	ModePlan:  lipgloss.Color("#E0B040"),
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

	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(colorTitle)
	styleDim = lipgloss.NewStyle().Foreground(colorDim)
	styleMuted = lipgloss.NewStyle().Foreground(colorMuted)
	styleTabOn = lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Underline(true)
	styleTabOff = lipgloss.NewStyle().Foreground(colorDim)
	styleError = lipgloss.NewStyle().Foreground(colorErr)
	styleOK = lipgloss.NewStyle().Foreground(colorOK)
	styleWarn = lipgloss.NewStyle().Foreground(colorWarn)
	styleBadge = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleHeader = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(colorTitle)
	styleBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(0, 1)
	styleStatus = lipgloss.NewStyle().Foreground(colorDim)
	styleInput = lipgloss.NewStyle().Foreground(colorInput)
	styleCompSel = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleComp = lipgloss.NewStyle().Foreground(colorDim)
	styleHelpBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Padding(0, 1)
	styleRiskHi = lipgloss.NewStyle().Foreground(colorErr).Bold(true)
	styleRiskMed = lipgloss.NewStyle().Foreground(colorWarn)
	styleRiskLo = lipgloss.NewStyle().Foreground(colorOK)
	styleOverlay = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorWarn).Padding(0, 1)
	styleTLUser = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleTLSys = lipgloss.NewStyle().Foreground(colorDim)
	styleTLPlan = lipgloss.NewStyle().Foreground(colorWarn)
	styleTLRun = lipgloss.NewStyle().Foreground(colorOK)
	styleTLErr = lipgloss.NewStyle().Foreground(colorErr)
	styleHeart = lipgloss.NewStyle().Foreground(colorHeart)
	styleMetricsTitle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	stylePanelLabel = lipgloss.NewStyle().Foreground(colorMuted)
	styleModeAgent = lipgloss.NewStyle().Foreground(colorModeAgent).Bold(true)
	styleModePlan = lipgloss.NewStyle().Foreground(colorModePlan).Bold(true)
	stylePickerBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Padding(0, 1)
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

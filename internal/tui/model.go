package tui

import (
	"os"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/yyZe0122/yunmengze-agent/internal/gatewayclient"
	"github.com/yyZe0122/yunmengze-agent/internal/modelstream"
	"github.com/yyZe0122/yunmengze-agent/internal/platform/paths"
	"github.com/yyZe0122/yunmengze-agent/pkg/eventapi"
	"github.com/yyZe0122/yunmengze-agent/pkg/schedulerapi"
)

// execMode is the draft permission posture for the next task submission
// (OpenCode-style Build/Plan). Persisted on the task as execution_mode.
type execMode string

const (
	modeAgent execMode = "agent"
	modePlan  execMode = "plan"
)

// listKind is a floating picker overlay kind (does not replace the timeline).
type listKind int

const (
	listNone     listKind = iota
	listSessions          // true sessions (chat containers)
	listTasks             // task list (debug / focus)
	listModels
	listJobs
	listSkills      // multi-select skill picker for next submit
	listPermissions // pending tool-call permissions (ADR-043)
)

const (
	runPollInterval   = 2 * time.Second
	permPollInterval  = 2 * time.Second
	animInterval      = 400 * time.Millisecond
	commandTimeout    = 30 * time.Second
	historyLimit      = 50
	contextBreakWidth = 100
	contextPanelWidth = 30
	overlayMaxLines   = 10
	// timeline body defaults (fold large run results).
	timelineBodyMaxLines = 12
	timelineBodyMaxChars = 2400
	// permOpenGrace is how long decision hotkeys are ignored after auto-open.
	permOpenGrace = 400 * time.Millisecond
)

type model struct {
	mode    paths.Mode
	gateway Gateway

	width  int
	height int
	theme  ThemeName
	// draftMode is the Tab-selected mode for the next /new submission.
	draftMode execMode

	input     textinput.Model
	viewport  viewport.Model
	completer completer

	statusMsg string
	errMsg    string
	helpOpen  bool
	sseState  string
	modelName string
	models    []string
	cwd       string
	dataDir   string
	busy      bool

	// Floating picker (sessions/models/jobs/skills). Slash completer is separate.
	list        listKind
	selectedIdx int

	tasks       []gatewayclient.Task
	sessions    []gatewayclient.Session
	jobs        []schedulerapi.Job
	skills      []gatewayclient.Skill
	permissions []gatewayclient.Permission
	// selectedSkillIDs is draft selection for the next task submit (kept across turns).
	selectedSkillIDs []string
	sessionID        gatewayclient.SessionID
	task             *gatewayclient.Task
	planID           gatewayclient.PlanID
	plan             *gatewayclient.Plan
	runs             []gatewayclient.Run
	messages         []gatewayclient.TranscriptMessage
	// live assistant draft from model-stream (typewriter); cleared on complete/refresh.
	liveContent  string
	liveThinking string
	liveRunID    gatewayclient.RunID
	timeline     []timelineItem
	tlCache      timelineRenderCache
	stickBottom  bool

	usage         gatewayclient.TaskUsage
	usageOK       bool
	runUsage      gatewayclient.RunUsage
	runUsageOK    bool
	taskContext   gatewayclient.TaskContext
	contextOK     bool
	mcpStatus     gatewayclient.MCPStatus
	mcpOK         bool
	contextWindow int64 // model context length from ModelConfig; 0 = unknown

	pendingPermCount int
	lastPermPoll     time.Time
	// autoOpenedPermList avoids re-opening the picker every poll while pending remains.
	autoOpenedPermList bool
	// permGraceUntil ignores decision keys briefly after auto-open (Crush-style).
	permGraceUntil time.Time
	// permCycleIdx cycles Enter: once → similar → permanent → deny.
	permCycleIdx int

	animFrame int
	animOn    bool

	history    []string
	historyIdx int // -1 = live input

	dirty           bool
	refreshing      bool
	refreshGen      uint64
	pendingRefresh  bool
	lastRunPoll     time.Time
	viewportContent string
	sseAfter        uint64
	quitting        bool
}

type tickMsg time.Time

// refreshKind selects which gateway slices to reload.
type refreshKind int

const (
	refreshFull refreshKind = iota
	refreshTask
	refreshRuns
	refreshPlan
)

type refreshDoneMsg struct {
	gen         uint64
	kind        refreshKind
	tasks       []gatewayclient.Task
	sessions    []gatewayclient.Session
	task        *gatewayclient.Task
	plan        *gatewayclient.Plan
	runs        []gatewayclient.Run
	messages    []gatewayclient.TranscriptMessage
	usage       gatewayclient.TaskUsage
	usageOK     bool
	runUsage    gatewayclient.RunUsage
	runUsageOK  bool
	taskContext gatewayclient.TaskContext
	contextOK   bool
	err         error
}

type statusDoneMsg struct {
	health gatewayclient.Health
	model  gatewayclient.ModelConfig
	mcp    gatewayclient.MCPStatus
	mcpOK  bool
	err    error
}

type commandDoneMsg struct {
	status        string
	err           error
	taskID        gatewayclient.TaskID
	planID        gatewayclient.PlanID
	sessionID     gatewayclient.SessionID
	clearTask     bool
	openList      listKind
	closeList     bool
	quit          bool
	help          bool
	toggleTheme   bool
	modelName     string
	models        []string
	contextWindow int64
	jobs          []schedulerapi.Job
	sessions      []gatewayclient.Session
	skills        []gatewayclient.Skill
	skillIDs      []string // replace selectedSkillIDs when set (toggle apply)
	permissions   []gatewayclient.Permission
}

// permPollDoneMsg is a background poll of pending tool permissions.
type permPollDoneMsg struct {
	permissions []gatewayclient.Permission
	err         error
	openList    bool // true when count went 0→N and list should auto-open once
}

type sseEventMsg struct {
	envelope eventapi.Envelope
}

type sseStateMsg struct {
	state string
	err   error
}

type modelStreamMsg struct {
	env modelstream.Envelope
}

func newModel(mode paths.Mode, gateway Gateway) model {
	ti := textinput.New()
	ti.Placeholder = "message or /command"
	ti.Focus()
	ti.CharLimit = 4000
	ti.Width = 80
	ti.Prompt = ""
	vp := viewport.New(80, 20)
	cwd, _ := os.Getwd()
	theme := loadTheme(mode)
	applyTheme(themeByName(theme))
	m := model{
		mode: mode, gateway: gateway, theme: theme, draftMode: modeAgent,
		input: ti, viewport: vp, sseState: "connecting", dirty: true,
		stickBottom: true, historyIdx: -1, cwd: cwd,
	}
	m.applyPlaceholder()
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.loadStatusCmd(), m.scheduleRefresh(refreshFull))
}

func tickCmd() tea.Cmd {
	return tea.Tick(animInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// scheduleRefresh coalesces concurrent refreshes (Crush-style in-flight gate).
func (m *model) scheduleRefresh(kind refreshKind) tea.Cmd {
	if m.refreshing {
		m.pendingRefresh = true
		m.dirty = true
		return nil
	}
	m.refreshing = true
	m.dirty = false
	m.pendingRefresh = false
	m.refreshGen++
	gen := m.refreshGen
	return m.refreshCmd(gen, kind)
}

func (m *model) wantsAnim() bool {
	return m.runActivity() == activityActive
}

func (m *model) pickerOpen() bool {
	return m.list != listNone || m.completer.visible
}

func (m *model) toggleDraftMode() {
	if m.draftMode == modePlan {
		m.draftMode = modeAgent
	} else {
		m.draftMode = modePlan
	}
	m.applyPlaceholder()
}

func (m *model) applyPlaceholder() {
	if m.draftMode == modePlan {
		m.input.Placeholder = "plan mode · read-only analysis (no edits)"
	} else {
		m.input.Placeholder = "agent mode · build (read+write) · message or /command"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

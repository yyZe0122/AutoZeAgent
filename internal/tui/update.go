package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"autozeagent.local/autozeagent/internal/gatewayclient"
	"autozeagent.local/autozeagent/internal/modelstream"
	"autozeagent.local/autozeagent/pkg/eventapi"
	"autozeagent.local/autozeagent/pkg/providerapi"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.syncViewport(true)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		m.animFrame++
		var cmds []tea.Cmd
		if m.wantsAnim() {
			m.animOn = true
			cmds = append(cmds, tickCmd())
		} else {
			m.animOn = false
		}
		if m.dirty && !m.refreshing {
			cmds = append(cmds, m.scheduleRefresh(refreshFull))
		} else if !m.refreshing && m.needsRunPoll() && time.Since(m.lastRunPoll) >= runPollInterval {
			cmds = append(cmds, m.scheduleRefresh(refreshFull))
		}
		return m, tea.Batch(cmds...)

	case refreshDoneMsg:
		return m.applyRefresh(msg)

	case statusDoneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			if msg.model.Model != "" {
				m.modelName = msg.model.Model
			}
			m.models = msg.model.Models
			if msg.health.Core.Runtime.DataDir != "" {
				m.dataDir = msg.health.Core.Runtime.DataDir
			}
			if msg.health.OK && m.statusMsg == "" {
				m.statusMsg = "daemon ok"
			}
		}
		return m, nil

	case commandDoneMsg:
		return m.applyCommand(msg)

	case sseEventMsg:
		return m.applySSE(msg.envelope)

	case modelStreamMsg:
		return m.applyModelStream(msg.env)

	case sseStateMsg:
		var cmd tea.Cmd
		if msg.state != "" {
			prev := m.sseState
			m.sseState = msg.state
			if msg.state == "ok" && prev == "reconnecting" {
				cmd = m.scheduleRefresh(refreshFull)
			}
		}
		return m, cmd

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.stickBottom = m.viewport.AtBottom()
		return m, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.completer.update(m.input.Value())
	return m, cmd
}

func (m model) applyRefresh(msg refreshDoneMsg) (tea.Model, tea.Cmd) {
	if msg.gen != 0 && msg.gen != m.refreshGen {
		// Stale response from a superseded refresh.
		return m, nil
	}
	m.refreshing = false
	m.busy = false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
	} else {
		m.errMsg = ""
		if msg.tasks != nil {
			m.tasks = msg.tasks
		}
		if msg.sessions != nil {
			m.sessions = msg.sessions
		}
		if msg.task != nil {
			m.task = msg.task
			if msg.task.SessionID != nil && *msg.task.SessionID != "" {
				m.sessionID = *msg.task.SessionID
			}
		}
		// Full refresh replaces optional slices (including nil = none).
		// Partial kinds only patch when the fan-out produced a value.
		switch msg.kind {
		case refreshFull:
			m.plan = msg.plan
			if msg.plan != nil {
				m.planID = msg.plan.ID
			}
			m.prompt = msg.prompt
			m.runs = msg.runs
			if msg.messages != nil {
				m.messages = msg.messages
			}
		default:
			if msg.plan != nil {
				m.plan = msg.plan
				m.planID = msg.plan.ID
			}
			if msg.prompt != nil {
				m.prompt = msg.prompt
			}
			if msg.runs != nil {
				m.runs = msg.runs
			}
			if msg.messages != nil {
				m.messages = msg.messages
			}
		}
		if msg.usageOK {
			m.usage = msg.usage
			m.usageOK = true
		}
		// Authoritative transcript clears live typewriter draft.
		if msg.messages != nil {
			m.liveContent = ""
			m.liveThinking = ""
			m.liveRunID = ""
		}
		m.timeline = buildChatTimeline(m.messages, m.task, m.plan, m.prompt, m.runs)
		if m.liveContent != "" || m.liveThinking != "" {
			m.timeline = appendLiveDraft(m.timeline, m.liveThinking, m.liveContent)
		}
		m.lastRunPoll = time.Now()
		if n := m.listLen(); n > 0 && m.selectedIdx >= n {
			m.selectedIdx = n - 1
		}
		if m.waitingApproval() {
			m.approvalOpen = true
			m.statusMsg = "waiting approval — a allow · r reject · Esc hide panel"
		}
	}
	m.layout()
	m.syncViewport(false)

	var cmds []tea.Cmd
	if m.pendingRefresh || m.dirty {
		m.pendingRefresh = false
		m.dirty = false
		cmds = append(cmds, m.scheduleRefresh(refreshFull))
	}
	if m.wantsAnim() && !m.animOn {
		m.animOn = true
		cmds = append(cmds, tickCmd())
	}
	return m, tea.Batch(cmds...)
}

func (m model) applyCommand(msg commandDoneMsg) (tea.Model, tea.Cmd) {
	m.busy = false
	if msg.quit {
		m.quitting = true
		return m, tea.Quit
	}
	if msg.help {
		m.helpOpen = true
		m.statusMsg = ""
		m.errMsg = ""
		m.syncViewport(true)
		return m, nil
	}
	if msg.toggleDetails {
		m.planDetails = !m.planDetails
		m.statusMsg = "plan details " + map[bool]string{true: "on", false: "off"}[m.planDetails]
		m.syncViewport(true)
		return m, nil
	}
	if msg.toggleTheme {
		next := toggleTheme(m.theme)
		if err := saveTheme(m.mode, next); err != nil {
			m.errMsg = err.Error()
		} else {
			m.theme = next
			applyTheme(themeByName(next))
			m.statusMsg = "theme " + string(next)
			m.errMsg = ""
		}
		m.syncViewport(true)
		return m, nil
	}
	if msg.modelName != "" {
		m.modelName = msg.modelName
	}
	if msg.models != nil {
		m.models = msg.models
	}
	if msg.jobs != nil {
		m.jobs = msg.jobs
	}
	if msg.clearTask {
		m.sessionID = ""
		m.task = nil
		m.plan = nil
		m.planID = ""
		m.prompt = nil
		m.runs = nil
		m.messages = nil
		m.timeline = nil
		m.tlCache = timelineRenderCache{}
		m.usage = gatewayclient.TaskUsage{}
		m.usageOK = false
		m.approvalOpen = false
		m.viewportContent = ""
	}
	if msg.sessions != nil {
		m.sessions = msg.sessions
	}
	if msg.closeList {
		m.closeList()
	}
	if msg.openList != listNone {
		m.openList(msg.openList)
	}
	needRefresh := false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		if msg.status != "" {
			m.statusMsg = msg.status
		} else {
			m.statusMsg = "request failed"
		}
		// Keep optimistic transcript so the user sees what failed; clear only the
		// placeholder task id so refresh does not focus a non-existent task.
		if m.task != nil && m.task.ID == "…" {
			m.task = nil
			m.timeline = buildChatTimeline(m.messages, nil, m.plan, m.prompt, m.runs)
		}
	} else {
		m.errMsg = ""
		if msg.status != "" {
			m.statusMsg = msg.status
		}
		if msg.sessionID != "" {
			m.sessionID = msg.sessionID
			needRefresh = true
		}
		if msg.taskID != "" {
			id := msg.taskID
			// Keep optimistic objective if we already focused this task/placeholder.
			defaultState := gatewayclient.TaskStateRunning
			if m.draftMode == modePlan {
				defaultState = gatewayclient.TaskStatePlanning
			}
			if m.task == nil || (m.task.ID != id && m.task.ID != "…") {
				m.task = &gatewayclient.Task{ID: id, State: defaultState, ExecutionMode: string(m.draftMode)}
			} else {
				m.task.ID = id
				if m.task.State == "" {
					m.task.State = defaultState
				}
			}
			if msg.sessionID != "" {
				sid := msg.sessionID
				m.task.SessionID = &sid
			}
			m.closeList()
			needRefresh = true
		}
		if msg.planID != "" {
			m.planID = msg.planID
			needRefresh = true
		}
		if msg.openList == listSessions || msg.openList == listTasks {
			needRefresh = true
		}
	}
	m.layout()
	m.syncViewport(false)
	if needRefresh {
		return m, m.scheduleRefresh(refreshFull)
	}
	return m, nil
}

func (m model) applyModelStream(env modelstream.Envelope) (tea.Model, tea.Cmd) {
	// Ignore streams for other sessions when focused.
	if m.sessionID != "" && env.SessionID != "" && gatewayclient.SessionID(env.SessionID) != m.sessionID {
		return m, nil
	}
	if env.RunID != "" {
		m.liveRunID = gatewayclient.RunID(env.RunID)
	}
	switch env.Event.Type {
	case providerapi.StreamDelta:
		m.liveContent += env.Event.ContentDelta
	case providerapi.StreamThinking:
		m.liveThinking += env.Event.ThinkingDelta
	case providerapi.StreamToolCall:
		if env.Event.ToolCall != nil {
			m.liveContent += fmt.Sprintf("\n⚙ %s(%s)", env.Event.ToolCall.Name, truncate(env.Event.ToolCall.Arguments, 80))
		}
	case providerapi.StreamComplete:
		m.liveContent = ""
		m.liveThinking = ""
		m.liveRunID = ""
		return m, m.scheduleRefresh(refreshFull)
	default:
		if env.Event.ContentDelta != "" {
			m.liveContent += env.Event.ContentDelta
		}
		if env.Event.ThinkingDelta != "" {
			m.liveThinking += env.Event.ThinkingDelta
		}
	}
	m.timeline = buildChatTimeline(m.messages, m.task, m.plan, m.prompt, m.runs)
	m.timeline = appendLiveDraft(m.timeline, m.liveThinking, m.liveContent)
	m.syncViewport(true)
	return m, nil
}

func (m model) applySSE(envelope eventapi.Envelope) (tea.Model, tea.Cmd) {
	if envelope.Sequence > m.sseAfter {
		m.sseAfter = envelope.Sequence
	}
	typ := envelope.Type
	var kind refreshKind
	switch {
	case strings.HasPrefix(typ, "run."):
		kind = refreshRuns
	case strings.HasPrefix(typ, "plan."), strings.HasPrefix(typ, "approval."):
		kind = refreshPlan
	case strings.HasPrefix(typ, "task."):
		kind = refreshTask
	default:
		return m, nil
	}
	// Prefer immediate coalesced refresh over waiting for tick.
	return m, m.scheduleRefresh(kind)
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Approval shortcuts when input empty and no picker competing.
	if m.waitingApproval() && strings.TrimSpace(m.input.Value()) == "" && !m.completer.visible && m.list == listNone {
		switch msg.String() {
		case "a":
			m.busy = true
			m.approvalOpen = true
			return m, m.approveCmd(string(gatewayclient.ActionAllowPlan))
		case "r":
			m.busy = true
			m.approvalOpen = true
			return m, m.approveCmd(string(gatewayclient.ActionReject))
		}
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		m.input.SetValue("")
		m.errMsg = ""
		m.statusMsg = "use /quit to exit"
		m.historyIdx = -1
		m.completer.update("")
		return m, nil

	case tea.KeyEsc:
		if m.helpOpen {
			m.helpOpen = false
			m.syncViewport(true)
			return m, nil
		}
		if m.completer.visible {
			m.completer.dismiss()
			m.layout()
			return m, nil
		}
		if m.list != listNone {
			m.closeList()
			m.layout()
			return m, nil
		}
		if m.approvalOpen {
			m.approvalOpen = false
			m.layout()
			return m, nil
		}
		m.input.SetValue("")
		m.errMsg = ""
		m.historyIdx = -1
		m.completer.update("")
		return m, nil

	case tea.KeyTab:
		if m.completer.visible {
			if name := m.completer.accept(); name != "" {
				m.input.SetValue(name + " ")
				m.input.CursorEnd()
				m.completer.update(m.input.Value())
			}
			m.layout()
			return m, nil
		}
		// Tab toggles agent/plan permission mode (OpenCode-style).
		m.toggleDraftMode()
		m.statusMsg = "mode " + string(m.draftMode)
		return m, nil

	case tea.KeyShiftTab:
		m.toggleDraftMode()
		m.statusMsg = "mode " + string(m.draftMode)
		return m, nil

	case tea.KeyPgUp:
		// Always scroll conversation — pickers do not steal this.
		m.viewport.LineUp(5)
		m.stickBottom = m.viewport.AtBottom()
		return m, nil

	case tea.KeyPgDown:
		m.viewport.LineDown(5)
		m.stickBottom = m.viewport.AtBottom()
		return m, nil

	case tea.KeyUp:
		if m.completer.visible {
			m.completer.move(-1)
			return m, nil
		}
		if m.inListMode() {
			if m.selectedIdx > 0 {
				m.selectedIdx--
			}
			return m, nil
		}
		if strings.TrimSpace(m.input.Value()) == "" || m.historyIdx >= 0 {
			m.historyPrev()
			return m, nil
		}
		m.viewport.LineUp(1)
		m.stickBottom = false
		return m, nil

	case tea.KeyDown:
		if m.completer.visible {
			m.completer.move(1)
			return m, nil
		}
		if m.inListMode() {
			if m.selectedIdx < m.listLen()-1 {
				m.selectedIdx++
			}
			return m, nil
		}
		if m.historyIdx >= 0 {
			m.historyNext()
			return m, nil
		}
		m.viewport.LineDown(1)
		m.stickBottom = m.viewport.AtBottom()
		return m, nil

	case tea.KeyEnter:
		// Two-stage slash completer: first Enter completes prefix → full name;
		// second Enter (or Enter when already complete) executes.
		if m.completer.visible {
			name := m.completer.selectedName()
			if name != "" {
				typed := strings.TrimSpace(m.input.Value())
				if !inputIsCompleteCommand(typed, name) {
					m.completer.dismiss()
					m.input.SetValue(name)
					m.input.CursorEnd()
					m.historyIdx = -1
					m.completer.update(m.input.Value())
					m.layout()
					return m, nil
				}
				m.completer.dismiss()
				m.input.SetValue("")
				m.historyIdx = -1
				m.pushHistory(name)
				m.helpOpen = false
				m.busy = true
				m.layout()
				return m, m.handleLineCmd(name)
			}
		}
		line := strings.TrimSpace(m.input.Value())
		m.input.SetValue("")
		m.completer.dismiss()
		m.historyIdx = -1
		if line == "" {
			if m.inListMode() && m.listLen() > 0 {
				cmd := m.listEnter()
				m.layout()
				m.syncViewport(false)
				return m, cmd
			}
			return m, nil
		}
		m.pushHistory(line)
		m.helpOpen = false
		m.busy = true
		// Optimistic local feedback for plain-text → /new submissions.
		if cmd, ok := m.optimisticNew(line); ok {
			m.layout()
			m.syncViewport(true)
			return m, cmd
		}
		m.layout()
		return m, m.handleLineCmd(line)
	}

	prevVisible := m.completer.visible
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.completer.update(m.input.Value())
	m.historyIdx = -1
	if m.completer.visible != prevVisible {
		m.layout()
	}
	return m, cmd
}

// optimisticNew paints a local user message before Submit returns.
func (m *model) optimisticNew(line string) (tea.Cmd, bool) {
	name, arg := parseSlash(line)
	objective := line
	freshSession := false
	if name != "" {
		if name != "/new" {
			return nil, false
		}
		objective = strings.TrimSpace(arg)
		if objective == "" {
			return nil, false
		}
		freshSession = true
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	execMode := string(m.draftMode)
	if execMode == "" {
		execMode = gatewayclient.ExecutionModeAgent
	}
	if freshSession {
		m.sessionID = ""
		m.messages = nil
		m.plan = nil
		m.planID = ""
		m.prompt = nil
		m.runs = nil
	}
	// Agent = chat turn (not Planner). Plan mode shows planning until approve.
	optimisticState := gatewayclient.TaskStateRunning
	if execMode == gatewayclient.ExecutionModePlan {
		optimisticState = gatewayclient.TaskStatePlanning
	}
	m.task = &gatewayclient.Task{
		ID:            "…",
		Title:         gatewayclient.TaskTitle(objective),
		Objective:     objective,
		State:         optimisticState,
		ExecutionMode: execMode,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if m.sessionID != "" {
		sid := m.sessionID
		m.task.SessionID = &sid
	}
	m.approvalOpen = false
	// Append optimistic user bubble to chat transcript.
	m.messages = append(m.messages, gatewayclient.TranscriptMessage{
		ID:        "local-user:" + now,
		SessionID: m.sessionID,
		Role:      "user",
		Content:   objective,
		CreatedAt: now,
	})
	m.timeline = buildChatTimeline(m.messages, m.task, m.plan, m.prompt, m.runs)
	if m.sessionID != "" {
		m.statusMsg = "sending…"
	} else {
		m.statusMsg = "starting session…"
	}
	m.errMsg = ""
	m.stickBottom = true
	return m.handleLineCmd(line), true
}

func (m *model) pushHistory(line string) {
	if line == "" {
		return
	}
	if n := len(m.history); n > 0 && m.history[n-1] == line {
		return
	}
	m.history = append(m.history, line)
	if len(m.history) > historyLimit {
		m.history = m.history[len(m.history)-historyLimit:]
	}
}

func (m *model) historyPrev() {
	if len(m.history) == 0 {
		return
	}
	if m.historyIdx < 0 {
		m.historyIdx = len(m.history) - 1
	} else if m.historyIdx > 0 {
		m.historyIdx--
	}
	m.input.SetValue(m.history[m.historyIdx])
	m.input.CursorEnd()
	m.completer.update(m.input.Value())
}

func (m *model) historyNext() {
	if m.historyIdx < 0 {
		return
	}
	if m.historyIdx >= len(m.history)-1 {
		m.historyIdx = -1
		m.input.SetValue("")
		m.completer.update("")
		return
	}
	m.historyIdx++
	m.input.SetValue(m.history[m.historyIdx])
	m.input.CursorEnd()
	m.completer.update(m.input.Value())
}

func (m *model) layout() {
	header := 2
	if m.task != nil {
		header = 3
	}
	// strip + status + input box (2 lines + border)
	footer := 6
	if m.completer.visible {
		footer += min(6, len(m.completer.items)) + 2
	}
	if m.list != listNone {
		n := min(overlayMaxLines, max(1, m.listLen()))
		footer += n + 3
	}
	if m.approvalOpen && m.waitingApproval() {
		footer += 6
	}
	h := m.height - header - footer
	if h < 5 {
		h = 5
	}
	w := m.width - 4
	if m.showContextPanel() {
		w = m.width - contextPanelWidth - 6
	}
	if w < 20 {
		w = 20
	}
	m.viewport.Width = w
	m.viewport.Height = h
	m.input.Width = max(10, m.width-8)
}

func (m *model) showContextPanel() bool {
	return m.width >= contextBreakWidth
}

func (m *model) needsRunPoll() bool {
	for _, run := range m.runs {
		switch run.State {
		case gatewayclient.RunStateCompleted, gatewayclient.RunStateFailed, gatewayclient.RunStateCancelled:
		default:
			return true
		}
	}
	if m.task != nil {
		switch m.task.State {
		case gatewayclient.TaskStateCompleted, gatewayclient.TaskStateFailed, gatewayclient.TaskStateCancelled:
			return false
		case gatewayclient.TaskStateRunning, gatewayclient.TaskStatePlanning, gatewayclient.TaskStateWaitingApproval:
			return true
		}
	}
	return false
}

func (m *model) handleSSE(envelope eventapi.Envelope) {
	// Kept for tests; production routes through applySSE.
	if envelope.Sequence > m.sseAfter {
		m.sseAfter = envelope.Sequence
	}
	typ := envelope.Type
	if strings.HasPrefix(typ, "task.") || strings.HasPrefix(typ, "plan.") ||
		strings.HasPrefix(typ, "approval.") || strings.HasPrefix(typ, "run.") {
		m.dirty = true
	}
}

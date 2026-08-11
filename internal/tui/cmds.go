package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yyZe0122/yunmengze-agent/internal/gatewayclient"
	"github.com/yyZe0122/yunmengze-agent/pkg/schedulerapi"
)

func (m model) handleLineCmd(line string) tea.Cmd {
	name, arg := parseSlash(line)
	if name == "" {
		// Plain text: continue current session when focused, else start a new one.
		if m.sessionID != "" && m.sessionID != "…" {
			return m.newTaskCmd(strings.TrimSpace(line))
		}
		name = "/new"
		arg = strings.TrimSpace(line)
	}
	switch name {
	case "/quit":
		return func() tea.Msg { return commandDoneMsg{quit: true} }
	case "/help":
		return func() tea.Msg { return commandDoneMsg{help: true, status: strings.TrimSpace(helpText())} }
	case "/status":
		return m.statusCommandCmd()
	case "/model":
		return m.modelCommandCmd(arg)
	case "/skills":
		return m.skillsCmd()
	case "/theme":
		return m.themeCommandCmd(arg)
	case "/cron":
		return m.cronCmd(arg)
	case "/compact":
		return m.compactCmd(arg)
	case "/perm":
		return m.permCmd(arg)
	case "/memory":
		return m.memoryCmd(arg)
	case "/refresh-memory":
		return m.refreshMemoryCmd()
	case "/new":
		// Explicit /new always opens a fresh session (clear focus first).
		return m.freshSessionCmd(arg)
	case "/pause", "/resume", "/cancel":
		action, _ := gatewayclient.ParseTaskAction(name)
		return m.taskActionCmd(action, arg)
	case "/retry":
		return m.retryCmd()
	case "/back", "/sessions":
		return func() tea.Msg {
			return commandDoneMsg{openList: listSessions, status: "sessions"}
		}
	case "/tasks":
		return m.tasksCmd(arg)
	default:
		return func() tea.Msg {
			return commandDoneMsg{err: fmt.Errorf("unknown command %s (try /help)", name)}
		}
	}
}

// freshSessionCmd clears the current session focus then submits a new task
// (daemon creates a new session via EnsureSession).
func (m model) freshSessionCmd(objective string) tea.Cmd {
	// Capture with empty session so Submit does not reuse.
	mm := m
	mm.sessionID = ""
	return mm.newTaskCmd(objective)
}

func (m model) tasksCmd(arg string) tea.Cmd {
	arg = strings.TrimSpace(arg)
	return func() tea.Msg {
		if arg == "" {
			return commandDoneMsg{openList: listTasks, status: "tasks"}
		}
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		tasks, err := m.gateway.ListTasks(ctx, 50)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		arg = strings.ToLower(arg)
		var match *gatewayclient.Task
		for i := range tasks {
			id := strings.ToLower(string(tasks[i].ID))
			if strings.HasPrefix(id, arg) || strings.Contains(id, arg) {
				if match != nil {
					return commandDoneMsg{openList: listSessions, err: fmt.Errorf("ambiguous task prefix %q", arg)}
				}
				t := tasks[i]
				match = &t
			}
		}
		if match == nil {
			return commandDoneMsg{err: fmt.Errorf("no task matching %q", arg)}
		}
		return commandDoneMsg{taskID: match.ID, closeList: true, status: fmt.Sprintf("focused %s", match.ID)}
	}
}

func (m model) cronCmd(arg string) tea.Cmd {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
			defer cancel()
			jobs, err := m.gateway.ListJobs(ctx, false)
			if err != nil {
				return commandDoneMsg{err: err}
			}
			return commandDoneMsg{openList: listJobs, jobs: jobs, status: "scheduled jobs · /cron <every> <objective> to create"}
		}
	}
	return m.cronCreateCmd(arg)
}

func (m model) permCmd(arg string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		arg = strings.TrimSpace(arg)
		sessionID := strings.TrimSpace(string(m.sessionID))
		if sessionID == "…" {
			sessionID = ""
		}
		if arg == "" {
			items, err := m.gateway.ListPermissions(ctx, sessionID, 20)
			if err != nil {
				return commandDoneMsg{err: err}
			}
			if len(items) == 0 {
				return commandDoneMsg{status: "no pending tool permissions", permissions: items}
			}
			return commandDoneMsg{
				openList:    listPermissions,
				permissions: items,
				status:      fmt.Sprintf("%d pending · Enter once · /perm once|similar|permanent|deny <id>", len(items)),
			}
		}
		parts := strings.Fields(arg)
		if len(parts) != 2 {
			return commandDoneMsg{err: fmt.Errorf("usage: /perm [allow_once|allow_similar|allow_permanent|deny <id-prefix>]")}
		}
		decision := strings.ToLower(strings.TrimSpace(parts[0]))
		prefix := strings.ToLower(strings.TrimSpace(parts[1]))
		// Aliases: once/similar/permanent; allow_session → allow_similar.
		switch decision {
		case "once", "allow_once":
			decision = "allow_once"
		case "similar", "allow_similar", "allow_session", "session":
			decision = "allow_similar"
		case "permanent", "allow_permanent", "always":
			decision = "allow_permanent"
		case "deny":
		default:
			return commandDoneMsg{err: fmt.Errorf("decision must be allow_once, allow_similar, allow_permanent, or deny")}
		}
		items, err := m.gateway.ListPermissions(ctx, sessionID, 50)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		var match *gatewayclient.Permission
		for i := range items {
			id := strings.ToLower(items[i].ID)
			if strings.HasPrefix(id, prefix) || strings.Contains(id, prefix) {
				if match != nil {
					return commandDoneMsg{err: fmt.Errorf("ambiguous permission prefix %q", prefix)}
				}
				t := items[i]
				match = &t
			}
		}
		if match == nil {
			return commandDoneMsg{err: fmt.Errorf("no pending permission matching %q", prefix)}
		}
		var updated gatewayclient.Permission
		var decideErr error
		if decision == "allow_permanent" {
			// Typed /perm permanent assumes explicit user intent (= second confirm).
			updated, decideErr = m.gateway.DecidePermissionConfirm(ctx, match.ID, decision, true)
		} else {
			updated, decideErr = m.gateway.DecidePermission(ctx, match.ID, decision)
		}
		if decideErr != nil {
			return commandDoneMsg{err: decideErr}
		}
		remaining, _ := m.gateway.ListPermissions(ctx, sessionID, 20)
		msg := commandDoneMsg{
			status:      fmt.Sprintf("permission %s → %s (%s)", shortID(updated.ID), decision, updated.ToolName),
			permissions: remaining,
		}
		if len(remaining) == 0 {
			msg.closeList = true
		}
		return msg
	}
}

func (m model) permDecideCmd(permissionID, decision string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		var updated gatewayclient.Permission
		var err error
		if decision == "allow_permanent" {
			// Hotkey / Enter cycle for permanent requires confirm (same as typed /perm).
			updated, err = m.gateway.DecidePermissionConfirm(ctx, permissionID, decision, true)
		} else {
			updated, err = m.gateway.DecidePermission(ctx, permissionID, decision)
		}
		if err != nil {
			return commandDoneMsg{err: err}
		}
		sessionID := strings.TrimSpace(string(m.sessionID))
		if sessionID == "…" {
			sessionID = ""
		}
		remaining, _ := m.gateway.ListPermissions(ctx, sessionID, 20)
		msg := commandDoneMsg{
			status:      fmt.Sprintf("permission %s → %s (%s)", shortID(updated.ID), decision, updated.ToolName),
			permissions: remaining,
		}
		if len(remaining) == 0 {
			msg.closeList = true
		} else {
			msg.openList = listPermissions
		}
		return msg
	}
}

func (m model) pollPermissionsCmd(autoOpen bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		sessionID := strings.TrimSpace(string(m.sessionID))
		if sessionID == "…" {
			sessionID = ""
		}
		items, err := m.gateway.ListPermissions(ctx, sessionID, 20)
		if err != nil {
			return permPollDoneMsg{err: err}
		}
		return permPollDoneMsg{permissions: items, openList: autoOpen && len(items) > 0}
	}
}

func (m model) retryCmd() tea.Cmd {
	return func() tea.Msg {
		objective := lastUserMessage(m.messages)
		if objective == "" {
			return commandDoneMsg{err: fmt.Errorf("no user message to retry on focused session")}
		}
		if m.sessionID == "" || m.sessionID == "…" {
			return commandDoneMsg{err: fmt.Errorf("focus a session first")}
		}
		return m.newTaskCmd(objective)()
	}
}

func lastUserMessage(messages []gatewayclient.TranscriptMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(messages[i].Role), "user") {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}

func (m model) compactCmd(arg string) tea.Cmd {
	return func() tea.Msg {
		sessionID := strings.TrimSpace(string(m.sessionID))
		if sessionID == "" || sessionID == "…" {
			return commandDoneMsg{err: fmt.Errorf("focus a session first (send a message or /sessions), then /compact")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		result, err := m.gateway.CompactSession(ctx, gatewayclient.SessionID(sessionID), arg)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		src := result.Source
		if src == "" {
			src = "ok"
		}
		status := fmt.Sprintf("compacted session (%s)", src)
		if f := strings.TrimSpace(arg); f != "" {
			status = fmt.Sprintf("compacted session (%s, focus)", src)
		}
		return commandDoneMsg{status: status}
	}
}

func (m model) refreshMemoryCmd() tea.Cmd {
	return func() tea.Msg {
		sessionID := strings.TrimSpace(string(m.sessionID))
		if sessionID == "…" {
			sessionID = ""
		}
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		if err := m.gateway.RefreshMemory(ctx, sessionID); err != nil {
			return commandDoneMsg{err: err}
		}
		if sessionID == "" {
			return commandDoneMsg{status: "memory snapshot cleared (all sessions); next turns reinject"}
		}
		return commandDoneMsg{status: "memory snapshot refreshed for session; next turn reinjects"}
	}
}

func (m model) memoryCmd(arg string) tea.Cmd {
	return func() tea.Msg {
		arg = strings.TrimSpace(arg)
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		sessionID := strings.TrimSpace(string(m.sessionID))
		if sessionID == "…" {
			sessionID = ""
		}
		if arg == "" {
			entries, err := m.gateway.ListMemory(ctx, sessionID, "", "", 32)
			if err != nil {
				return commandDoneMsg{err: err}
			}
			return commandDoneMsg{status: formatMemoryList(entries)}
		}
		fields := strings.Fields(arg)
		switch strings.ToLower(fields[0]) {
		case "refresh":
			if err := m.gateway.RefreshMemory(ctx, sessionID); err != nil {
				return commandDoneMsg{err: err}
			}
			return commandDoneMsg{status: "memory snapshot refreshed"}
		case "forget":
			if len(fields) < 2 {
				return commandDoneMsg{err: fmt.Errorf("usage: /memory forget <entry_id>")}
			}
			if err := m.gateway.ForgetMemory(ctx, fields[1]); err != nil {
				return commandDoneMsg{err: err}
			}
			return commandDoneMsg{status: "forgot " + fields[1]}
		case "promote":
			if len(fields) < 2 {
				return commandDoneMsg{err: fmt.Errorf("usage: /memory promote <entry_id>")}
			}
			e, err := m.gateway.PromoteMemory(ctx, fields[1])
			if err != nil {
				return commandDoneMsg{err: err}
			}
			return commandDoneMsg{status: fmt.Sprintf("promoted → %s (global curated)", e.ID)}
		default:
			// Treat remainder as search query.
			entries, err := m.gateway.ListMemory(ctx, sessionID, arg, "", 32)
			if err != nil {
				return commandDoneMsg{err: err}
			}
			return commandDoneMsg{status: formatMemoryList(entries)}
		}
	}
}

func formatMemoryList(entries []gatewayclient.MemoryEntry) string {
	if len(entries) == 0 {
		return "no memory entries"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d memory entr", len(entries)))
	if len(entries) == 1 {
		b.WriteString("y:\n")
	} else {
		b.WriteString("ies:\n")
	}
	for i, e := range entries {
		if i >= 20 {
			b.WriteString(fmt.Sprintf("… +%d more\n", len(entries)-20))
			break
		}
		scope := e.SessionID
		if scope == "" {
			scope = "global"
		}
		kind := e.Kind
		if kind == "" {
			kind = "?"
		}
		content := strings.ReplaceAll(e.Content, "\n", " ")
		if len([]rune(content)) > 80 {
			content = string([]rune(content)[:80]) + "…"
		}
		b.WriteString(fmt.Sprintf("  %s [%s/%s] %s\n", e.ID, kind, scope, content))
	}
	return strings.TrimSpace(b.String())
}

// cronCreateCmd: /cron <every> <objective> on the current session (TUI primary path).
// Mode and skills follow the draft (Tab agent|plan, /skills).
func (m model) cronCreateCmd(arg string) tea.Cmd {
	return func() tea.Msg {
		everyRaw, objective, ok := splitCronCreateArg(arg)
		if !ok {
			return commandDoneMsg{err: fmt.Errorf("usage: /cron <every> <objective>  (e.g. /cron 15m check status)")}
		}
		every, err := parseCronEvery(everyRaw)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		sessionID := strings.TrimSpace(string(m.sessionID))
		if sessionID == "" || sessionID == "…" {
			return commandDoneMsg{err: fmt.Errorf("focus a session first (send a message or /sessions), then /cron")}
		}
		execMode := string(m.draftMode)
		if execMode != gatewayclient.ExecutionModePlan {
			execMode = gatewayclient.ExecutionModeAgent
		}
		key, err := gatewayclient.RandomID("job-")
		if err != nil {
			return commandDoneMsg{err: err}
		}
		title := gatewayclient.TaskTitle(objective)
		name := title
		if len(name) > 40 {
			name = name[:40]
		}
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		job, err := m.gateway.CreateJob(ctx, schedulerapi.CreateRequest{
			Name: name, SessionID: sessionID, TaskTitle: title, TaskObjective: objective,
			ExecutionMode: execMode, SkillIDs: append([]string(nil), m.selectedSkillIDs...),
			IntervalSeconds: int64(every.Seconds()), IdempotencyKey: key,
		})
		if err != nil {
			return commandDoneMsg{err: err}
		}
		jobs, listErr := m.gateway.ListJobs(ctx, false)
		if listErr != nil {
			return commandDoneMsg{
				status: fmt.Sprintf("created job %s every %s (%s)", shortID(job.ID), every, execMode),
			}
		}
		return commandDoneMsg{
			openList: listJobs, jobs: jobs,
			status: fmt.Sprintf("created job %s every %s (%s)", shortID(job.ID), every, execMode),
		}
	}
}

func splitCronCreateArg(arg string) (every, objective string, ok bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", "", false
	}
	parts := strings.SplitN(arg, " ", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	every = strings.TrimSpace(parts[0])
	objective = strings.TrimSpace(parts[1])
	if every == "" || objective == "" {
		return "", "", false
	}
	return every, objective, true
}

func parseCronEvery(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid interval %q (use Go duration, e.g. 15m, 1h)", raw)
	}
	if d < time.Second {
		return 0, fmt.Errorf("interval must be at least 1s")
	}
	return d, nil
}

func (m model) themeCommandCmd(arg string) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(arg) != "" {
			return commandDoneMsg{err: fmt.Errorf("/theme toggles day/night (no args)")}
		}
		return commandDoneMsg{toggleTheme: true}
	}
}

func (m model) skillsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		skills, err := m.gateway.ListSkills(ctx)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		status := "toggle skills · Enter select · Esc close"
		if n := len(m.selectedSkillIDs); n > 0 {
			status = fmt.Sprintf("%d skill(s) selected · Enter toggle · Esc close", n)
		}
		return commandDoneMsg{openList: listSkills, skills: skills, status: status}
	}
}

func (m model) loadStatusCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		health, err := m.gateway.Health(ctx)
		if err != nil {
			return statusDoneMsg{err: err}
		}
		modelCfg, err := m.gateway.ModelConfig(ctx)
		if err != nil {
			return statusDoneMsg{health: health, err: err}
		}
		msg := statusDoneMsg{health: health, model: modelCfg}
		if mcp, mcpErr := m.gateway.MCPStatus(ctx); mcpErr == nil {
			msg.mcp = mcp
			msg.mcpOK = true
		}
		return msg
	}
}

func (m model) statusCommandCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		health, err := m.gateway.Health(ctx)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		modelCfg, _ := m.gateway.ModelConfig(ctx)
		var b strings.Builder
		fmt.Fprintf(&b, "health ok=%v model=%s draft=%s", health.OK, modelCfg.Model, m.draftMode)
		if m.sessionID != "" && m.sessionID != "…" {
			fmt.Fprintf(&b, " session=%s", shortID(string(m.sessionID)))
		}
		if m.task != nil {
			fmt.Fprintf(&b, " task=%s state=%s", shortID(string(m.task.ID)), m.task.State)
		}
		if m.contextOK {
			fmt.Fprintf(&b, " ctx=%d/%d", m.taskContext.LastPromptTokens, m.taskContext.UsableTokens)
			if m.taskContext.Pressure > 0 {
				fmt.Fprintf(&b, " pressure=%.0f%%", m.taskContext.Pressure*100)
			}
		}
		sessionID := strings.TrimSpace(string(m.sessionID))
		if sessionID == "…" {
			sessionID = ""
		}
		if perms, err := m.gateway.ListPermissions(ctx, sessionID, 20); err == nil {
			fmt.Fprintf(&b, " perms=%d", len(perms))
		}
		if n := len(m.selectedSkillIDs); n > 0 {
			fmt.Fprintf(&b, " skills=%d", n)
		}
		return commandDoneMsg{status: b.String()}
	}
}

func (m model) modelCommandCmd(arg string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		cfg, err := m.gateway.ModelConfig(ctx)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		arg = strings.TrimSpace(arg)
		if arg == "" {
			return commandDoneMsg{
				openList:      listModels,
				modelName:     cfg.Model,
				models:        cfg.Models,
				contextWindow: cfg.ContextWindow,
				status:        "select a model",
			}
		}
		if !strings.Contains(arg, "/") {
			return commandDoneMsg{err: fmt.Errorf("model must use provider/model format (or /model with no args to pick)")}
		}
		if arg == cfg.Model {
			return commandDoneMsg{
				status:        fmt.Sprintf("already using %s", cfg.Model),
				modelName:     cfg.Model,
				models:        cfg.Models,
				contextWindow: cfg.ContextWindow,
				closeList:     true,
			}
		}
		updated, err := m.gateway.SetModelConfig(ctx, arg)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		return commandDoneMsg{
			status:        fmt.Sprintf("model=%s", updated.Model),
			modelName:     updated.Model,
			models:        updated.Models,
			contextWindow: updated.ContextWindow,
			closeList:     true,
		}
	}
}

func (m model) newTaskCmd(objective string) tea.Cmd {
	objective = strings.TrimSpace(objective)
	execMode := string(m.draftMode)
	if execMode == "" {
		execMode = gatewayclient.ExecutionModeAgent
	}
	sessionID := m.sessionID
	if sessionID == "…" {
		sessionID = ""
	}
	skillIDs := append([]string(nil), m.selectedSkillIDs...)
	return func() tea.Msg {
		if objective == "" {
			return commandDoneMsg{err: fmt.Errorf("usage: /new <objective>")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		req := gatewayclient.TaskSubmissionRequest{
			Title: gatewayclient.TaskTitle(objective), Objective: objective,
			ExecutionMode: execMode, Workspace: m.cwd,
		}
		if sessionID != "" {
			req.SessionID = sessionID
		}
		if len(skillIDs) > 0 {
			req.SkillIDs = skillIDs
		}
		submitted, err := m.gateway.SubmitTask(ctx, req)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		sid := sessionID
		if submitted.Task.SessionID != nil && *submitted.Task.SessionID != "" {
			sid = *submitted.Task.SessionID
		}
		label := "build"
		if execMode == gatewayclient.ExecutionModePlan {
			label = "plan (read-only)"
		}
		status := fmt.Sprintf("%s · task %s", label, shortID(string(submitted.Task.ID)))
		if sid != "" {
			status = fmt.Sprintf("%s · session %s · task %s", label, shortID(string(sid)), shortID(string(submitted.Task.ID)))
		}
		return commandDoneMsg{
			status:    status,
			taskID:    submitted.Task.ID,
			planID:    "",
			sessionID: sid,
		}
	}
}

func (m model) taskActionCmd(action gatewayclient.TaskAction, reason string) tea.Cmd {
	return func() tea.Msg {
		if m.task == nil {
			return commandDoneMsg{err: fmt.Errorf("no current task")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		current, err := m.gateway.GetTask(ctx, m.task.ID)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		updated, err := m.gateway.ControlTask(ctx, current.ID, action, current.Version, reason)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		return commandDoneMsg{status: fmt.Sprintf("task %s → %s", updated.ID, updated.State), taskID: updated.ID}
	}
}

func (m model) refreshCmd(gen uint64, kind refreshKind) tea.Cmd {
	gw := m.gateway
	taskID := gatewayclient.TaskID("")
	if m.task != nil && m.task.ID != "" && m.task.ID != "…" {
		taskID = m.task.ID
	}
	sessionID := m.sessionID
	if sessionID == "…" {
		sessionID = ""
	}
	planID := m.planID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()

		msg := refreshDoneMsg{gen: gen, kind: kind}

		if kind == refreshFull {
			if sessions, err := gw.ListSessions(ctx, 30); err == nil {
				msg.sessions = sessions
			}
			if tasks, err := gw.ListTasks(ctx, 20); err == nil {
				msg.tasks = tasks
			} else if msg.sessions == nil {
				msg.err = err
				return msg
			}
		}

		// Transcript: prefer session, else task.
		if sessionID != "" {
			if messages, err := gw.SessionMessages(ctx, sessionID, 200); err == nil {
				msg.messages = messages
			}
		} else if taskID != "" {
			if messages, err := gw.TaskMessages(ctx, taskID, 200); err == nil {
				msg.messages = messages
			}
		}

		if taskID == "" {
			return msg
		}

		var (
			wg          sync.WaitGroup
			taskMu      sync.Mutex
			taskErr     error
			task        gatewayclient.Task
			plan        *gatewayclient.Plan
			runs        []gatewayclient.Run
			usage       gatewayclient.TaskUsage
			usageOK     bool
			taskContext gatewayclient.TaskContext
			contextOK   bool
		)

		needTask := kind == refreshFull || kind == refreshTask || kind == refreshPlan
		needPlan := kind == refreshFull || kind == refreshPlan || kind == refreshTask
		needRuns := kind == refreshFull || kind == refreshRuns || kind == refreshTask
		needUsage := kind == refreshFull || kind == refreshRuns

		if needTask {
			wg.Add(1)
			go func() {
				defer wg.Done()
				t, err := gw.GetTask(ctx, taskID)
				taskMu.Lock()
				defer taskMu.Unlock()
				if err != nil {
					taskErr = err
					return
				}
				task = t
			}()
		}

		if needPlan {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var p gatewayclient.Plan
				var err error
				if planID != "" {
					p, err = gw.GetPlan(ctx, planID)
				} else {
					p, err = gw.FindPlanForTask(ctx, taskID)
				}
				if err != nil {
					return
				}
				taskMu.Lock()
				plan = &p
				taskMu.Unlock()
			}()
		}

		if needRuns {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, err := gw.ListRuns(ctx, taskID, 50)
				if err != nil {
					return
				}
				taskMu.Lock()
				runs = r
				taskMu.Unlock()
			}()
		}

		if needUsage {
			wg.Add(1)
			go func() {
				defer wg.Done()
				u, err := gw.TaskUsage(ctx, taskID)
				if err != nil {
					return
				}
				taskMu.Lock()
				usage = u
				usageOK = true
				taskMu.Unlock()
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				c, err := gw.TaskContext(ctx, taskID)
				if err != nil {
					return
				}
				taskMu.Lock()
				taskContext = c
				contextOK = true
				taskMu.Unlock()
			}()
		}

		wg.Wait()
		if taskErr != nil && kind != refreshRuns {
			if kind == refreshFull || kind == refreshTask {
				msg.err = taskErr
				return msg
			}
		}
		if task.ID != "" {
			msg.task = &task
		}
		msg.plan = plan
		msg.runs = runs
		msg.usage = usage
		msg.usageOK = usageOK
		msg.taskContext = taskContext
		msg.contextOK = contextOK

		// Parent run usage rollup (self + children); pick root parent when present.
		if needUsage && len(runs) > 0 {
			parentID := pickParentRunID(runs)
			if parentID != "" {
				if ru, err := gw.RunUsage(ctx, parentID); err == nil {
					msg.runUsage = ru
					msg.runUsageOK = true
				}
			}
		}
		return msg
	}
}

// pickParentRunID chooses a top-level run (no parent) that has children, else the first root run.
func pickParentRunID(runs []gatewayclient.Run) gatewayclient.RunID {
	hasChild := make(map[string]bool, len(runs))
	for _, r := range runs {
		if r.ParentRunID != nil && strings.TrimSpace(string(*r.ParentRunID)) != "" {
			hasChild[string(*r.ParentRunID)] = true
		}
	}
	var firstRoot gatewayclient.RunID
	for _, r := range runs {
		if r.ParentRunID != nil && strings.TrimSpace(string(*r.ParentRunID)) != "" {
			continue
		}
		if firstRoot == "" {
			firstRoot = r.ID
		}
		if hasChild[string(r.ID)] {
			return r.ID
		}
	}
	return firstRoot
}

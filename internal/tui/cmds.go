package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"autozeagent.local/autozeagent/internal/gatewayclient"
	"autozeagent.local/autozeagent/pkg/schedulerapi"
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
	case "/new":
		// Explicit /new always opens a fresh session (clear focus first).
		return m.freshSessionCmd(arg)
	case "/pause", "/resume", "/cancel":
		action, _ := gatewayclient.ParseTaskAction(name)
		return m.taskActionCmd(action, arg)
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
		summary := fmt.Sprintf("health ok=%v model=%s mode=%s", health.OK, modelCfg.Model, m.mode)
		if m.task != nil {
			summary += fmt.Sprintf(" task=%s state=%s", m.task.ID, m.task.State)
		}
		return commandDoneMsg{status: summary}
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
			ExecutionMode: execMode,
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
		return msg
	}
}

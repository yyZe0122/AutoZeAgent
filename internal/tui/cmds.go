package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"autozeagent.local/autozeagent/internal/gatewayclient"
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
	case "/theme":
		return m.themeCommandCmd(arg)
	case "/cron":
		return m.cronCmd()
	case "/new":
		// Explicit /new always opens a fresh session (clear focus first).
		return m.freshSessionCmd(arg)
	case "/approve":
		return m.approveCmd(arg)
	case "/run":
		return m.runCmd()
	case "/pause", "/resume", "/cancel":
		action, _ := gatewayclient.ParseTaskAction(name)
		return m.taskActionCmd(action, arg)
	case "/back", "/sessions":
		return func() tea.Msg {
			return commandDoneMsg{openList: listSessions, status: "sessions"}
		}
	case "/tasks":
		return m.tasksCmd(arg)
	case "/details":
		return func() tea.Msg { return commandDoneMsg{toggleDetails: true} }
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

func (m model) cronCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		jobs, err := m.gateway.ListJobs(ctx, false)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		return commandDoneMsg{openList: listJobs, jobs: jobs, status: "scheduled jobs"}
	}
}

func (m model) themeCommandCmd(arg string) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(arg) != "" {
			return commandDoneMsg{err: fmt.Errorf("/theme toggles day/night (no args)")}
		}
		return commandDoneMsg{toggleTheme: true}
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
		return statusDoneMsg{health: health, model: modelCfg}
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
				openList:  listModels,
				modelName: cfg.Model,
				models:    cfg.Models,
				status:    "select a model",
			}
		}
		if !strings.Contains(arg, "/") {
			return commandDoneMsg{err: fmt.Errorf("model must use provider/model format (or /model with no args to pick)")}
		}
		if arg == cfg.Model {
			return commandDoneMsg{
				status:    fmt.Sprintf("already using %s", cfg.Model),
				modelName: cfg.Model,
				models:    cfg.Models,
				closeList: true,
			}
		}
		updated, err := m.gateway.SetModelConfig(ctx, arg)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		return commandDoneMsg{
			status:    fmt.Sprintf("model=%s", updated.Model),
			modelName: updated.Model,
			models:    updated.Models,
			closeList: true,
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
		submitted, err := m.gateway.SubmitTask(ctx, req)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		sid := sessionID
		if submitted.Task.SessionID != nil && *submitted.Task.SessionID != "" {
			sid = *submitted.Task.SessionID
		}
		status := fmt.Sprintf("task %s submitted (%s · %s)", submitted.Task.ID, submitted.Task.State, execMode)
		if execMode == gatewayclient.ExecutionModeAgent {
			status = fmt.Sprintf("message sent · task %s", shortID(string(submitted.Task.ID)))
			if sid != "" {
				status = fmt.Sprintf("chat · session %s · task %s", shortID(string(sid)), shortID(string(submitted.Task.ID)))
			}
		} else if submitted.PlanningPending {
			status = fmt.Sprintf("task %s planning… (%s)", submitted.Task.ID, execMode)
			if sid != "" {
				status = fmt.Sprintf("planning… · task %s (%s)", shortID(string(submitted.Task.ID)), execMode)
			}
			status += " — plan mode: approve to run"
		}
		// Agent chat must not focus a client-invented plan- id (that is plan-mode only).
		planID := submitted.PlanID
		if execMode == gatewayclient.ExecutionModeAgent {
			planID = ""
		}
		return commandDoneMsg{
			status:    status,
			taskID:    submitted.Task.ID,
			planID:    planID,
			sessionID: sid,
		}
	}
}

func (m model) approveCmd(arg string) tea.Cmd {
	return func() tea.Msg {
		action, err := gatewayclient.ParseApprovalAction(arg)
		if err != nil {
			return commandDoneMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		prompt := m.prompt
		if prompt == nil {
			planID, err := m.resolvePlanID(ctx)
			if err != nil {
				return commandDoneMsg{err: err}
			}
			p, err := m.gateway.ApprovalPrompt(ctx, planID, "")
			if err != nil {
				return commandDoneMsg{err: err}
			}
			prompt = &p
		}
		if !gatewayclient.PromptAllows(*prompt, action, "") {
			return commandDoneMsg{err: fmt.Errorf("action %s not available", action)}
		}
		decision, err := m.gateway.DecideApproval(ctx, *prompt, "", action, "local-user", "")
		if err != nil {
			return commandDoneMsg{err: err}
		}
		status := fmt.Sprintf("approval %s (%s)", decision.ID, decision.Decision)
		// After human approve, Start is allowed for both agent and plan modes; grants enforce scope.
		switch action {
		case gatewayclient.ActionAllowPlan, gatewayclient.ActionAllowOnce, gatewayclient.ActionAllowLimited:
			started, startErr := m.gateway.StartRuns(ctx, gatewayclient.RunStartRequest{
				TaskID: prompt.TaskID, PlanID: prompt.PlanID, PlanRevision: prompt.Revision, PlanHash: prompt.PlanHash,
			})
			if startErr != nil {
				return commandDoneMsg{status: status, err: fmt.Errorf("auto-start runs: %w", startErr), taskID: prompt.TaskID}
			}
			status += fmt.Sprintf("; started %d run(s)", len(started.RunIDs))
			return commandDoneMsg{status: status, taskID: prompt.TaskID}
		}
		return commandDoneMsg{status: status, taskID: prompt.TaskID}
	}
}

func (m model) runCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		prompt := m.prompt
		if prompt == nil {
			planID, err := m.resolvePlanID(ctx)
			if err != nil {
				return commandDoneMsg{err: err}
			}
			p, err := m.gateway.ApprovalPrompt(ctx, planID, "")
			if err != nil {
				return commandDoneMsg{err: err}
			}
			prompt = &p
		}
		started, err := m.gateway.StartRuns(ctx, gatewayclient.RunStartRequest{
			TaskID: prompt.TaskID, PlanID: prompt.PlanID, PlanRevision: prompt.Revision, PlanHash: prompt.PlanHash,
		})
		if err != nil {
			return commandDoneMsg{err: err}
		}
		return commandDoneMsg{
			status: fmt.Sprintf("started %d run(s)", len(started.RunIDs)),
			taskID: prompt.TaskID,
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

func (m model) resolvePlanID(ctx context.Context) (gatewayclient.PlanID, error) {
	if m.planID != "" {
		return m.planID, nil
	}
	if m.plan != nil {
		return m.plan.ID, nil
	}
	if m.prompt != nil {
		return m.prompt.PlanID, nil
	}
	if m.task == nil {
		return "", fmt.Errorf("no current task/plan")
	}
	plan, err := m.gateway.FindPlanForTask(ctx, m.task.ID)
	if err != nil {
		return "", err
	}
	return plan.ID, nil
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
			wg      sync.WaitGroup
			taskMu  sync.Mutex
			taskErr error
			task    gatewayclient.Task
			plan    *gatewayclient.Plan
			prompt  *gatewayclient.Prompt
			runs    []gatewayclient.Run
			usage   gatewayclient.TaskUsage
			usageOK bool
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
				if pr, promptErr := gw.ApprovalPrompt(ctx, p.ID, ""); promptErr == nil {
					taskMu.Lock()
					prompt = &pr
					taskMu.Unlock()
				}
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
		msg.prompt = prompt
		msg.runs = runs
		msg.usage = usage
		msg.usageOK = usageOK
		return msg
	}
}

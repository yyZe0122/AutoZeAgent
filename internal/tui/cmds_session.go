package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yyZe0122/yunmengze-agent/internal/gatewayclient"
)

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

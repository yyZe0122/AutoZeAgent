package tui

import (
	"fmt"
	"strings"

	"autozeagent.local/autozeagent/internal/gatewayclient"
)

type timelineKind string

const (
	tlUser   timelineKind = "user"
	tlSystem timelineKind = "system"
	tlPlan   timelineKind = "plan"
	tlRun    timelineKind = "run"
	tlError  timelineKind = "error"
)

type timelineItem struct {
	Kind  timelineKind
	At    string
	Title string
	Body  string
	State string
}

func buildTimeline(task *gatewayclient.Task, plan *gatewayclient.Plan, prompt *gatewayclient.Prompt, runs []gatewayclient.Run) []timelineItem {
	if task == nil {
		return nil
	}
	items := make([]timelineItem, 0, 8+len(runs))

	obj := strings.TrimSpace(task.Objective)
	if obj == "" {
		obj = task.Title
	}
	items = append(items, timelineItem{
		Kind: tlUser, At: task.CreatedAt, Title: "objective", Body: obj,
	})

	items = append(items, timelineItem{
		Kind: tlSystem, At: task.CreatedAt, Title: "task created",
		Body: fmt.Sprintf("%s", task.ID), State: gatewayclient.TaskStateCreated,
	})

	switch task.State {
	case gatewayclient.TaskStatePlanning:
		items = append(items, timelineItem{
			Kind: tlSystem, At: task.UpdatedAt, Title: "planning…", State: task.State,
		})
	case gatewayclient.TaskStateWaitingApproval:
		items = append(items, timelineItem{
			Kind: tlSystem, At: task.UpdatedAt, Title: "waiting approval", State: task.State,
		})
	case gatewayclient.TaskStateRunning:
		items = append(items, timelineItem{
			Kind: tlSystem, At: task.UpdatedAt, Title: "running", State: task.State,
		})
	case gatewayclient.TaskStatePaused:
		items = append(items, timelineItem{
			Kind: tlSystem, At: task.UpdatedAt, Title: "paused", State: task.State,
		})
	case gatewayclient.TaskStateCompleted:
		items = append(items, timelineItem{
			Kind: tlSystem, At: task.UpdatedAt, Title: "completed", State: task.State,
		})
	case gatewayclient.TaskStateFailed:
		items = append(items, timelineItem{
			Kind: tlError, At: task.UpdatedAt, Title: "task failed", State: task.State,
		})
	case gatewayclient.TaskStateCancelled:
		items = append(items, timelineItem{
			Kind: tlSystem, At: task.UpdatedAt, Title: "cancelled", State: task.State,
		})
	}

	if prompt != nil {
		body := fmt.Sprintf("rev=%d · %d step(s)", prompt.Revision, len(prompt.Steps))
		if len(prompt.Steps) > 0 {
			maxRisk := ""
			for _, s := range prompt.Steps {
				r := string(s.Risk)
				if riskRank(r) >= riskRank(maxRisk) {
					maxRisk = r
				}
			}
			if maxRisk != "" {
				body += " · max risk=" + maxRisk
			}
		}
		items = append(items, timelineItem{
			Kind: tlPlan, At: "", Title: fmt.Sprintf("plan %s", prompt.PlanID),
			Body: body, State: "ready",
		})
	} else if plan != nil {
		items = append(items, timelineItem{
			Kind: tlPlan, At: plan.UpdatedAt, Title: fmt.Sprintf("plan %s", plan.ID),
			Body:  fmt.Sprintf("rev=%d state=%s · %d step(s)", plan.Revision, plan.State, len(plan.Steps)),
			State: plan.State,
		})
	}

	for _, run := range runs {
		step := "plan"
		if run.StepID != nil {
			step = string(*run.StepID)
		}
		title := fmt.Sprintf("run %s [%s]", shortID(string(run.ID)), step)
		body := ""
		kind := tlRun
		if run.Error != nil && strings.TrimSpace(*run.Error) != "" {
			kind = tlError
			body = foldBody(strings.TrimSpace(*run.Error))
		} else if run.Result != nil && strings.TrimSpace(*run.Result) != "" {
			body = foldBody(strings.TrimSpace(*run.Result))
		}
		at := run.StartedAt
		if run.FinishedAt != nil {
			at = *run.FinishedAt
		}
		items = append(items, timelineItem{
			Kind: kind, At: at, Title: title, Body: body, State: run.State,
		})
	}
	return items
}

// foldBody truncates large run output so the viewport stays cheap to rebuild
// (Crush-style: cache/fold expensive message bodies; expand via /details later).
func foldBody(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	truncated := false
	if len(lines) > timelineBodyMaxLines {
		lines = lines[:timelineBodyMaxLines]
		truncated = true
	}
	out := strings.Join(lines, "\n")
	runes := []rune(out)
	if len(runes) > timelineBodyMaxChars {
		out = string(runes[:timelineBodyMaxChars])
		truncated = true
	}
	if truncated {
		out = strings.TrimRight(out, "\n") + "\n… (truncated · /details)"
	}
	return out
}

func riskRank(r string) int {
	switch strings.ToLower(r) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium", "moderate":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12] + "…"
}

func renderTimeline(items []timelineItem) string {
	return renderTimelineUncached(items)
}

func renderTimelineUncached(items []timelineItem) string {
	if len(items) == 0 {
		return styleDim.Render("No activity yet.")
	}
	var b strings.Builder
	for i, item := range items {
		prefix := "·"
		var titleStyle = styleTLSys
		switch item.Kind {
		case tlUser:
			prefix = "▸"
			titleStyle = styleTLUser
		case tlPlan:
			prefix = "◇"
			titleStyle = styleTLPlan
		case tlRun:
			prefix = "●"
			titleStyle = styleTLRun
		case tlError:
			prefix = "✖"
			titleStyle = styleTLErr
		}
		line := titleStyle.Render(prefix + " " + item.Title)
		if item.State != "" {
			line += "  " + stateBadge(item.State)
		}
		b.WriteString(line)
		b.WriteByte('\n')
		if item.Body != "" {
			for _, bl := range strings.Split(item.Body, "\n") {
				b.WriteString(styleDim.Render("  "+bl) + "\n")
			}
		}
		if i < len(items)-1 {
			b.WriteString(styleMuted.Render("  │") + "\n")
		}
	}
	return b.String()
}

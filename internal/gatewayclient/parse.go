package gatewayclient

import (
	"fmt"
	"strings"
)

// ParseApprovalAction maps a CLI/TUI argument to an approval Action.
// Empty arg defaults to allow_plan.
func ParseApprovalAction(arg string) (Action, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		arg = string(ActionAllowPlan)
	}
	action := Action(arg)
	if !ValidApprovalAction(action) {
		return "", fmt.Errorf("unknown approval action %q", arg)
	}
	return action, nil
}

// ParseTaskAction maps a slash command name to a task control action.
func ParseTaskAction(name string) (TaskAction, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "/pause", "pause":
		return TaskActionPause, true
	case "/resume", "resume":
		return TaskActionResume, true
	case "/cancel", "cancel":
		return TaskActionCancel, true
	default:
		return "", false
	}
}

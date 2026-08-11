package gatewayclient

import (
	"strings"
)

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

// Package runlog builds shared slog key-value pairs for the daemon log chain (ADR-047).
// Call sites still use log/slog; this package only reduces field-name drift.
package runlog

import "strings"

// IDs are optional identity fields for a log line. Empty strings are omitted.
type IDs struct {
	SessionID string
	TaskID    string
	RunID     string
	PlanID    string
	StepID    string
	TraceID   string
}

// Attrs returns alternating key/value pairs for slog.Info/Error/etc.
// Always includes component, operation, result when non-empty; then non-empty IDs.
func Attrs(component, operation, result string, ids IDs, extra ...any) []any {
	out := make([]any, 0, 16+len(extra))
	if c := strings.TrimSpace(component); c != "" {
		out = append(out, "component", c)
	}
	if o := strings.TrimSpace(operation); o != "" {
		out = append(out, "operation", o)
	}
	if r := strings.TrimSpace(result); r != "" {
		out = append(out, "result", r)
	}
	if v := strings.TrimSpace(ids.SessionID); v != "" {
		out = append(out, "session_id", v)
	}
	if v := strings.TrimSpace(ids.TaskID); v != "" {
		out = append(out, "task_id", v)
	}
	if v := strings.TrimSpace(ids.RunID); v != "" {
		out = append(out, "run_id", v)
	}
	if v := strings.TrimSpace(ids.PlanID); v != "" {
		out = append(out, "plan_id", v)
	}
	if v := strings.TrimSpace(ids.StepID); v != "" {
		out = append(out, "step_id", v)
	}
	trace := strings.TrimSpace(ids.TraceID)
	if trace == "" {
		trace = strings.TrimSpace(ids.RunID)
	}
	if trace != "" {
		out = append(out, "trace_id", trace)
	}
	out = append(out, extra...)
	return out
}

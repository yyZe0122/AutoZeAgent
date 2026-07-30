package tui

import (
	"fmt"
	"time"

	"autozeagent.local/autozeagent/internal/gatewayclient"
)

// MCPStatus is the future MCP adapter summary for the Metrics panel.
// Enabled is false until Tool Broker exposes MCP status.
type MCPStatus struct {
	Enabled bool
	Total   int
	OK      int
	Error   int
	Detail  string
}

// SessionMetrics is the TUI chrome surface for usage / model / MCP indicators.
// Implementations may return ok=false when the underlying service is missing.
type SessionMetrics interface {
	// TaskTokenUsage returns used tokens and optional budget max for the focused task.
	TaskTokenUsage() (used, max int64, ok bool)
	// ContextWindow is the model context length when known.
	ContextWindow() (n int, ok bool)
	// CacheHitRate is provider cache hit ratio in [0,1] when known.
	CacheHitRate() (rate float64, ok bool)
	// MCPStatus summarizes MCP servers (placeholder until Broker wires adapters).
	MCPStatus() MCPStatus
}

// modelMetrics is the production SessionMetrics backed by model fields.
// Context / cache / MCP stay unavailable until daemon surfaces them.
type modelMetrics struct {
	m *model
}

func (s modelMetrics) TaskTokenUsage() (used, max int64, ok bool) {
	if s.m == nil || !s.m.usageOK {
		return 0, 0, false
	}
	used = s.m.usage.TotalTokens
	if s.m.prompt != nil && s.m.prompt.Budget.MaxTokens > 0 {
		max = s.m.prompt.Budget.MaxTokens
	}
	return used, max, true
}

func (s modelMetrics) ContextWindow() (int, bool) {
	return 0, false
}

func (s modelMetrics) CacheHitRate() (float64, bool) {
	return 0, false
}

func (s modelMetrics) MCPStatus() MCPStatus {
	return MCPStatus{Enabled: false}
}

func (m *model) metrics() SessionMetrics {
	return modelMetrics{m: m}
}

type runActivity int

const (
	activityIdle runActivity = iota
	activityWaiting
	activityActive
)

func (m model) runActivity() runActivity {
	if m.task == nil {
		return activityIdle
	}
	switch m.task.State {
	case gatewayclient.TaskStateWaitingApproval, gatewayclient.TaskStatePaused:
		return activityWaiting
	case gatewayclient.TaskStateRunning, gatewayclient.TaskStatePlanning:
		return activityActive
	case gatewayclient.TaskStateCompleted, gatewayclient.TaskStateFailed, gatewayclient.TaskStateCancelled:
		// fall through — check live runs
	}
	for _, run := range m.runs {
		switch run.State {
		case gatewayclient.RunStateCompleted, gatewayclient.RunStateFailed, gatewayclient.RunStateCancelled:
		default:
			return activityActive
		}
	}
	if m.task.State == gatewayclient.TaskStateWaitingApproval {
		return activityWaiting
	}
	switch m.task.State {
	case gatewayclient.TaskStateCompleted, gatewayclient.TaskStateFailed, gatewayclient.TaskStateCancelled:
		return activityIdle
	default:
		return activityIdle
	}
}

func (m model) activityLabel() string {
	switch m.runActivity() {
	case activityActive:
		if m.task != nil && m.task.State == gatewayclient.TaskStatePlanning {
			return "planning"
		}
		return "running"
	case activityWaiting:
		if m.task != nil && m.task.State == gatewayclient.TaskStatePaused {
			return "paused"
		}
		return "waiting"
	default:
		return "idle"
	}
}

func (m model) activityElapsed() time.Duration {
	if m.runActivity() != activityActive {
		return 0
	}
	var earliest time.Time
	for _, run := range m.runs {
		switch run.State {
		case gatewayclient.RunStateCompleted, gatewayclient.RunStateFailed, gatewayclient.RunStateCancelled:
			continue
		}
		started, err := time.Parse(time.RFC3339Nano, run.StartedAt)
		if err != nil {
			continue
		}
		if earliest.IsZero() || started.Before(earliest) {
			earliest = started
		}
	}
	if earliest.IsZero() && m.task != nil {
		if t, err := time.Parse(time.RFC3339Nano, m.task.UpdatedAt); err == nil {
			earliest = t
		}
	}
	if earliest.IsZero() {
		return 0
	}
	return time.Since(earliest).Round(time.Second)
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	if mins < 60 {
		return fmt.Sprintf("%dm%02ds", mins, secs)
	}
	hours := mins / 60
	mins = mins % 60
	return fmt.Sprintf("%dh%02dm", hours, mins)
}

// heartbeatWave returns a scrolling pink pulse when active, flat line otherwise.
func heartbeatWave(active bool, frame int) string {
	const width = 8
	if !active {
		return "────────"
	}
	// ECG-ish pulse pattern; rotate by frame.
	pattern := []rune("▁▂▃▅▇▅▃▂")
	out := make([]rune, width)
	for i := 0; i < width; i++ {
		out[i] = pattern[(i+frame)%len(pattern)]
	}
	return string(out)
}

func formatTokens(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		if n%1000 == 0 {
			return fmt.Sprintf("%dk", n/1000)
		}
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
}

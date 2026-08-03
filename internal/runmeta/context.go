// Package runmeta carries parent agent-loop metadata into Tool.Execute via context.
// Shared by agent and tools to avoid import cycles (ADR-039 task tool).
package runmeta

import "context"

type key struct{}

// Context is metadata for the current agent run when invoking tools.
type Context struct {
	RunID              string
	TaskID             string
	SessionID          string
	PlanID             string
	PlanHash           string
	StepID             string
	Actor              string
	TraceID            string
	AllowedTools       []string
	CapabilityGrantIDs map[string][]string
	MaxOutputTokens    int64
	MaxTotalTokens     int64
	MaxCostMicros      int64
	ToolTimeoutMillis  int64
	// Depth is 0 for top-level chat runs; each task spawn increments by 1.
	Depth int
	// CallID is the current tool call when set by the broker.
	CallID string
}

// With attaches metadata for tools that need parent identity.
func With(ctx context.Context, rc Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, key{}, rc)
}

// From returns metadata when present.
func From(ctx context.Context) (Context, bool) {
	if ctx == nil {
		return Context{}, false
	}
	rc, ok := ctx.Value(key{}).(Context)
	return rc, ok
}

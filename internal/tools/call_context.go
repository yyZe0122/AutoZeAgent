package tools

import "context"

type callIDContextKey struct{}

// WithToolCallID attaches the broker tool_call_id for process isolation unit naming.
func WithToolCallID(ctx context.Context, callID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, callIDContextKey{}, callID)
}

func toolCallIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(callIDContextKey{}).(string)
	return value
}

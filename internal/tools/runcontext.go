package tools

import (
	"context"

	"github.com/yyZe0122/yunmengze-agent/internal/runmeta"
)

// RunContext is an alias for runmeta.Context (ADR-039).
type RunContext = runmeta.Context

// WithRunContext attaches run metadata for tools that need parent identity.
func WithRunContext(ctx context.Context, rc RunContext) context.Context {
	return runmeta.With(ctx, rc)
}

// RunContextFrom returns metadata when present.
func RunContextFrom(ctx context.Context) (RunContext, bool) {
	return runmeta.From(ctx)
}

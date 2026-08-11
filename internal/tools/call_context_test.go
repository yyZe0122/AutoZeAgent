package tools

import (
	"context"
	"testing"
)

func TestToolCallIDContext(t *testing.T) {
	ctx := WithToolCallID(context.Background(), "call-42")
	if got := toolCallIDFromContext(ctx); got != "call-42" {
		t.Fatalf("got %q", got)
	}
	if got := toolCallIDFromContext(context.Background()); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

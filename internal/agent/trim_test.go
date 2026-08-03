package agent

import (
	"strings"
	"testing"
	"unicode/utf8"

	"autozeagent.local/autozeagent/pkg/providerapi"
)

func TestTrimRunesLeavesShortContent(t *testing.T) {
	got := trimRunes("hello", 32)
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestTrimRunesHeadAndTail(t *testing.T) {
	content := strings.Repeat("a", 100) + "MIDDLE" + strings.Repeat("b", 100)
	got := trimRunes(content, 64)
	if utf8.RuneCountInString(got) > 64 {
		t.Fatalf("len=%d want <=64, got %q", utf8.RuneCountInString(got), got)
	}
	if !strings.Contains(got, "trimmed") {
		t.Fatalf("missing marker: %q", got)
	}
	if !strings.HasPrefix(got, "a") || !strings.HasSuffix(got, "b") {
		t.Fatalf("expected head a… and tail …b, got %q", got)
	}
	if strings.Contains(got, "MIDDLE") {
		t.Fatalf("middle should be dropped: %q", got)
	}
}

func TestTrimMessagesForProviderDoesNotMutateInput(t *testing.T) {
	long := strings.Repeat("x", 200)
	in := []providerapi.Message{
		{Role: providerapi.RoleUser, Content: "hi"},
		{Role: providerapi.RoleTool, ToolCallID: "c1", Content: long},
		{Role: providerapi.RoleAssistant, Content: long, ToolCalls: []providerapi.ToolCall{{ID: "c2", Name: "t", Arguments: `{}`}}},
	}
	out := trimMessagesForProvider(in, 40)
	if in[1].Content != long {
		t.Fatal("input tool content mutated")
	}
	if out[1].Content == long {
		t.Fatal("tool content should be trimmed")
	}
	if utf8.RuneCountInString(out[1].Content) > 40 {
		t.Fatalf("tool content too long: %d", utf8.RuneCountInString(out[1].Content))
	}
	// Assistant with tool calls: content not trimmed.
	if out[2].Content != long {
		t.Fatal("assistant tool-call message content should stay full")
	}
	if out[0].Content != "hi" {
		t.Fatalf("user message changed: %q", out[0].Content)
	}
}

func TestTrimMessagesForProviderTrimsPlainAssistant(t *testing.T) {
	long := strings.Repeat("z", 100)
	out := trimMessagesForProvider([]providerapi.Message{
		{Role: providerapi.RoleAssistant, Content: long},
	}, 32)
	if out[0].Content == long {
		t.Fatal("plain assistant should be trimmed")
	}
}

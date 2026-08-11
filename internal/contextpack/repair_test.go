package contextpack

import (
	"strings"
	"testing"

	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
)

func TestRepairToolMessagesInjectsMissingResults(t *testing.T) {
	in := []providerapi.Message{
		{Role: providerapi.RoleUser, Content: "hi"},
		{Role: providerapi.RoleAssistant, ToolCalls: []providerapi.ToolCall{
			{ID: "c1", Name: "fs_read", Arguments: `{}`},
		}},
		// missing tool result
		{Role: providerapi.RoleUser, Content: "next"},
	}
	out := RepairToolMessages(in)
	found := false
	for _, m := range out {
		if m.Role == providerapi.RoleTool && m.ToolCallID == "c1" {
			found = true
			if !strings.Contains(m.Content, "orphan_tool_call") {
				t.Fatalf("content = %q", m.Content)
			}
		}
	}
	if !found {
		t.Fatalf("missing synthetic tool result: %+v", out)
	}
}

func TestRepairToolMessagesDropsOrphanResults(t *testing.T) {
	in := []providerapi.Message{
		{Role: providerapi.RoleTool, ToolCallID: "ghost", Content: "nope"},
		{Role: providerapi.RoleUser, Content: "u"},
	}
	out := RepairToolMessages(in)
	for _, m := range out {
		if m.Role == providerapi.RoleTool {
			t.Fatalf("orphan result should be dropped: %+v", out)
		}
	}
}

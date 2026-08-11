package contextpack

import (
	"testing"

	"autozeagent.local/autozeagent/pkg/providerapi"
)

func TestSplitHeadTailKeepsToolPairs(t *testing.T) {
	msgs := []providerapi.Message{
		{Role: providerapi.RoleSystem, Content: "sys"},
		{Role: providerapi.RoleUser, Content: "u1"},
		{Role: providerapi.RoleAssistant, Content: "a1"},
		{Role: providerapi.RoleUser, Content: "u2"},
		{Role: providerapi.RoleAssistant, ToolCalls: []providerapi.ToolCall{
			{ID: "t1", Name: "fs_read", Arguments: `{}`},
		}},
		{Role: providerapi.RoleTool, ToolCallID: "t1", Content: "body"},
		{Role: providerapi.RoleUser, Content: "u3"},
		{Role: providerapi.RoleAssistant, Content: "a3"},
	}
	// keep 2 user turns → cut at u2; tool pair with u2 turn must stay in tail
	head, tail := SplitHeadTail(msgs, 2)
	if len(head) == 0 {
		// if cut moved, head may still have u1
	}
	// Ensure tool call and result are both in tail or both in head, never split.
	inHeadCall, inHeadRes := false, false
	inTailCall, inTailRes := false, false
	for _, m := range head {
		if m.Role == providerapi.RoleAssistant && len(m.ToolCalls) > 0 {
			inHeadCall = true
		}
		if m.Role == providerapi.RoleTool && m.ToolCallID == "t1" {
			inHeadRes = true
		}
	}
	for _, m := range tail {
		if m.Role == providerapi.RoleAssistant && len(m.ToolCalls) > 0 {
			inTailCall = true
		}
		if m.Role == providerapi.RoleTool && m.ToolCallID == "t1" {
			inTailRes = true
		}
	}
	if inHeadCall != inHeadRes || inTailCall != inTailRes {
		t.Fatalf("tool pair split: headCall=%v headRes=%v tailCall=%v tailRes=%v", inHeadCall, inHeadRes, inTailCall, inTailRes)
	}
	if !inTailCall && !inHeadCall {
		t.Fatal("tool pair disappeared")
	}
}

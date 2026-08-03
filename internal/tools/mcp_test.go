package tools

import "testing"

func TestMCPLocalName(t *testing.T) {
	got := mcpLocalName("my.server", "tool.name!")
	if got != "mcp_my_server_tool_name" {
		t.Fatalf("got %q", got)
	}
}

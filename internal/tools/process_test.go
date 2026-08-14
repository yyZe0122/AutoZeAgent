package tools

import (
	"encoding/json"
	"testing"

	"github.com/yyZe0122/yunmengze-agent/internal/tools/internal/executor"
)

func TestProcessShellAuthorizationAndEcho(t *testing.T) {
	root := t.TempDir()
	guard, err := NewPathGuard([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := executor.NewRunner(executor.Config{MaxOutputBytes: 64 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := newProcessShellTool(guard, runner)
	if err != nil {
		t.Fatal(err)
	}
	if tool.Definition().Name != "process_shell" {
		t.Fatalf("name=%s", tool.Definition().Name)
	}
	auth, err := tool.Authorization(mustJSON(t, map[string]any{"command": "echo hi", "directory": root}))
	if err != nil {
		t.Fatal(err)
	}
	if auth.Capability != "process_shell" || auth.Command != "/bin/sh" {
		t.Fatalf("auth=%+v", auth)
	}
	if len(auth.Arguments) != 2 || auth.Arguments[0] != "-c" || auth.Arguments[1] != "echo hi" {
		t.Fatalf("args=%v", auth.Arguments)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

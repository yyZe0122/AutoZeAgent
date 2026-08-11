package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteLogsFiltersAndTails(t *testing.T) {
	path := filepath.Join(t.TempDir(), daemonLogFileName)
	content := strings.Join([]string{
		`{"level":"INFO","component":"agent","run_id":"run-1","session_id":"s1","task_id":"t1","msg":"first"}`,
		`not-json`,
		`{"level":"ERROR","component":"agent","run_id":"run-1","session_id":"s1","task_id":"t1","msg":"wrong-level"}`,
		`{"level":"INFO","component":"tool_broker","run_id":"run-1","session_id":"s1","task_id":"t1","msg":"wrong-component"}`,
		`{"level":"INFO","component":"agent","run_id":"run-1","session_id":"s1","task_id":"t1","msg":"second"}`,
		`{"level":"INFO","component":"agent","run_id":"run-1","session_id":"s1","task_id":"t1","msg":"third"}`,
		`{"level":"INFO","component":"agent","run_id":"run-2","session_id":"s2","task_id":"t2","msg":"other-session"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := writeLogs(&output, path, logFilters{
		tail: 2, level: "info", component: "agent", runID: "run-1", sessionID: "s1", taskID: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "first") || strings.Contains(got, "wrong-") || strings.Contains(got, "other-session") {
		t.Fatalf("unexpected log entry in output: %s", got)
	}
	if !strings.Contains(got, "second") || !strings.Contains(got, "third") {
		t.Fatalf("tail output missing expected entries: %s", got)
	}
	if lines := strings.Count(strings.TrimSpace(got), "\n") + 1; lines != 2 {
		t.Fatalf("output has %d lines, want 2: %s", lines, got)
	}
}

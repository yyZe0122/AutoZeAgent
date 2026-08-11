package runlog_test

import (
	"testing"

	"github.com/yyZe0122/yunmengze-agent/internal/runlog"
)

func TestAttrsOmitsEmptyAndDefaultsTrace(t *testing.T) {
	got := runlog.Attrs("agent", "run", "started", runlog.IDs{
		SessionID: "s1",
		TaskID:    "t1",
		RunID:     "r1",
	}, "iteration", 2)
	wantKeys := []string{"component", "operation", "result", "session_id", "task_id", "run_id", "trace_id", "iteration"}
	if len(got)%2 != 0 {
		t.Fatalf("attrs length %d not even: %#v", len(got), got)
	}
	if len(got)/2 != len(wantKeys) {
		t.Fatalf("got %d pairs want %d: %#v", len(got)/2, len(wantKeys), got)
	}
	for i, key := range wantKeys {
		if got[i*2] != key {
			t.Fatalf("key[%d]=%v want %s", i, got[i*2], key)
		}
	}
	if got[1] != "agent" || got[13] != "r1" || got[15] != 2 {
		t.Fatalf("unexpected values: %#v", got)
	}
}

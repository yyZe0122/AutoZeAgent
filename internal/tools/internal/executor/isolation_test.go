package executor

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeIsolationConfigDefaults(t *testing.T) {
	got := normalizeIsolationConfig(IsolationConfig{})
	if got.Mode != IsolationModeAuto {
		t.Fatalf("mode = %q, want auto", got.Mode)
	}
	if got.MemoryMax != "512M" || got.MemorySwapMax != "0" || got.CPUQuota != "200%" || got.TasksMax != "256" {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestNormalizeIsolationConfigOff(t *testing.T) {
	got := normalizeIsolationConfig(IsolationConfig{Mode: "OFF", MemoryMax: "1G"})
	if got.Mode != IsolationModeOff {
		t.Fatalf("mode = %q, want off", got.Mode)
	}
	if got.MemoryMax != "1G" {
		t.Fatalf("memory max = %q", got.MemoryMax)
	}
}

func TestIsolationUnitName(t *testing.T) {
	name := isolationUnitName("call/abc.def")
	if !strings.HasPrefix(name, "autozeagent-tool-") || !strings.HasSuffix(name, ".scope") {
		t.Fatalf("unit name = %q", name)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(name, "autozeagent-tool-"), ".scope")
	if strings.ContainsAny(body, "./") {
		t.Fatalf("unit body not sanitized: %q", body)
	}
	empty := isolationUnitName("")
	if !strings.HasPrefix(empty, "autozeagent-tool-ephemeral-") {
		t.Fatalf("empty call id unit = %q", empty)
	}
}

func TestRuntimeMaxSec(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	if got := runtimeMaxSec(time.Time{}, now); got != 0 {
		t.Fatalf("zero deadline = %d", got)
	}
	deadline := now.Add(30 * time.Second)
	if got := runtimeMaxSec(deadline, now); got != 35 {
		t.Fatalf("runtime max = %d, want 35", got)
	}
	if got := runtimeMaxSec(now.Add(-time.Second), now); got != 1 {
		t.Fatalf("past deadline = %d, want 1", got)
	}
}

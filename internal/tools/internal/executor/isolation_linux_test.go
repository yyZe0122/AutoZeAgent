//go:build linux

package executor

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestProbeModeOff(t *testing.T) {
	rt := &isolationRuntime{
		config:   normalizeIsolationConfig(IsolationConfig{Mode: IsolationModeOff}),
		lookPath: func(string) (string, error) { return "/bin/systemd-run", nil },
		runCommand: func(context.Context, string, ...string) error {
			t.Fatal("probe should not run when mode is off")
			return nil
		},
	}
	status := rt.probe()
	if status.Mode != StatusProcessGroupOnly || status.Reason != "isolation mode off" {
		t.Fatalf("status = %+v", status)
	}
}

func TestProbeNoSystemdRun(t *testing.T) {
	rt := &isolationRuntime{
		config:   normalizeIsolationConfig(IsolationConfig{Mode: IsolationModeAuto}),
		lookPath: func(string) (string, error) { return "", errors.New("not found") },
		runCommand: func(context.Context, string, ...string) error {
			t.Fatal("runCommand should not be called")
			return nil
		},
	}
	// Force cgroup check path: if host has no cgroup v2, reason differs.
	status := rt.probe()
	if status.Mode != StatusProcessGroupOnly {
		t.Fatalf("mode = %q", status.Mode)
	}
	if status.Reason != "no_cgroup_v2" && status.Reason != "no_systemd_run" {
		t.Fatalf("reason = %q", status.Reason)
	}
}

func TestWrapCommandBuildsSystemdRun(t *testing.T) {
	rt := &isolationRuntime{
		config: normalizeIsolationConfig(IsolationConfig{
			Mode: IsolationModeAuto, MemoryMax: "64M", MemorySwapMax: "0", CPUQuota: "100%", TasksMax: "32",
		}),
		status: IsolationStatus{Mode: StatusSystemdScope, UserScope: true, SystemdRun: "/usr/bin/systemd-run"},
	}
	uid := uint32(1000)
	name, args, unit, cleanup, ok := rt.wrapCommand(context.Background(), Request{
		Command: "echo", Arguments: []string{"hi"}, CallID: "call-1",
	}, processPolicy{UID: &uid})
	if !ok {
		t.Fatal("expected wrap")
	}
	if name != "/usr/bin/systemd-run" {
		t.Fatalf("name = %q", name)
	}
	if unit != "autozeagent-tool-call-1.scope" {
		t.Fatalf("unit = %q", unit)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--user", "--scope", "--unit=autozeagent-tool-call-1.scope",
		"--property=MemoryMax=64M", "--uid=1000", "--", "echo", "hi",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q in %q", want, joined)
		}
	}
	if cleanup == nil {
		t.Fatal("cleanup nil")
	}
	cleanup()
}

func TestWrapCommandInactive(t *testing.T) {
	rt := &isolationRuntime{status: IsolationStatus{Mode: StatusProcessGroupOnly, Reason: "no_systemd_run"}}
	_, _, _, _, ok := rt.wrapCommand(context.Background(), Request{Command: "true"}, processPolicy{})
	if ok {
		t.Fatal("expected no wrap when degraded")
	}
}

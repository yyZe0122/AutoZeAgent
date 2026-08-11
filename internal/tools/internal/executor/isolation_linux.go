//go:build linux

package executor

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const cgroupControllersPath = "/sys/fs/cgroup/cgroup.controllers"

type isolationRuntime struct {
	config     IsolationConfig
	status     IsolationStatus
	lookPath   func(string) (string, error)
	runCommand func(context.Context, string, ...string) error
}

func newIsolationRuntime(config IsolationConfig) *isolationRuntime {
	rt := &isolationRuntime{
		config:   normalizeIsolationConfig(config),
		lookPath: exec.LookPath,
		runCommand: func(ctx context.Context, name string, args ...string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				msg := strings.TrimSpace(stderr.String())
				if msg != "" {
					return fmt.Errorf("%w: %s", err, msg)
				}
				return err
			}
			return nil
		},
	}
	rt.status = rt.probe()
	slog.Info("process isolation probe",
		"component", "executor",
		"operation", "isolation_probe",
		"result", rt.status.Mode,
		"reason", rt.status.Reason,
		"user_scope", rt.status.UserScope,
	)
	return rt
}

func (rt *isolationRuntime) Status() IsolationStatus {
	if rt == nil {
		return IsolationStatus{Mode: StatusUnsupported, Reason: "isolation unavailable"}
	}
	return rt.status
}

func (rt *isolationRuntime) probe() IsolationStatus {
	if rt.config.Mode == IsolationModeOff {
		return IsolationStatus{Mode: StatusProcessGroupOnly, Reason: "isolation mode off"}
	}
	if !fileExists(cgroupControllersPath) {
		return IsolationStatus{Mode: StatusProcessGroupOnly, Reason: "no_cgroup_v2"}
	}
	path, err := rt.lookPath("systemd-run")
	if err != nil || strings.TrimSpace(path) == "" {
		return IsolationStatus{Mode: StatusProcessGroupOnly, Reason: "no_systemd_run"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	unit := fmt.Sprintf("ymz-probe-%d.scope", os.Getpid())
	// Prefer --user when a user session is available; fall back to system.
	userArgs := []string{"--user", "--quiet", "--collect", "--scope", "--unit=" + unit, "--", "true"}
	if err := rt.runCommand(ctx, path, userArgs...); err == nil {
		return IsolationStatus{Mode: StatusSystemdScope, UserScope: true, SystemdRun: path}
	}
	sysArgs := []string{"--quiet", "--collect", "--scope", "--unit=" + unit, "--", "true"}
	if err := rt.runCommand(ctx, path, sysArgs...); err == nil {
		return IsolationStatus{Mode: StatusSystemdScope, UserScope: false, SystemdRun: path}
	}
	return IsolationStatus{Mode: StatusProcessGroupOnly, Reason: "probe_failed", SystemdRun: path}
}

// wrapCommand returns argv for systemd-run --scope when isolation is active.
// ok is false when the caller should exec the original command.
func (rt *isolationRuntime) wrapCommand(ctx context.Context, request Request, policy processPolicy) (name string, args []string, unit string, cleanup func(), ok bool) {
	if rt == nil || rt.status.Mode != StatusSystemdScope || strings.TrimSpace(rt.status.SystemdRun) == "" {
		return "", nil, "", func() {}, false
	}
	unit = isolationUnitName(request.CallID)
	args = make([]string, 0, 16+len(request.Arguments))
	if rt.status.UserScope {
		args = append(args, "--user")
	}
	args = append(args,
		"--quiet",
		"--collect",
		"--scope",
		"--unit="+unit,
		"--property=MemoryMax="+rt.config.MemoryMax,
		"--property=MemorySwapMax="+rt.config.MemorySwapMax,
		"--property=CPUQuota="+rt.config.CPUQuota,
		"--property=TasksMax="+rt.config.TasksMax,
	)
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		if sec := runtimeMaxSec(deadline, time.Now()); sec > 0 {
			args = append(args, "--property=RuntimeMaxSec="+strconv.Itoa(sec))
		}
	}
	if policy.UID != nil {
		args = append(args, "--uid="+strconv.FormatUint(uint64(*policy.UID), 10))
	}
	if policy.GID != nil {
		args = append(args, "--gid="+strconv.FormatUint(uint64(*policy.GID), 10))
	}
	args = append(args, "--", request.Command)
	args = append(args, request.Arguments...)
	cleanup = func() {
		rt.stopUnit(unit)
	}
	return rt.status.SystemdRun, args, unit, cleanup, true
}

func (rt *isolationRuntime) stopUnit(unit string) {
	if rt == nil || strings.TrimSpace(unit) == "" || rt.lookPath == nil {
		return
	}
	systemctl, err := rt.lookPath("systemctl")
	if err != nil {
		return
	}
	if rt.runCommand == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	args := []string{}
	if rt.status.UserScope {
		args = append(args, "--user")
	}
	args = append(args, "stop", unit)
	_ = rt.runCommand(ctx, systemctl, args...)
}

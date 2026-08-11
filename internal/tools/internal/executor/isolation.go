package executor

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// Isolation modes for process_exec / git subprocesses (P5b baseline).
const (
	IsolationModeAuto = "auto"
	IsolationModeOff  = "off"

	// StatusMode values reported after probe.
	StatusSystemdScope     = "systemd_scope"
	StatusProcessGroupOnly = "process_group_only"
	StatusUnsupported      = "unsupported"
)

// IsolationConfig controls Linux process isolation baseline (systemd scope + cgroup v2).
// Non-Linux platforms ignore these settings.
type IsolationConfig struct {
	// Mode is auto (default) or off.
	Mode string
	// MemoryMax is a systemd MemoryMax= value (default 512M).
	MemoryMax string
	// MemorySwapMax defaults to 0.
	MemorySwapMax string
	// CPUQuota defaults to 200%.
	CPUQuota string
	// TasksMax defaults to 256.
	TasksMax string
}

// IsolationStatus is the effective isolation after probe.
type IsolationStatus struct {
	// Mode is systemd_scope, process_group_only, or unsupported.
	Mode string
	// Reason explains degraded / unsupported cases.
	Reason string
	// UserScope is true when systemd-run --user must be used.
	UserScope bool
	// SystemdRun is the resolved path to systemd-run when Mode is systemd_scope.
	SystemdRun string
}

func normalizeIsolationConfig(config IsolationConfig) IsolationConfig {
	mode := strings.ToLower(strings.TrimSpace(config.Mode))
	if mode == "" {
		mode = IsolationModeAuto
	}
	if mode != IsolationModeAuto && mode != IsolationModeOff {
		mode = IsolationModeAuto
	}
	config.Mode = mode
	if strings.TrimSpace(config.MemoryMax) == "" {
		config.MemoryMax = "512M"
	}
	if strings.TrimSpace(config.MemorySwapMax) == "" {
		config.MemorySwapMax = "0"
	}
	if strings.TrimSpace(config.CPUQuota) == "" {
		config.CPUQuota = "200%"
	}
	if strings.TrimSpace(config.TasksMax) == "" {
		config.TasksMax = "256"
	}
	return config
}

var unitSafe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func isolationUnitName(callID string) string {
	id := strings.TrimSpace(callID)
	if id == "" {
		id = fmt.Sprintf("ephemeral-%d", time.Now().UnixNano())
	}
	id = unitSafe.ReplaceAllString(id, "-")
	if len(id) > 80 {
		id = id[:80]
	}
	id = strings.Trim(id, "-")
	if id == "" {
		id = "unknown"
	}
	return "ymz-tool-" + id + ".scope"
}

func runtimeMaxSec(deadline time.Time, now time.Time) int {
	if deadline.IsZero() {
		return 0
	}
	remaining := deadline.Sub(now)
	if remaining <= 0 {
		return 1
	}
	sec := int(remaining.Seconds()) + 5
	if sec < 1 {
		return 1
	}
	if sec > 86400 {
		return 86400
	}
	return sec
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

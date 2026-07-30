// Package daemonctl starts, stops, and ensures the unique local autozeagentd process.
// The CLI uses this so TUI/run work without a manually started daemon; stop is explicit only.
package daemonctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"autozeagent.local/autozeagent/internal/gatewayclient"
	"autozeagent.local/autozeagent/internal/platform/paths"
)

const (
	pidFilename     = "autozeagentd.pid"
	defaultReadyFor = 20 * time.Second
	pollInterval    = 100 * time.Millisecond
)

// Status describes whether the local gateway is healthy and any recorded PID.
type Status struct {
	Mode     paths.Mode `json:"mode"`
	Running  bool       `json:"running"`
	Healthy  bool       `json:"healthy"`
	PID      int        `json:"pid,omitempty"`
	PIDAlive bool       `json:"pid_alive,omitempty"`
	Message  string     `json:"message,omitempty"`
}

// Ensure makes the local gateway healthy for mode, starting autozeagentd if needed.
// It does not stop the daemon when the caller exits.
func Ensure(ctx context.Context, mode paths.Mode) error {
	if ctx == nil {
		return errors.New("daemonctl ensure context is required")
	}
	layout, err := paths.Resolve(mode)
	if err != nil {
		return err
	}
	if healthy, _ := probeHealth(ctx, layout.RuntimeDir); healthy {
		return nil
	}
	if err := startDetached(mode, layout); err != nil {
		// Another client may have won the race; wait for readiness either way.
		if !errors.Is(err, errAlreadyRunning) {
			// Still try wait: gateway might be coming up from a concurrent start.
			if waitErr := waitHealthy(ctx, layout.RuntimeDir); waitErr == nil {
				return nil
			}
			return err
		}
	}
	if err := waitHealthy(ctx, layout.RuntimeDir); err != nil {
		return err
	}
	return nil
}

// Start starts autozeagentd if the gateway is not already healthy.
func Start(ctx context.Context, mode paths.Mode) error {
	if ctx == nil {
		return errors.New("daemonctl start context is required")
	}
	layout, err := paths.Resolve(mode)
	if err != nil {
		return err
	}
	if healthy, _ := probeHealth(ctx, layout.RuntimeDir); healthy {
		return nil
	}
	if err := startDetached(mode, layout); err != nil {
		if errors.Is(err, errAlreadyRunning) {
			return waitHealthy(ctx, layout.RuntimeDir)
		}
		return err
	}
	return waitHealthy(ctx, layout.RuntimeDir)
}

// Stop signals the unique daemon for mode to shut down and waits until the gateway is gone.
func Stop(ctx context.Context, mode paths.Mode) error {
	if ctx == nil {
		return errors.New("daemonctl stop context is required")
	}
	layout, err := paths.Resolve(mode)
	if err != nil {
		return err
	}
	pid, pidErr := readPID(layout.RuntimeDir)
	signaled := false
	if pidErr == nil && pid > 0 {
		if err := signalStop(pid); err == nil {
			signaled = true
		} else if !errors.Is(err, os.ErrProcessDone) && !isNoSuchProcess(err) {
			// Fall through to wait/cleanup; process may already be dead.
			_ = err
		}
	}
	if !signaled {
		// No usable PID: if gateway is already down, treat as success.
		if healthy, _ := probeHealth(ctx, layout.RuntimeDir); !healthy {
			_ = removePID(layout.RuntimeDir)
			return nil
		}
		return errors.New("daemon is running but pid file is missing or stale; stop autozeagentd manually")
	}
	deadline := time.Now().Add(defaultReadyFor)
	for {
		if healthy, _ := probeHealth(ctx, layout.RuntimeDir); !healthy {
			if !processAlive(pid) {
				_ = removePID(layout.RuntimeDir)
				return nil
			}
		}
		if time.Now().After(deadline) {
			if processAlive(pid) {
				return fmt.Errorf("daemon pid %d did not stop within %s", pid, defaultReadyFor)
			}
			_ = removePID(layout.RuntimeDir)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// Inspect returns runtime status for mode.
func Inspect(ctx context.Context, mode paths.Mode) (Status, error) {
	if ctx == nil {
		return Status{}, errors.New("daemonctl status context is required")
	}
	layout, err := paths.Resolve(mode)
	if err != nil {
		return Status{}, err
	}
	status := Status{Mode: mode}
	pid, pidErr := readPID(layout.RuntimeDir)
	if pidErr == nil && pid > 0 {
		status.PID = pid
		status.PIDAlive = processAlive(pid)
	}
	healthy, healthErr := probeHealth(ctx, layout.RuntimeDir)
	status.Healthy = healthy
	status.Running = healthy || status.PIDAlive
	switch {
	case healthy:
		status.Message = "gateway healthy"
	case status.PIDAlive:
		status.Message = "process alive; gateway not ready"
	case healthErr != nil:
		status.Message = healthErr.Error()
	default:
		status.Message = "daemon not running"
	}
	return status, nil
}

var errAlreadyRunning = errors.New("local gateway is already running")

func startDetached(mode paths.Mode, layout paths.Layout) error {
	if err := os.MkdirAll(layout.RuntimeDir, 0o750); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	if err := os.MkdirAll(layout.LogDir, 0o750); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	// Fast path: endpoint already answering.
	probeCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if healthy, _ := probeHealth(probeCtx, layout.RuntimeDir); healthy {
		return errAlreadyRunning
	}

	bin, err := lookPathDaemon()
	if err != nil {
		return err
	}
	logPath := filepath.Join(layout.LogDir, "autozeagentd.stdout.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("open daemon stdout log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(bin, "--mode", string(mode))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Dir = layout.DataDir
	// Client cwd is passed so first-run EnsureConfig can migrate project-local provider config
	// even though the daemon process itself runs with DataDir as working directory.
	env := os.Environ()
	if cwd, err := os.Getwd(); err == nil && strings.TrimSpace(cwd) != "" {
		env = append(env, "AUTOZEAGENT_CLIENT_CWD="+cwd)
	}
	cmd.Env = env
	configureDetached(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start autozeagentd: %w", err)
	}
	// Detach: do not wait; child writes its own pid file after gateway binds.
	_ = cmd.Process.Release()
	return nil
}

func waitHealthy(ctx context.Context, runtimeDir string) error {
	deadline := time.Now().Add(defaultReadyFor)
	var lastErr error
	for {
		healthy, err := probeHealth(ctx, runtimeDir)
		if healthy {
			return nil
		}
		if err != nil {
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr == nil {
				lastErr = errors.New("timed out")
			}
			return fmt.Errorf("wait for local gateway: %w", lastErr)
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("wait for local gateway: %w", lastErr)
			}
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func probeHealth(ctx context.Context, runtimeDir string) (bool, error) {
	client, err := gatewayclient.NewFromRuntimeDir(runtimeDir)
	if err != nil {
		return false, err
	}
	health, err := client.Health(ctx)
	if err != nil {
		return false, err
	}
	return health.OK, nil
}

func lookPathDaemon() (string, error) {
	name := "autozeagentd" + exeSuffix()
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		// When invoked via aze → autozeagent symlink, Executable may resolve to autozeagent.
		if resolved, err := filepath.EvalSymlinks(self); err == nil {
			candidate = filepath.Join(filepath.Dir(resolved), name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("find autozeagentd: put it on PATH or next to the CLI binary: %w", err)
	}
	return path, nil
}

// WritePID records the current process id for stop/status (called by autozeagentd).
func WritePID(runtimeDir string) error {
	if strings.TrimSpace(runtimeDir) == "" {
		return errors.New("runtime directory is required")
	}
	if err := os.MkdirAll(runtimeDir, 0o750); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	path := pidPath(runtimeDir)
	tmp := path + ".tmp"
	content := []byte(strconv.Itoa(os.Getpid()) + "\n")
	if err := os.WriteFile(tmp, content, 0o640); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publish pid file: %w", err)
	}
	return nil
}

// RemovePID deletes the pid file if it still points at this process.
func RemovePID(runtimeDir string) error {
	pid, err := readPID(runtimeDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if pid != os.Getpid() {
		return nil
	}
	return removePID(runtimeDir)
}

func readPID(runtimeDir string) (int, error) {
	raw, err := os.ReadFile(pidPath(runtimeDir))
	if err != nil {
		return 0, err
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return 0, errors.New("pid file is empty")
	}
	pid, err := strconv.Atoi(text)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid file contents %q", text)
	}
	return pid, nil
}

func removePID(runtimeDir string) error {
	if err := os.Remove(pidPath(runtimeDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func pidPath(runtimeDir string) string {
	return filepath.Join(filepath.Clean(runtimeDir), pidFilename)
}

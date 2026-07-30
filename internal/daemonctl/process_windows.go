//go:build windows

package daemonctl

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func exeSuffix() string { return ".exe" }

func configureDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup,
		HideWindow:    true,
	}
}

func signalStop(pid int) error {
	// Detached Windows processes do not receive console CTRL_BREAK reliably.
	// Force-terminate the process tree (daemon is single-process).
	killer := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid), "/T", "/F")
	if out, err := killer.CombinedOutput(); err != nil {
		return fmt.Errorf("taskkill: %w (%s)", err, string(out))
	}
	return nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows FindProcess always succeeds; probe with a zero-timeout wait via signal is unsupported.
	// Use OpenProcess semantics via duplicate handle check: Signal is not implemented — try Kill with 0.
	// os.FindProcess + syscall: send signal 0 is not available; use process handle wait.
	const stillActive = 259 // STILL_ACTIVE
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)
	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}

func isNoSuchProcess(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ERROR_INVALID_PARAMETER)
}

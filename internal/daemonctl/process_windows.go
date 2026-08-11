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
	// FindProcess always succeeds on Windows; probe via OpenProcess + exit code.
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
	// ERROR_INVALID_PARAMETER = 87 (invalid PID / handle semantics on Windows).
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.Errno(87))
}

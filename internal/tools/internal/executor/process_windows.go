//go:build windows

package executor

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createNewProcessGroup = 0x00000200

type processGroup struct {
	job     windows.Handle
	process windows.Handle
}

func prepareProcess(command *exec.Cmd, _ processPolicy) (*processGroup, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	information := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	information.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&information)),
		uint32(unsafe.Sizeof(information)),
	); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
	return &processGroup{job: job}, nil
}

func (g *processGroup) Started(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return errors.New("process is not running")
	}
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(command.Process.Pid),
	)
	if err != nil {
		return err
	}
	if err := windows.AssignProcessToJobObject(g.job, process); err != nil {
		windows.CloseHandle(process)
		return err
	}
	g.process = process
	return nil
}

func (g *processGroup) Terminate(command *exec.Cmd) error {
	if g != nil && g.job != 0 {
		if err := windows.TerminateJobObject(g.job, 1); err == nil {
			return nil
		}
	}
	if command == nil || command.Process == nil {
		return errors.New("process is not running")
	}
	killer := exec.Command("taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F")
	if output, err := killer.CombinedOutput(); err != nil {
		if killErr := command.Process.Kill(); killErr != nil {
			return fmt.Errorf("taskkill: %v: %s; direct kill: %w", err, output, killErr)
		}
	}
	return nil
}

func (g *processGroup) Close() {
	if g == nil {
		return
	}
	if g.process != 0 {
		_ = windows.CloseHandle(g.process)
		g.process = 0
	}
	if g.job != 0 {
		_ = windows.CloseHandle(g.job)
		g.job = 0
	}
}

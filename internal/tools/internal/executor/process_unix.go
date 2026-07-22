//go:build !windows

package executor

import (
	"errors"
	"os/exec"
	"syscall"
)

type processGroup struct{}

func prepareProcess(command *exec.Cmd, policy processPolicy) (*processGroup, error) {
	attributes := &syscall.SysProcAttr{Setpgid: true}
	if policy.UID != nil || policy.GID != nil {
		credential := &syscall.Credential{}
		if policy.UID != nil {
			credential.Uid = *policy.UID
		}
		if policy.GID != nil {
			credential.Gid = *policy.GID
		}
		attributes.Credential = credential
	}
	command.SysProcAttr = attributes
	return &processGroup{}, nil
}

func (*processGroup) Started(*exec.Cmd) error { return nil }

func (*processGroup) Terminate(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return errors.New("process is not running")
	}
	return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
}

func (*processGroup) Close() {}

//go:build !windows

package localshell

import (
	"errors"
	"os/exec"
	"syscall"
)

func procSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessTree(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return errors.New("process is not running")
	}
	return syscall.Kill(-cmd.Process.Pid, sig)
}

func killProcessGroupSoft(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}

func killProcessGroupHard(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}

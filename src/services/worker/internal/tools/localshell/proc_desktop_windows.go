//go:build windows

package localshell

import (
	"errors"
	"os/exec"
	"strconv"
	"syscall"
)

func procSysProcAttr() *syscall.SysProcAttr {
	return nil
}

func terminateProcessTree(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return errors.New("process is not running")
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	if sig == syscall.SIGKILL {
		return exec.Command("taskkill.exe", "/PID", pid, "/T", "/F").Run()
	}
	return exec.Command("taskkill.exe", "/PID", pid, "/T").Run()
}

func killProcessGroupSoft(pid int) error {
	return exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T").Run()
}

func killProcessGroupHard(pid int) error {
	return exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}

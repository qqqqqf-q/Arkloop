//go:build windows

package plugincontrib

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func configureDaemonCommand(_ *exec.Cmd) {}

func killDaemonProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	output, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
	return err == nil && strings.Contains(string(output), strconv.Itoa(pid))
}

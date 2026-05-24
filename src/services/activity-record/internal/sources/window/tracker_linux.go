//go:build linux

package window

import (
	"os/exec"
	"strconv"
	"strings"
)

func ActiveWindow() (WindowInfo, error) {
	var info WindowInfo
	// xdotool for X11
	nameOut, err := exec.Command("xdotool", "getactivewindow", "getwindowname").Output()
	if err != nil {
		return info, err
	}
	info.WindowTitle = strings.TrimSpace(string(nameOut))

	pidOut, err := exec.Command("xdotool", "getactivewindow", "getwindowpid").Output()
	if err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(pidOut))); err == nil {
			info.PID = pid
		}
	}

	classOut, err := exec.Command("xdotool", "getactivewindow", "getwindowclassname").Output()
	if err == nil {
		info.App = strings.TrimSpace(string(classOut))
	}
	if info.App == "" {
		info.App = info.WindowTitle
	}
	return info, nil
}

func idleSeconds() (float64, error) {
	out, err := exec.Command("xprintidle").Output()
	if err != nil {
		return 0, err
	}
	ms, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, err
	}
	return ms / 1000.0, nil
}

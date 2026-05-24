//go:build windows

package window

import (
	"syscall"
	"unsafe"
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW      = user32.NewProc("GetWindowTextW")
	procGetWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
	procGetLastInputInfo    = user32.NewProc("GetLastInputInfo")
	procGetTickCount64      = kernel32.NewProc("GetTickCount64")
)

type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

func ActiveWindow() (WindowInfo, error) {
	var info WindowInfo
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return info, nil
	}

	buf := make([]uint16, 512)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	info.WindowTitle = syscall.UTF16ToString(buf)
	info.App = info.WindowTitle

	var pid uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	info.PID = int(pid)

	return info, nil
}

func idleSeconds() (float64, error) {
	lii := lastInputInfo{cbSize: uint32(unsafe.Sizeof(lastInputInfo{}))}
	ret, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii)))
	if ret == 0 {
		return 0, err
	}
	tick, _, _ := procGetTickCount64.Call()
	idleMs := uint64(tick) - uint64(lii.dwTime)
	return float64(idleMs) / 1000.0, nil
}

//go:build windows

package mouse

import (
	"context"
	"syscall"
	"unsafe"
)

var (
	user32                    = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookExW     = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx        = user32.NewProc("CallNextHookEx")
	procGetMessageW           = user32.NewProc("GetMessageW")
	procUnhookWindowsHookEx   = user32.NewProc("UnhookWindowsHookEx")
)

const whMouseLL = 14

const (
	wmLButtonDown  = 0x0201
	wmRButtonDown  = 0x0204
	wmMButtonDown  = 0x0207
	wmMouseWheel   = 0x020A
	wmMouseHWheel  = 0x020E
)

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

var globalCounters *counters

func hookProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 {
		switch wParam {
		case wmLButtonDown, wmRButtonDown, wmMButtonDown:
			globalCounters.clicks.Add(1)
		case wmMouseWheel, wmMouseHWheel:
			globalCounters.scrolls.Add(1)
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

func listenMouse(ctx context.Context, c *counters) error {
	globalCounters = c

	hook, _, err := procSetWindowsHookExW.Call(
		whMouseLL,
		syscall.NewCallback(hookProc),
		0, 0,
	)
	if hook == 0 {
		return err
	}
	defer procUnhookWindowsHookEx.Call(hook)

	go func() {
		<-ctx.Done()
		procUnhookWindowsHookEx.Call(hook)
	}()

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if ret == 0 || ctx.Err() != nil {
			return nil
		}
	}
}

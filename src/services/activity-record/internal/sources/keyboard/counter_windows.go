//go:build windows

package keyboard

import (
	"context"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookExW = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx    = user32.NewProc("CallNextHookEx")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
)

const whKeyboardLL = 13

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

var globalCount *atomic.Int64

func hookProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 && (wParam == 0x100 || wParam == 0x104) {
		globalCount.Add(1)
	}
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

func listenKeystrokes(ctx context.Context, count *atomic.Int64) error {
	globalCount = count

	hook, _, err := procSetWindowsHookExW.Call(
		whKeyboardLL,
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

//go:build windows

package mouse

import (
	"context"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
)

const whMouseLL = 14

const (
	wmLButtonDown = 0x0201
	wmRButtonDown = 0x0204
	wmMButtonDown = 0x0207
	wmMouseWheel  = 0x020A
	wmMouseHWheel = 0x020E
	wmMouseMove   = 0x0200
)

type msllHookStruct struct {
	pt          struct{ x, y int32 }
	mouseData   uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

var globalAgg *mouseAgg

func hookProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 && globalAgg != nil {
		info := (*msllHookStruct)(unsafe.Pointer(lParam))

		switch wParam {
		case wmLButtonDown, wmRButtonDown, wmMButtonDown:
			globalAgg.clicks++
			globalAgg.pathEvents = append(globalAgg.pathEvents, mousePathEvent{
				at: time.Now(),
				x:  float64(info.pt.x),
				y:  float64(info.pt.y),
			})
		case wmMouseWheel, wmMouseHWheel:
			globalAgg.scrolls++
		case wmMouseMove:
			globalAgg.pathEvents = append(globalAgg.pathEvents, mousePathEvent{
				at: time.Now(),
				x:  float64(info.pt.x),
				y:  float64(info.pt.y),
			})
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

func listenMouse(ctx context.Context, agg *mouseAgg) error {
	globalAgg = agg

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

	var m struct {
		hwnd    uintptr
		message uint32
		wParam  uintptr
		lParam  uintptr
		time    uint32
		pt      struct{ x, y int32 }
	}
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if ret == 0 || ctx.Err() != nil {
			return nil
		}
	}
}

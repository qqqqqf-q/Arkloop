//go:build windows

package keyboard

import (
	"context"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                 = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookExW  = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx     = user32.NewProc("CallNextHookEx")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procToUnicode          = user32.NewProc("ToUnicode")
	procGetKeyboardState   = user32.NewProc("GetKeyboardState")
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

var keyEventBuf = make(chan keyEvent, 512)

func hookProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 && (wParam == 0x100 || wParam == 0x104) {
		vkCode := uint32(wParam)
		scanCode := uint32((lParam >> 16) & 0xFF)
		isBackspace := vkCode == 0x08
		isEnter := vkCode == 0x0D
		isTab := vkCode == 0x09

		chars := ""
		if !isBackspace && !isEnter && !isTab {
			var keyState [256]byte
			procGetKeyboardState.Call(uintptr(unsafe.Pointer(&keyState[0])))
			var buf [4]uint16
			ret, _, _ := procToUnicode.Call(
				uintptr(vkCode),
				uintptr(scanCode),
				uintptr(unsafe.Pointer(&keyState[0])),
				uintptr(unsafe.Pointer(&buf[0])),
				4, 0,
			)
			if ret > 0 && ret <= 4 {
				runes := make([]rune, int(ret))
				for i := 0; i < int(ret); i++ {
					runes[i] = rune(buf[i])
				}
				chars = string(runes)
			}
		}

		select {
		case keyEventBuf <- keyEvent{
			chars:       chars,
			isBackspace: isBackspace,
			isEnter:     isEnter,
			isTab:       isTab,
		}:
		default:
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

func listenKeystrokes(ctx context.Context, session *typingSession) error {
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

	go func() {
		var m msg
		for {
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if ret == 0 || ctx.Err() != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case kev := <-keyEventBuf:
			session.feed(kev)
		case <-ticker.C:
			// drain any remaining events
			for {
				select {
				case kev := <-keyEventBuf:
					session.feed(kev)
				default:
					goto done
				}
			}
		done:
		}
	}
}

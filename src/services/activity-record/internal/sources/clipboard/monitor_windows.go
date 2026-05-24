//go:build windows

package clipboard

import (
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procOpenClipboard   = user32.NewProc("OpenClipboard")
	procCloseClipboard  = user32.NewProc("CloseClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
)

const cfUnicodeText = 13

func clipboardText() (string, error) {
	ret, _, _ := procIsClipboardFormatAvailable.Call(cfUnicodeText)
	if ret == 0 {
		return "", nil
	}

	ret, _, err := procOpenClipboard.Call(0)
	if ret == 0 {
		return "", err
	}
	defer procCloseClipboard.Call()

	h, _, err := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", err
	}

	ptr, _, err := procGlobalLock.Call(h)
	if ptr == 0 {
		return "", err
	}
	defer procGlobalUnlock.Call(h)

	text := syscall.UTF16ToString((*[1 << 20]uint16)(unsafe.Pointer(ptr))[:])
	return strings.TrimRight(text, "\r\n"), nil
}

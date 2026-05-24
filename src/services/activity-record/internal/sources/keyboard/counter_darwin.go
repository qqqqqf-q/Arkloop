//go:build darwin

package keyboard

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
#include <stdint.h>

static volatile int64_t keyCount = 0;

static CGEventRef keyCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *refcon) {
	(void)proxy;
	(void)event;
	(void)refcon;
	if (type == kCGEventKeyDown) {
		__sync_add_and_fetch(&keyCount, 1);
	}
	return event;
}

static int64_t swapKeyCount() {
	return __sync_lock_test_and_set(&keyCount, 0);
}

static int startKeyTap() {
	CGEventMask mask = CGEventMaskBit(kCGEventKeyDown);
	CFMachPortRef tap = CGEventTapCreate(
		kCGSessionEventTap,
		kCGHeadInsertEventTap,
		kCGEventTapOptionListenOnly,
		mask,
		keyCallback,
		NULL
	);
	if (!tap) return -1;

	CFRunLoopSourceRef src = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0);
	CFRunLoopAddSource(CFRunLoopGetCurrent(), src, kCFRunLoopCommonModes);
	CGEventTapEnable(tap, true);
	CFRunLoopRun();
	CFRelease(src);
	CFRelease(tap);
	return 0;
}
*/
import "C"
import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

func listenKeystrokes(ctx context.Context, count *atomic.Int64) error {
	done := make(chan error, 1)

	go func() {
		ret := C.startKeyTap()
		if ret != 0 {
			done <- fmt.Errorf("CGEventTapCreate failed (accessibility permission required)")
		} else {
			done <- nil
		}
	}()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			C.CFRunLoopStop(C.CFRunLoopGetCurrent())
			return nil
		case <-ticker.C:
			n := int64(C.swapKeyCount())
			if n > 0 {
				count.Add(n)
			}
		}
	}
}

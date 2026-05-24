//go:build darwin

package mouse

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
#include <stdint.h>

static volatile int64_t clickCount = 0;
static volatile int64_t scrollCount = 0;

static CGEventRef mouseCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *refcon) {
	(void)proxy;
	(void)event;
	(void)refcon;
	switch (type) {
	case kCGEventLeftMouseDown:
	case kCGEventRightMouseDown:
	case kCGEventOtherMouseDown:
		__sync_add_and_fetch(&clickCount, 1);
		break;
	case kCGEventScrollWheel:
		__sync_add_and_fetch(&scrollCount, 1);
		break;
	default:
		break;
	}
	return event;
}

static int64_t swapClickCount() {
	return __sync_lock_test_and_set(&clickCount, 0);
}

static int64_t swapScrollCount() {
	return __sync_lock_test_and_set(&scrollCount, 0);
}

static int startMouseTap() {
	CGEventMask mask =
		CGEventMaskBit(kCGEventLeftMouseDown) |
		CGEventMaskBit(kCGEventRightMouseDown) |
		CGEventMaskBit(kCGEventOtherMouseDown) |
		CGEventMaskBit(kCGEventScrollWheel);
	CFMachPortRef tap = CGEventTapCreate(
		kCGSessionEventTap,
		kCGHeadInsertEventTap,
		kCGEventTapOptionListenOnly,
		mask,
		mouseCallback,
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
	"time"
)

func listenMouse(ctx context.Context, c *counters) error {
	done := make(chan error, 1)

	go func() {
		ret := C.startMouseTap()
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
			clicks := int64(C.swapClickCount())
			scrolls := int64(C.swapScrollCount())
			if clicks > 0 {
				c.clicks.Add(clicks)
			}
			if scrolls > 0 {
				c.scrolls.Add(scrolls)
			}
		}
	}
}

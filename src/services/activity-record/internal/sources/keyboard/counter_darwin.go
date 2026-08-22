//go:build darwin

package keyboard

/*
#cgo LDFLAGS: -framework CoreGraphics -framework Carbon
#include <CoreGraphics/CoreGraphics.h>
#include <Carbon/Carbon.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <mach/mach_time.h>

#define KEY_RING_SIZE 2048
#define MAX_CHARS 8

typedef struct {
	uint64_t  timestamp_ms;
	UniChar   chars[MAX_CHARS];
	uint8_t   char_count;
	uint16_t  keycode;
	CGEventFlags flags;
	uint8_t   is_backspace;
	uint8_t   is_enter;
	uint8_t   is_tab;
	uint8_t   _pad[1];
} key_event_t;

key_event_t key_ring[KEY_RING_SIZE] = {0};
static volatile int64_t key_write_pos = 0;

static uint64_t now_ms(void) {
	static mach_timebase_info_data_t tb = {0};
	if (tb.denom == 0) mach_timebase_info(&tb);
	uint64_t ns = mach_absolute_time() * tb.numer / tb.denom;
	return ns / 1000000;
}

static CGEventRef keyCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *refcon) {
	(void)proxy;
	(void)refcon;

	if (type != kCGEventKeyDown) return event;

	int64_t idx = __sync_fetch_and_add(&key_write_pos, 1) % KEY_RING_SIZE;
	key_event_t *ev = &key_ring[idx];

	ev->timestamp_ms = now_ms();
	ev->keycode      = (uint16_t)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);
	ev->flags        = CGEventGetFlags(event);

	UniChar buf[MAX_CHARS] = {0};
	UniCharCount actual = 0;
	CGEventKeyboardGetUnicodeString(event, MAX_CHARS, &actual, buf);
	ev->char_count = (uint8_t)(actual < MAX_CHARS ? actual : MAX_CHARS);
	memcpy(ev->chars, buf, ev->char_count * sizeof(UniChar));

	ev->is_backspace = (ev->keycode == 51);
	ev->is_enter     = (ev->keycode == 36 || ev->keycode == 76);
	ev->is_tab       = (ev->keycode == 48);

	return event;
}

static key_event_t* keyRingElement(int64_t idx) {
	return &key_ring[idx % KEY_RING_SIZE];
}

static int64_t readKeyRingPos(void) {
	return __sync_fetch_and_add(&key_write_pos, 0);
}

static int startKeyTap(void) {
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
	"time"
)

func drainRing(lastPos *int64) []keyEvent {
	writePos := int64(C.readKeyRingPos())
	if writePos <= *lastPos {
		return nil
	}
	count := writePos - *lastPos
	if count > C.KEY_RING_SIZE {
		count = C.KEY_RING_SIZE
	}
	out := make([]keyEvent, 0, count)
	for i := *lastPos; i < writePos; i++ {
		idx := C.int64_t(i % C.KEY_RING_SIZE)
		// Use a C helper to get a real C pointer, not a Go stack copy.
		cev := C.keyRingElement(idx)
		var chars string
		if cev.char_count > 0 {
			runes := make([]rune, 0, cev.char_count)
			for j := C.uint8_t(0); j < cev.char_count; j++ {
				runes = append(runes, rune(cev.chars[j]))
			}
			chars = string(runes)
		}
		kev := keyEvent{
			timestampMs: uint64(cev.timestamp_ms),
			chars:       chars,
			isBackspace: cev.is_backspace == 1,
			isEnter:     cev.is_enter == 1,
			isTab:       cev.is_tab == 1,
		}
		if kev.chars != "" || kev.isBackspace || kev.isEnter || kev.isTab {
			out = append(out, kev)
		}
	}
	*lastPos = writePos
	return out
}

func listenKeystrokes(ctx context.Context, session *typingSession) error {
	done := make(chan error, 1)

	go func() {
		ret := C.startKeyTap()
		if ret != 0 {
			done <- fmt.Errorf("CGEventTapCreate failed (accessibility permission required)")
		} else {
			done <- nil
		}
	}()

	var lastRingPos int64
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			C.CFRunLoopStop(C.CFRunLoopGetCurrent())
			return nil
		case <-ticker.C:
			for _, kev := range drainRing(&lastRingPos) {
				session.feed(kev)
			}
		}
	}
}

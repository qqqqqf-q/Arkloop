//go:build darwin

package mouse

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
#include <stdint.h>
#include <mach/mach_time.h>

#define MOUSE_RING_SIZE 4096

typedef struct {
	uint64_t timestamp_ms;
	double   x;
	double   y;
	uint8_t  event_type; // 0=move, 1=left_down, 2=right_down, 3=other_down, 4=scroll
	uint8_t  _pad[7];
} mouse_event_t;

mouse_event_t mouse_ring[MOUSE_RING_SIZE] = {0};
volatile int64_t mouse_write_pos = 0;

static uint64_t now_ms(void) {
	static mach_timebase_info_data_t tb = {0};
	if (tb.denom == 0) mach_timebase_info(&tb);
	uint64_t ns = mach_absolute_time() * tb.numer / tb.denom;
	return ns / 1000000;
}

static CGEventRef mouseCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *refcon) {
	(void)proxy;
	(void)refcon;

	CGPoint loc = CGEventGetLocation(event);
	int64_t idx = __sync_fetch_and_add(&mouse_write_pos, 1) % MOUSE_RING_SIZE;
	mouse_event_t *ev = &mouse_ring[idx];

	ev->timestamp_ms = now_ms();
	ev->x = loc.x;
	ev->y = loc.y;

	switch (type) {
	case kCGEventMouseMoved:
	case kCGEventLeftMouseDragged:
	case kCGEventRightMouseDragged:
	case kCGEventOtherMouseDragged:
		ev->event_type = 0; // move
		break;
	case kCGEventLeftMouseDown:
		ev->event_type = 1;
		break;
	case kCGEventRightMouseDown:
		ev->event_type = 2;
		break;
	case kCGEventOtherMouseDown:
		ev->event_type = 3;
		break;
	case kCGEventScrollWheel:
		ev->event_type = 4;
		break;
	default:
		break;
	}

	return event;
}

static int64_t readMouseRingPos(void) {
	return __sync_fetch_and_add(&mouse_write_pos, 0);
}

static int startMouseTap(void) {
	CGEventMask mask =
		CGEventMaskBit(kCGEventLeftMouseDown) |
		CGEventMaskBit(kCGEventRightMouseDown) |
		CGEventMaskBit(kCGEventOtherMouseDown) |
		CGEventMaskBit(kCGEventScrollWheel) |
		CGEventMaskBit(kCGEventMouseMoved) |
		CGEventMaskBit(kCGEventLeftMouseDragged) |
		CGEventMaskBit(kCGEventRightMouseDragged) |
		CGEventMaskBit(kCGEventOtherMouseDragged);
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
	"unsafe"
)

// rawMouseEvent as captured from CGEventTap.
type rawMouseEvent struct {
	timestampMs uint64
	x, y        float64
	eventType   uint8 // 0=move, 1=left, 2=right, 3=other, 4=scroll
}

func drainMouseRing(lastPos *int64) []rawMouseEvent {
	writePos := int64(C.readMouseRingPos())
	if writePos <= *lastPos {
		return nil
	}
	count := writePos - *lastPos
	if count > C.MOUSE_RING_SIZE {
		count = C.MOUSE_RING_SIZE
	}
	out := make([]rawMouseEvent, 0, count)
	for i := *lastPos; i < writePos; i++ {
		idx := i % C.MOUSE_RING_SIZE
		cev := (*C.mouse_event_t)(unsafe.Pointer(&C.mouse_ring[idx]))
		out = append(out, rawMouseEvent{
			timestampMs: uint64(cev.timestamp_ms),
			x:           float64(cev.x),
			y:           float64(cev.y),
			eventType:   uint8(cev.event_type),
		})
	}
	*lastPos = writePos
	return out
}

// mouseAgg holds aggregated mouse activity for a 30s window.
type mouseAgg struct {
	clicks     int
	scrolls    int
	pathEvents []mousePathEvent
}

type mousePathEvent struct {
	at time.Time
	x  float64
	y  float64
}

func listenMouse(ctx context.Context, agg *mouseAgg) error {
	done := make(chan error, 1)

	go func() {
		ret := C.startMouseTap()
		if ret != 0 {
			done <- fmt.Errorf("CGEventTapCreate failed (accessibility permission required)")
		} else {
			done <- nil
		}
	}()

	var lastRingPos int64
	// For subsampling moves.
	var lastMoveX, lastMoveY float64
	hasLastMove := false
	pollInterval := 200 * time.Millisecond

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			C.CFRunLoopStop(C.CFRunLoopGetCurrent())
			return nil
		case <-ticker.C:
			for _, ev := range drainMouseRing(&lastRingPos) {
				switch ev.eventType {
				case 1, 2, 3: // clicks
					agg.clicks++
					// Also record click location as a path point.
					agg.pathEvents = append(agg.pathEvents, mousePathEvent{
						at: time.UnixMilli(int64(ev.timestampMs)),
						x:  ev.x, y: ev.y,
					})
				case 4: // scroll
					agg.scrolls++
				case 0: // move/drag
					// Subsampling: record if first point, moved > 40px, or direction changed.
					if !hasLastMove {
						hasLastMove = true
						lastMoveX, lastMoveY = ev.x, ev.y
						agg.pathEvents = append(agg.pathEvents, mousePathEvent{
							at: time.UnixMilli(int64(ev.timestampMs)),
							x:  ev.x, y: ev.y,
						})
						continue
					}
					dx := ev.x - lastMoveX
					dy := ev.y - lastMoveY
					dist := dx*dx + dy*dy
					if dist >= 1600 { // 40*40 = 1600
						lastMoveX, lastMoveY = ev.x, ev.y
						agg.pathEvents = append(agg.pathEvents, mousePathEvent{
							at: time.UnixMilli(int64(ev.timestampMs)),
							x:  ev.x, y: ev.y,
						})
					}
				}
			}
		}
	}
}

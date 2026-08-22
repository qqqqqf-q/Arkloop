//go:build darwin

package security

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

#define SEC_RING_SIZE 64

typedef struct {
	int is_lock;
	int valid;
} sec_event_t;

sec_event_t sec_ring[SEC_RING_SIZE] = {0};
volatile int64_t sec_write_pos = 0;

static void onScreenLocked(CFNotificationCenterRef center, void *observer,
                           CFStringRef name, const void *object,
                           CFDictionaryRef userInfo) {
	(void)center; (void)observer; (void)name; (void)object; (void)userInfo;
	int64_t idx = __sync_fetch_and_add(&sec_write_pos, 1) % SEC_RING_SIZE;
	sec_ring[idx].is_lock = 1;
	sec_ring[idx].valid = 1;
}

static void onScreenUnlocked(CFNotificationCenterRef center, void *observer,
                             CFStringRef name, const void *object,
                             CFDictionaryRef userInfo) {
	(void)center; (void)observer; (void)name; (void)object; (void)userInfo;
	int64_t idx = __sync_fetch_and_add(&sec_write_pos, 1) % SEC_RING_SIZE;
	sec_ring[idx].is_lock = 0;
	sec_ring[idx].valid = 1;
}

static int64_t readSecRingPos(void) {
	return __sync_fetch_and_add(&sec_write_pos, 0);
}

static int startSecObserver(void) {
	CFNotificationCenterRef distCenter =
		CFNotificationCenterGetDistributedCenter();
	if (!distCenter) return -1;

	CFNotificationCenterAddObserver(
		distCenter, NULL, onScreenLocked,
		CFSTR("com.apple.screenIsLocked"), NULL,
		CFNotificationSuspensionBehaviorDeliverImmediately);

	CFNotificationCenterAddObserver(
		distCenter, NULL, onScreenUnlocked,
		CFSTR("com.apple.screenIsUnlocked"), NULL,
		CFNotificationSuspensionBehaviorDeliverImmediately);

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

type secEvent struct {
	IsLock bool
	At     time.Time
}

func drainSecRing(lastPos *int64) []secEvent {
	writePos := int64(C.readSecRingPos())
	if writePos <= *lastPos {
		return nil
	}
	count := writePos - *lastPos
	if count > C.SEC_RING_SIZE {
		count = C.SEC_RING_SIZE
	}
	out := make([]secEvent, 0, count)
	for i := *lastPos; i < writePos; i++ {
		idx := i % C.SEC_RING_SIZE
		cev := (*C.sec_event_t)(unsafe.Pointer(&C.sec_ring[idx]))
		if cev.valid != 0 {
			out = append(out, secEvent{IsLock: cev.is_lock == 1, At: time.Now()})
		}
	}
	*lastPos = writePos
	return out
}

func startObserver() error {
	ret := C.startSecObserver()
	if ret != 0 {
		return fmt.Errorf("startSecObserver failed")
	}
	return nil
}

func listenSecurity(ctx context.Context, events chan<- secEvent) error {
	if err := startObserver(); err != nil {
		return err
	}

	var lastRingPos int64
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for _, ev := range drainSecRing(&lastRingPos) {
				select {
				case events <- ev:
				default:
				}
			}
		}
	}
}

//go:build darwin

package window

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreGraphics -framework AppKit
#include <CoreGraphics/CoreGraphics.h>
#include <AppKit/AppKit.h>
#include <stdlib.h>

typedef struct {
	char *app;
	char *title;
	int pid;
} ActiveWindowResult;

static ActiveWindowResult getActiveWindow() {
	ActiveWindowResult result = {NULL, NULL, 0};
	@autoreleasepool {
		NSRunningApplication *frontApp = [[NSWorkspace sharedWorkspace] frontmostApplication];
		if (frontApp) {
			result.pid = (int)frontApp.processIdentifier;
			NSString *name = frontApp.localizedName;
			if (name) {
				result.app = strdup([name UTF8String]);
			}
		}
		CFArrayRef windowList = CGWindowListCopyWindowInfo(
			kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements,
			kCGNullWindowID
		);
		if (windowList) {
			CFIndex count = CFArrayGetCount(windowList);
			for (CFIndex i = 0; i < count; i++) {
				CFDictionaryRef info = CFArrayGetValueAtIndex(windowList, i);
				CFNumberRef pidRef;
				int wPid = 0;
				if (CFDictionaryGetValueIfPresent(info, kCGWindowOwnerPID, (const void **)&pidRef)) {
					CFNumberGetValue(pidRef, kCFNumberIntType, &wPid);
				}
				if (wPid == result.pid) {
					CFStringRef titleRef;
					if (CFDictionaryGetValueIfPresent(info, kCGWindowName, (const void **)&titleRef) && titleRef) {
						CFIndex len = CFStringGetLength(titleRef);
						CFIndex maxSize = CFStringGetMaximumSizeForEncoding(len, kCFStringEncodingUTF8) + 1;
						char *buf = malloc(maxSize);
						if (CFStringGetCString(titleRef, buf, maxSize, kCFStringEncodingUTF8)) {
							if (strlen(buf) > 0) {
								result.title = buf;
								break;
							}
						}
						free(buf);
					}
				}
			}
			CFRelease(windowList);
		}
	}
	return result;
}

static double getIdleSeconds() {
	return CGEventSourceSecondsSinceLastEventType(
		kCGEventSourceStateCombinedSessionState,
		kCGAnyInputEventType
	);
}
*/
import "C"
import "unsafe"

func ActiveWindow() (WindowInfo, error) {
	result := C.getActiveWindow()
	var info WindowInfo
	if result.app != nil {
		info.App = C.GoString(result.app)
		C.free(unsafe.Pointer(result.app))
	}
	if result.title != nil {
		info.WindowTitle = C.GoString(result.title)
		C.free(unsafe.Pointer(result.title))
	}
	info.PID = int(result.pid)
	return info, nil
}

func idleSeconds() (float64, error) {
	return float64(C.getIdleSeconds()), nil
}

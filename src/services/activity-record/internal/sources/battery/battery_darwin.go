//go:build darwin

package battery

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/ps/IOPowerSources.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	int is_charging;
	int pct;
	int is_present;
} batt_t;

static int sample_batt(batt_t *out) {
	memset(out, 0, sizeof(*out));
	CFTypeRef info = IOPSCopyPowerSourcesInfo();
	if (!info) return -1;
	CFArrayRef list = IOPSCopyPowerSourcesList(info);
	if (!list) { CFRelease(info); return -1; }

	CFIndex count = CFArrayGetCount(list);
	if (count == 0) { CFRelease(list); CFRelease(info); return 0; }

	CFStringRef internalType = CFSTR("InternalBattery");
	for (CFIndex i = 0; i < count; i++) {
		CFDictionaryRef dict = IOPSGetPowerSourceDescription(info,
			(CFTypeRef)CFArrayGetValueAtIndex(list, i));
		if (!dict) continue;

		CFStringRef type = (CFStringRef)CFDictionaryGetValue(dict, CFSTR("Type"));
		if (!type) {
			continue;
		}

		// Check if internal battery.
		CFIndex internalLen = CFStringGetLength(internalType);
		CFIndex typeLen = CFStringGetLength(type);
		if (typeLen != internalLen ||
		    CFStringCompare(type, internalType, 0) != kCFCompareEqualTo) {
			continue;
		}

		out->is_present = 1;

		CFBooleanRef charging = (CFBooleanRef)CFDictionaryGetValue(dict, CFSTR("Is Charging"));
		if (charging && CFBooleanGetValue(charging)) {
			out->is_charging = 1;
		}

		CFNumberRef pctNum = (CFNumberRef)CFDictionaryGetValue(dict, CFSTR("Current Capacity"));
		if (pctNum) {
			int val = 0;
			CFNumberGetValue(pctNum, kCFNumberIntType, &val);
			out->pct = val;
		}
		break;
	}

	CFRelease(list);
	CFRelease(info);
	return 0;
}
*/
import "C"
import "fmt"

type battSample struct {
	IsCharging bool
	Pct        int
	Present    bool
}

func sampleBattery() (battSample, error) {
	var cs C.batt_t
	if C.sample_batt(&cs) != 0 {
		return battSample{}, fmt.Errorf("sample_batt failed")
	}
	return battSample{
		IsCharging: cs.is_charging == 1,
		Pct:        int(cs.pct),
		Present:    cs.is_present == 1,
	}, nil
}

//go:build darwin

package ax

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreGraphics -framework AppKit -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>
#include <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>
#include <sys/time.h>

// ---- constants ----

#define AX_MAX_TEXT_BUF (100 * 1024)
#define AX_YIELD_EVERY 100

// ---- result struct ----

typedef struct {
	char   *app_name;
	char   *window_title;
	int     pid;
	char   *text_content;
	int     element_count;
	int     truncated;
	char   *truncation_reason;
	double  walk_duration_ms;
	char   *browser_url;
	char   *error_msg;
} AXWalkResult;

// ---- walk state ----

typedef struct {
	char   *buf;
	int     buf_len;
	int     buf_cap;
	int     element_count;
	int     max_nodes;
	int     max_depth;
	double  deadline;   // CFAbsoluteTime
	int     truncated;
	char    truncation_reason[16];
} WalkState;

// ---- text buffer helpers ----

static void buf_append(WalkState *s, const char *text) {
	if (!text) return;
	int tlen = (int)strlen(text);
	if (tlen == 0) return;
	// trim leading/trailing whitespace
	while (tlen > 0 && (text[0] == ' ' || text[0] == '\t' || text[0] == '\n' || text[0] == '\r')) { text++; tlen--; }
	while (tlen > 0 && (text[tlen-1] == ' ' || text[tlen-1] == '\t' || text[tlen-1] == '\n' || text[tlen-1] == '\r')) { tlen--; }
	if (tlen == 0) return;
	int need = s->buf_len + tlen + 1; // +1 for newline separator
	if (need >= s->buf_cap) return; // hard cap
	if (s->buf_len > 0) {
		s->buf[s->buf_len] = '\n';
		s->buf_len++;
	}
	memcpy(s->buf + s->buf_len, text, tlen);
	s->buf_len += tlen;
	s->buf[s->buf_len] = '\0';
}

static void buf_append_cfstr(WalkState *s, CFStringRef str) {
	if (!str) return;
	CFIndex len = CFStringGetLength(str);
	if (len == 0) return;
	CFIndex maxSize = CFStringGetMaximumSizeForEncoding(len, kCFStringEncodingUTF8) + 1;
	if (maxSize > 8192) maxSize = 8192;
	char tmp[8192];
	char *buf = (maxSize <= (CFIndex)sizeof(tmp)) ? tmp : (char *)malloc(maxSize);
	if (CFStringGetCString(str, buf, maxSize, kCFStringEncodingUTF8)) {
		buf_append(s, buf);
	}
	if (buf != tmp) free(buf);
}

// ---- role classification ----

static int should_skip_role(CFStringRef role) {
	if (!role) return 1;
	static CFStringRef skipped[12];
	static int inited = 0;
	if (!inited) {
		skipped[0]  = CFSTR("AXScrollBar");
		skipped[1]  = CFSTR("AXImage");
		skipped[2]  = CFSTR("AXSplitter");
		skipped[3]  = CFSTR("AXGrowArea");
		skipped[4]  = CFSTR("AXMenuBar");
		skipped[5]  = CFSTR("AXMenu");
		skipped[6]  = CFSTR("AXToolbar");
		skipped[7]  = CFSTR("AXSecureTextField");
		skipped[8]  = CFSTR("AXMenuBarItem");
		skipped[9]  = CFSTR("AXRuler");
		skipped[10] = CFSTR("AXBusyIndicator");
		skipped[11] = CFSTR("AXProgressIndicator");
		inited = 1;
	}
	for (int i = 0; i < 12; i++) {
		if (CFStringCompare(role, skipped[i], 0) == kCFCompareEqualTo) return 1;
	}
	return 0;
}

static int should_extract_text(CFStringRef role) {
	if (!role) return 0;
	static CFStringRef roles[17];
	static int inited = 0;
	if (!inited) {
		roles[0]  = CFSTR("AXStaticText");
		roles[1]  = CFSTR("AXTextField");
		roles[2]  = CFSTR("AXTextArea");
		roles[3]  = CFSTR("AXButton");
		roles[4]  = CFSTR("AXMenuItem");
		roles[5]  = CFSTR("AXCell");
		roles[6]  = CFSTR("AXHeading");
		roles[7]  = CFSTR("AXLink");
		roles[8]  = CFSTR("AXMenuButton");
		roles[9]  = CFSTR("AXPopUpButton");
		roles[10] = CFSTR("AXComboBox");
		roles[11] = CFSTR("AXCheckBox");
		roles[12] = CFSTR("AXRadioButton");
		roles[13] = CFSTR("AXDisclosureTriangle");
		roles[14] = CFSTR("AXTab");
		roles[15] = CFSTR("AXWebArea");
		roles[16] = CFSTR("AXGroup");
		inited = 1;
	}
	for (int i = 0; i < 17; i++) {
		if (CFStringCompare(role, roles[i], 0) == kCFCompareEqualTo) return 1;
	}
	return 0;
}

// ---- text extraction ----

static void extract_text(AXUIElementRef elem, CFStringRef role, WalkState *s) {
	CFTypeRef val = NULL;

	// text fields: prefer AXValue (the actual content)
	if (CFStringCompare(role, CFSTR("AXStaticText"), 0) == kCFCompareEqualTo ||
		CFStringCompare(role, CFSTR("AXTextField"), 0) == kCFCompareEqualTo ||
		CFStringCompare(role, CFSTR("AXTextArea"), 0) == kCFCompareEqualTo ||
		CFStringCompare(role, CFSTR("AXComboBox"), 0) == kCFCompareEqualTo) {
		AXUIElementCopyAttributeValue(elem, kAXValueAttribute, &val);
		if (val && CFGetTypeID(val) == CFStringGetTypeID() && CFStringGetLength((CFStringRef)val) > 0) {
			buf_append_cfstr(s, (CFStringRef)val);
			CFRelease(val);
			return;
		}
		if (val) { CFRelease(val); val = NULL; }
	}

	// AXWebArea / AXGroup: try AXValue
	if (CFStringCompare(role, CFSTR("AXWebArea"), 0) == kCFCompareEqualTo ||
		CFStringCompare(role, CFSTR("AXGroup"), 0) == kCFCompareEqualTo) {
		AXUIElementCopyAttributeValue(elem, kAXValueAttribute, &val);
		if (val && CFGetTypeID(val) == CFStringGetTypeID() && CFStringGetLength((CFStringRef)val) > 0) {
			buf_append_cfstr(s, (CFStringRef)val);
			CFRelease(val);
			return;
		}
		if (val) { CFRelease(val); val = NULL; }
	}

	// fallback: AXTitle
	AXUIElementCopyAttributeValue(elem, kAXTitleAttribute, &val);
	if (val && CFGetTypeID(val) == CFStringGetTypeID() && CFStringGetLength((CFStringRef)val) > 0) {
		buf_append_cfstr(s, (CFStringRef)val);
		CFRelease(val);
		return;
	}
	if (val) { CFRelease(val); val = NULL; }

	// fallback: AXDescription
	AXUIElementCopyAttributeValue(elem, kAXDescriptionAttribute, &val);
	if (val && CFGetTypeID(val) == CFStringGetTypeID() && CFStringGetLength((CFStringRef)val) > 0) {
		buf_append_cfstr(s, (CFStringRef)val);
		CFRelease(val);
		return;
	}
	if (val) CFRelease(val);
}

// ---- recursive walker ----

static void walk_recursive(AXUIElementRef elem, int depth, WalkState *s) {
	if (s->truncated) return;
	if (s->element_count >= s->max_nodes) {
		s->truncated = 1;
		strcpy(s->truncation_reason, "max_nodes");
		return;
	}
	if (depth >= s->max_depth) {
		s->truncated = 1;
		strcpy(s->truncation_reason, "max_depth");
		return;
	}
	if (CFAbsoluteTimeGetCurrent() >= s->deadline) {
		s->truncated = 1;
		strcpy(s->truncation_reason, "timeout");
		return;
	}

	s->element_count++;

	if (s->element_count % AX_YIELD_EVERY == 0) {
		usleep(100);
	}

	AXUIElementSetMessagingTimeout(elem, 0.2);

	CFTypeRef roleRef = NULL;
	AXError err = AXUIElementCopyAttributeValue(elem, kAXRoleAttribute, &roleRef);
	if (err != kAXErrorSuccess || !roleRef) {
		if (roleRef) CFRelease(roleRef);
		return;
	}
	CFStringRef role = (CFStringRef)roleRef;

	if (should_skip_role(role)) {
		CFRelease(role);
		return;
	}

	if (should_extract_text(role)) {
		extract_text(elem, role, s);
	}

	CFRelease(role);

	// recurse into children
	CFTypeRef childrenRef = NULL;
	err = AXUIElementCopyAttributeValue(elem, kAXChildrenAttribute, &childrenRef);
	if (err != kAXErrorSuccess || !childrenRef) {
		if (childrenRef) CFRelease(childrenRef);
		return;
	}
	if (CFGetTypeID(childrenRef) != CFArrayGetTypeID()) {
		CFRelease(childrenRef);
		return;
	}
	CFArrayRef children = (CFArrayRef)childrenRef;
	CFIndex count = CFArrayGetCount(children);
	for (CFIndex i = 0; i < count; i++) {
		if (s->truncated) break;
		AXUIElementRef child = (AXUIElementRef)CFArrayGetValueAtIndex(children, i);
		walk_recursive(child, depth + 1, s);
	}
	CFRelease(children);
}

// ---- enhanced mode for Chromium/Electron ----

static pid_t enhanced_last_pid = 0;
static double enhanced_last_time = 0;

static int is_chromium_app(const char *name) {
	static const char *apps[] = {
		"Google Chrome", "Google Chrome Canary", "Chromium",
		"Microsoft Edge", "Brave Browser", "Arc", "Vivaldi", "Opera",
		"Code", "Cursor", "Windsurf", "VSCodium", "Trae",
		"Electron", "Slack", "Discord", "Obsidian", "Notion",
		"Spotify", "Figma", "Linear", "Postman",
		NULL
	};
	for (int i = 0; apps[i]; i++) {
		if (strcmp(name, apps[i]) == 0) return 1;
	}
	return 0;
}

static void try_set_enhanced_mode(AXUIElementRef app, pid_t pid) {
	double now = CFAbsoluteTimeGetCurrent();
	if (pid == enhanced_last_pid && (now - enhanced_last_time) < 60.0) return;
	AXUIElementSetAttributeValue(app, CFSTR("AXEnhancedUserInterface"), kCFBooleanTrue);
	AXUIElementSetAttributeValue(app, CFSTR("AXManualAccessibility"), kCFBooleanTrue);
	enhanced_last_pid = pid;
	enhanced_last_time = now;
}

// ---- excluded apps ----

static int is_excluded_app(const char *name) {
	static const char *excluded[] = {
		"1Password", "Bitwarden", "KeePassXC", "LastPass", "Dashlane",
		"Keychain Access", "loginwindow", "SecurityAgent",
		NULL
	};
	for (int i = 0; excluded[i]; i++) {
		if (strcmp(name, excluded[i]) == 0) return 1;
	}
	return 0;
}

// ---- incognito detection ----

static int is_incognito_title(const char *title) {
	if (!title) return 0;
	if (strstr(title, "Incognito") || strstr(title, "InPrivate") ||
		strstr(title, "Private Browsing") || strstr(title, "Private browsing")) {
		return 1;
	}
	return 0;
}

// ---- browser URL extraction ----

static char* extract_browser_url(AXUIElementRef window) {
	CFTypeRef docRef = NULL;
	AXError err = AXUIElementCopyAttributeValue(window, CFSTR("AXDocument"), &docRef);
	if (err != kAXErrorSuccess || !docRef) {
		if (docRef) CFRelease(docRef);
		return NULL;
	}
	if (CFGetTypeID(docRef) != CFStringGetTypeID()) {
		CFRelease(docRef);
		return NULL;
	}
	CFStringRef doc = (CFStringRef)docRef;
	CFIndex len = CFStringGetLength(doc);
	if (len == 0) {
		CFRelease(doc);
		return NULL;
	}
	CFIndex maxSize = CFStringGetMaximumSizeForEncoding(len, kCFStringEncodingUTF8) + 1;
	char *buf = (char *)malloc(maxSize);
	if (!CFStringGetCString(doc, buf, maxSize, kCFStringEncodingUTF8)) {
		free(buf);
		CFRelease(doc);
		return NULL;
	}
	CFRelease(doc);
	// skip file:// URLs (local documents, not browser)
	if (strncmp(buf, "file://", 7) == 0) {
		free(buf);
		return NULL;
	}
	return buf;
}

// ---- cfstring to strdup'd char* ----

static char* cfstr_to_cstr(CFStringRef str) {
	if (!str) return NULL;
	CFIndex len = CFStringGetLength(str);
	if (len == 0) return NULL;
	CFIndex maxSize = CFStringGetMaximumSizeForEncoding(len, kCFStringEncodingUTF8) + 1;
	char *buf = (char *)malloc(maxSize);
	if (!CFStringGetCString(str, buf, maxSize, kCFStringEncodingUTF8)) {
		free(buf);
		return NULL;
	}
	return buf;
}

// ---- main entry point ----

static AXWalkResult doWalkFocusedWindow(int max_depth, int max_nodes, double timeout_ms) {
	AXWalkResult result = {0};
	@autoreleasepool {
		double start = CFAbsoluteTimeGetCurrent();

		// 1. check accessibility trust
		if (!AXIsProcessTrusted()) {
			result.error_msg = strdup("accessibility permission required");
			return result;
		}

		// 2. get frontmost app via NSWorkspace
		NSRunningApplication *frontApp = [[NSWorkspace sharedWorkspace] frontmostApplication];
		if (!frontApp) {
			return result;
		}
		result.pid = (int)frontApp.processIdentifier;
		NSString *name = frontApp.localizedName;
		if (name) {
			result.app_name = strdup([name UTF8String]);
		}

		// 3. check exclusions
		if (result.app_name && is_excluded_app(result.app_name)) {
			return result;
		}

		// 4. create AX element from PID
		AXUIElementRef app = AXUIElementCreateApplication((pid_t)result.pid);

		// 5. enhanced mode for Chromium/Electron
		if (result.app_name && is_chromium_app(result.app_name)) {
			try_set_enhanced_mode(app, (pid_t)result.pid);
		}

		// 6. get focused window
		CFTypeRef winRef = NULL;
		AXError err = AXUIElementCopyAttributeValue(app, kAXFocusedWindowAttribute, &winRef);
		CFRelease(app);
		if (err != kAXErrorSuccess || !winRef) {
			if (winRef) CFRelease(winRef);
			return result;
		}
		AXUIElementRef window = (AXUIElementRef)winRef;

		// 7. get window title
		CFTypeRef titleRef = NULL;
		AXUIElementCopyAttributeValue(window, kAXTitleAttribute, &titleRef);
		if (titleRef && CFGetTypeID(titleRef) == CFStringGetTypeID()) {
			result.window_title = cfstr_to_cstr((CFStringRef)titleRef);
		}
		if (titleRef) CFRelease(titleRef);

		// 8. incognito check
		if (result.window_title && is_incognito_title(result.window_title)) {
			CFRelease(window);
			return result;
		}

		// 9. browser URL
		result.browser_url = extract_browser_url(window);

		// 10. walk the tree
		WalkState state = {0};
		state.buf = (char *)calloc(AX_MAX_TEXT_BUF, 1);
		state.buf_cap = AX_MAX_TEXT_BUF;
		state.max_nodes = max_nodes;
		state.max_depth = max_depth;
		state.deadline = start + (timeout_ms / 1000.0);

		walk_recursive(window, 0, &state);

		CFRelease(window);

		double end = CFAbsoluteTimeGetCurrent();
		result.walk_duration_ms = (end - start) * 1000.0;
		result.element_count = state.element_count;

		if (state.buf_len > 0) {
			result.text_content = state.buf;
		} else {
			free(state.buf);
		}

		if (state.truncated) {
			result.truncated = 1;
			result.truncation_reason = strdup(state.truncation_reason);
		}
	}
	return result;
}

static void freeAXWalkResult(AXWalkResult *r) {
	if (r->app_name)          free(r->app_name);
	if (r->window_title)      free(r->window_title);
	if (r->text_content)      free(r->text_content);
	if (r->truncation_reason) free(r->truncation_reason);
	if (r->browser_url)       free(r->browser_url);
	if (r->error_msg)         free(r->error_msg);
}

static double getIdleSeconds() {
	return CGEventSourceSecondsSinceLastEventType(
		kCGEventSourceStateCombinedSessionState,
		kCGAnyInputEventType
	);
}
*/
import "C"
import (
	"fmt"
	"runtime"
	"unsafe"
)

func walkOnThread(maxDepth, maxNodes int, timeoutMs float64) WalkResult {
	var result WalkResult
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		result = doWalk(maxDepth, maxNodes, timeoutMs)
		close(done)
	}()
	<-done
	return result
}

func doWalk(maxDepth, maxNodes int, timeoutMs float64) WalkResult {
	cr := C.doWalkFocusedWindow(C.int(maxDepth), C.int(maxNodes), C.double(timeoutMs))
	defer C.freeAXWalkResult(&cr)

	var r WalkResult
	if cr.error_msg != nil {
		r.Error = fmt.Errorf("%s", C.GoString(cr.error_msg))
		return r
	}
	if cr.app_name != nil {
		r.AppName = C.GoString(cr.app_name)
	}
	if cr.window_title != nil {
		r.WindowTitle = C.GoString(cr.window_title)
	}
	r.PID = int(cr.pid)
	if cr.text_content != nil {
		r.TextContent = C.GoString(cr.text_content)
	}
	r.ElementCount = int(cr.element_count)
	r.WalkDurationMs = float64(cr.walk_duration_ms)
	if cr.browser_url != nil {
		r.BrowserURL = C.GoString(cr.browser_url)
	}
	if cr.truncated != 0 {
		r.Truncated = true
		if cr.truncation_reason != nil {
			r.TruncationReason = C.GoString(cr.truncation_reason)
		}
	}
	return r
}

func idleSeconds() (float64, error) {
	return float64(C.getIdleSeconds()), nil
}

// ensure unsafe import is used
var _ = unsafe.Pointer(nil)

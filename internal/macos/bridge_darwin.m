// AppKit/Accessibility side of the macOS backend. Everything here
// runs on the main thread: AX observer sources and the key event tap
// are added to the main run loop; workspace notifications and Carbon
// hotkey events are delivered there too.
#import <AppKit/AppKit.h>
#import <QuartzCore/QuartzCore.h>
#include <ApplicationServices/ApplicationServices.h>
#include <Carbon/Carbon.h>
#include <pthread.h>
#include <stdlib.h>
#include <math.h>
#include <string.h>
#include <unistd.h>

#include "bridge.h"
#include "_cgo_export.h"

// Private but stable for a decade (AeroSpace, yabai and Amethyst rely
// on it): the CGWindowID behind an AX window element.
extern AXError _AXUIElementGetWindow(AXUIElementRef element, CGWindowID *out);

typedef struct {
	uint32_t wid;
	pid_t pid;
	AXUIElementRef el;
	int fullscreen; // in native fullscreen (its own Space): out of the tiling
} tracked_win;

typedef struct {
	pid_t pid;
	AXObserverRef obs;
	AXUIElementRef app;
} tracked_app;

static tracked_win *wins;
static int nwins, capwins;
static tracked_app *apps;
static int napps, capapps;
static CFMachPortRef keytap;

static int find_win(uint32_t wid) {
	for (int i = 0; i < nwins; i++)
		if (wins[i].wid == wid) return i;
	return -1;
}

static int find_el(AXUIElementRef el) {
	for (int i = 0; i < nwins; i++)
		if (CFEqual(wins[i].el, el)) return i;
	return -1;
}

static int find_app(pid_t pid) {
	for (int i = 0; i < napps; i++)
		if (apps[i].pid == pid) return i;
	return -1;
}

// copy_str returns a malloc'd UTF-8 copy of a string attribute ("" if
// missing); the caller frees it.
static char *copy_str(AXUIElementRef el, CFStringRef attr) {
	CFTypeRef v = NULL;
	char *out = NULL;
	if (AXUIElementCopyAttributeValue(el, attr, &v) == kAXErrorSuccess && v && CFGetTypeID(v) == CFStringGetTypeID()) {
		out = strdup([(__bridge NSString *)v UTF8String] ?: "");
	}
	if (v) CFRelease(v);
	return out ? out : strdup("");
}

static int bool_attr(AXUIElementRef el, CFStringRef attr) {
	CFTypeRef v = NULL;
	int out = 0;
	if (AXUIElementCopyAttributeValue(el, attr, &v) == kAXErrorSuccess && v && CFGetTypeID(v) == CFBooleanGetTypeID())
		out = CFBooleanGetValue(v);
	if (v) CFRelease(v);
	return out;
}

// zoom_enabled reports whether the window has an enabled zoom button.
// Fixed-size windows (Calculator, preference panes) have one, but
// disabled.
static int zoom_enabled(AXUIElementRef win) {
	CFTypeRef btn = NULL;
	int ok = 0;
	if (AXUIElementCopyAttributeValue(win, kAXZoomButtonAttribute, &btn) == kAXErrorSuccess && btn)
		ok = bool_attr((AXUIElementRef)btn, kAXEnabledAttribute);
	if (btn) CFRelease(btn);
	return ok;
}

static const char *bundle_of(pid_t pid) {
	NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
	return app.bundleIdentifier.UTF8String ?: "";
}

// report_window tells Go about tracked window i (new, deminiaturized,
// or its app unhidden). minimized overrides the AX attribute when set.
static void report_window(int i, int minimized) {
	AXUIElementRef win = wins[i].el;
	char *title = copy_str(win, kAXTitleAttribute);
	char *subrole = copy_str(win, kAXSubroleAttribute);
	goWindowAdded(wins[i].wid, wins[i].pid, (char *)bundle_of(wins[i].pid), title, subrole, zoom_enabled(win),
	              minimized || bool_attr(win, kAXMinimizedAttribute));
	free(title);
	free(subrole);
}

// window_notes are the per-window AX notifications wimy observes.
static CFStringRef window_notes(int k) {
	switch (k) {
	case 0: return kAXUIElementDestroyedNotification;
	case 1: return kAXTitleChangedNotification;
	case 2: return kAXWindowMiniaturizedNotification;
	case 3: return kAXWindowDeminiaturizedNotification;
	case 4: return kAXMovedNotification;
	default: return kAXResizedNotification;
	}
}

// register_window_notes adds the per-window notifications. A window
// that isn't ready yet refuses them; retry the missing ones once after
// 500ms, else a window whose "destroyed" notification never registered
// would stay in the layout after it closes.
static void register_window_notes(uint32_t wid, int retry) {
	int i = find_win(wid), a = i >= 0 ? find_app(wins[i].pid) : -1;
	if (a < 0) return;
	int failed = 0;
	for (int k = 0; k < 6; k++) {
		AXError err = AXObserverAddNotification(apps[a].obs, wins[i].el, window_notes(k), NULL);
		if (err != kAXErrorSuccess && err != kAXErrorNotificationAlreadyRegistered) failed = 1;
	}
	if (failed && retry) {
		dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 500 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
			register_window_notes(wid, 0);
		});
	} else if (failed) {
		NSLog(@"wimy: window %u refused AX notifications; it may linger after closing", wid);
	}
}

static void track_window(pid_t pid, AXUIElementRef win) {
	CGWindowID wid = 0;
	if (_AXUIElementGetWindow(win, &wid) != kAXErrorSuccess || wid == 0) return;
	if (find_win(wid) >= 0) return;
	char *role = copy_str(win, kAXRoleAttribute);
	int is_window = strcmp(role, "AXWindow") == 0;
	free(role);
	if (!is_window) return;

	if (nwins == capwins) {
		capwins = capwins ? capwins * 2 : 32;
		wins = realloc(wins, capwins * sizeof *wins);
	}
	wins[nwins++] = (tracked_win){wid, pid, (AXUIElementRef)CFRetain(win), 0};
	register_window_notes(wid, 1);
	report_window(find_win(wid), 0);
}

static void untrack_at(int i) {
	uint32_t wid = wins[i].wid;
	CFRelease(wins[i].el);
	wins[i] = wins[--nwins];
	goWindowRemoved(wid);
}

static void observer_cb(AXObserverRef obs, AXUIElementRef el, CFStringRef note, void *ctx) {
	if (CFEqual(note, kAXWindowCreatedNotification)) {
		pid_t pid = 0;
		AXUIElementGetPid(el, &pid);
		track_window(pid, el);
	} else if (CFEqual(note, kAXFocusedWindowChangedNotification)) {
		int i = find_el(el);
		if (i >= 0) goFocusChanged(wins[i].wid, wins[i].pid);
	} else if (CFEqual(note, kAXWindowMiniaturizedNotification)) {
		int i = find_el(el);
		if (i >= 0) goWindowGone(wins[i].wid);
	} else if (CFEqual(note, kAXWindowDeminiaturizedNotification)) {
		int i = find_el(el);
		if (i >= 0) report_window(i, 0);
	} else if (CFEqual(note, kAXMovedNotification) || CFEqual(note, kAXResizedNotification)) {
		int i = find_el(el);
		if (i < 0) return;
		// native fullscreen moves the window to its own Space: leave
		// the tiling (like minimize) until it comes back
		int fs = bool_attr(el, CFSTR("AXFullScreen"));
		if (fs && !wins[i].fullscreen) {
			wins[i].fullscreen = 1;
			goWindowGone(wins[i].wid);
		} else if (!fs && wins[i].fullscreen) {
			wins[i].fullscreen = 0;
			report_window(i, 0);
		} else if (!fs) {
			goWindowMoved(wins[i].wid);
		}
	} else if (CFEqual(note, kAXUIElementDestroyedNotification)) {
		int i = find_el(el);
		if (i >= 0) untrack_at(i);
	} else if (CFEqual(note, kAXTitleChangedNotification)) {
		int i = find_el(el);
		if (i >= 0) {
			char *title = copy_str(el, kAXTitleAttribute);
			goTitleChanged(wins[i].wid, title);
			free(title);
		}
	}
}

// watch_pid observes a regular app's windows. A freshly launched app
// often isn't ready for AX yet; retry with a doubling delay (250ms up
// to 2s, ~20s in total). App activation re-watches too.
static void watch_pid(pid_t pid, int attempts, int64_t delay_ms) {
	if (pid == getpid() || find_app(pid) >= 0) return;
	AXUIElementRef app = AXUIElementCreateApplication(pid);
	AXUIElementSetMessagingTimeout(app, 1.0);
	AXObserverRef obs = NULL;
	if (AXObserverCreate(pid, observer_cb, &obs) != kAXErrorSuccess) {
		CFRelease(app);
		return;
	}
	if (AXObserverAddNotification(obs, app, kAXWindowCreatedNotification, NULL) != kAXErrorSuccess) {
		CFRelease(obs);
		CFRelease(app);
		if (attempts > 0) {
			dispatch_after(dispatch_time(DISPATCH_TIME_NOW, delay_ms * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
				watch_pid(pid, attempts - 1, delay_ms * 2 > 2000 ? 2000 : delay_ms * 2);
			});
		}
		return;
	}
	AXObserverAddNotification(obs, app, kAXFocusedWindowChangedNotification, NULL);
	CFRunLoopAddSource(CFRunLoopGetMain(), AXObserverGetRunLoopSource(obs), kCFRunLoopDefaultMode);
	if (napps == capapps) {
		capapps = capapps ? capapps * 2 : 32;
		apps = realloc(apps, capapps * sizeof *apps);
	}
	apps[napps++] = (tracked_app){pid, obs, app};

	CFArrayRef list = NULL;
	if (AXUIElementCopyAttributeValue(app, kAXWindowsAttribute, (CFTypeRef *)&list) == kAXErrorSuccess && list) {
		for (CFIndex i = 0; i < CFArrayGetCount(list); i++)
			track_window(pid, (AXUIElementRef)CFArrayGetValueAtIndex(list, i));
		CFRelease(list);
	}
}

// App hiding (Cmd-H, Hide Others) is undone at once: a hidden app's
// windows would leave holes in the tiling, or come back in the wrong
// place. Tiling users hardly ever hide apps on purpose.
static void unhide_app(NSRunningApplication *app) {
	if (app.hidden && app.activationPolicy == NSApplicationActivationPolicyRegular) [app unhide];
}

static void watch_app(NSRunningApplication *app) {
	if (app.activationPolicy != NSApplicationActivationPolicyRegular) return;
	unhide_app(app);
	watch_pid(app.processIdentifier, 12, 250);
}

static void unwatch_pid(pid_t pid) {
	for (int i = nwins - 1; i >= 0; i--)
		if (wins[i].pid == pid) untrack_at(i);
	int a = find_app(pid);
	if (a < 0) return;
	CFRunLoopRemoveSource(CFRunLoopGetMain(), AXObserverGetRunLoopSource(apps[a].obs), kCFRunLoopDefaultMode);
	CFRelease(apps[a].obs);
	CFRelease(apps[a].app);
	apps[a] = apps[--napps];
}

int wimy_ax_trusted(int prompt) {
	NSDictionary *opts = @{(__bridge NSString *)kAXTrustedCheckOptionPrompt : @(prompt != 0)};
	return AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)opts);
}

// Entry points called from Go before [NSApp run] wrap their bodies in
// @autoreleasepool: there is no run loop pool yet.

void wimy_app_init(void) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
		AXUIElementRef sys = AXUIElementCreateSystemWide();
		AXUIElementSetMessagingTimeout(sys, 1.0);
		CFRelease(sys);
	}
}

void wimy_app_run(void) { [NSApp run]; }

void wimy_app_stop(void) {
	[NSApp stop:nil];
	// stop only takes effect after the next event
	NSEvent *e = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
	                                location:NSZeroPoint
	                           modifierFlags:0
	                               timestamp:0
	                            windowNumber:0
	                                 context:nil
	                                 subtype:0
	                                   data1:0
	                                   data2:0];
	[NSApp postEvent:e atStart:YES];
}

void wimy_dispatch(uintptr_t handle) {
	dispatch_async(dispatch_get_main_queue(), ^{
		goRunDispatched(handle);
	});
}

void wimy_schedule_apply(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		goApply();
	});
}

void wimy_schedule_apply_after(int ms) {
	dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)ms * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
		goApply();
	});
}

// focused_wid returns the tracked window an app has focused, or 0.
static uint32_t focused_wid(pid_t pid) {
	AXUIElementRef app = AXUIElementCreateApplication(pid);
	AXUIElementSetMessagingTimeout(app, 1.0);
	CFTypeRef win = NULL;
	CGWindowID wid = 0;
	if (AXUIElementCopyAttributeValue(app, kAXFocusedWindowAttribute, &win) == kAXErrorSuccess && win)
		_AXUIElementGetWindow((AXUIElementRef)win, &wid);
	if (win) CFRelease(win);
	CFRelease(app);
	return (wid && find_win(wid) >= 0) ? wid : 0;
}

uint32_t wimy_focused_window(void) {
	@autoreleasepool {
		NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
		return app ? focused_wid(app.processIdentifier) : 0;
	}
}

void wimy_start_tracking(void) {
	@autoreleasepool {
	NSNotificationCenter *wc = [[NSWorkspace sharedWorkspace] notificationCenter];
	[wc addObserverForName:NSWorkspaceDidActivateApplicationNotification object:nil queue:[NSOperationQueue mainQueue]
	            usingBlock:^(NSNotification *n) {
		            NSRunningApplication *app = n.userInfo[NSWorkspaceApplicationKey];
		            watch_app(app);
		            uint32_t wid = focused_wid(app.processIdentifier);
		            if (wid) goFocusChanged(wid, app.processIdentifier);
	            }];
	[wc addObserverForName:NSWorkspaceDidHideApplicationNotification object:nil queue:[NSOperationQueue mainQueue]
	            usingBlock:^(NSNotification *n) {
		            unhide_app(n.userInfo[NSWorkspaceApplicationKey]);
	            }];
	[wc addObserverForName:NSWorkspaceDidLaunchApplicationNotification object:nil queue:[NSOperationQueue mainQueue]
	            usingBlock:^(NSNotification *n) {
		            watch_app(n.userInfo[NSWorkspaceApplicationKey]);
	            }];
	[wc addObserverForName:NSWorkspaceDidTerminateApplicationNotification object:nil queue:[NSOperationQueue mainQueue]
	            usingBlock:^(NSNotification *n) {
		            NSRunningApplication *app = n.userInfo[NSWorkspaceApplicationKey];
		            unwatch_pid(app.processIdentifier);
	            }];
	[[NSNotificationCenter defaultCenter] addObserverForName:NSApplicationDidChangeScreenParametersNotification
	                                                  object:nil
	                                                   queue:[NSOperationQueue mainQueue]
	                                              usingBlock:^(NSNotification *n) {
		                                              goScreensChanged();
	                                              }];
	for (NSRunningApplication *app in [[NSWorkspace sharedWorkspace] runningApplications])
		watch_app(app);
	}
}

void wimy_start_secure_input_poll(void) {
	dispatch_source_t t = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, dispatch_get_main_queue());
	dispatch_source_set_timer(t, dispatch_time(DISPATCH_TIME_NOW, 2 * NSEC_PER_SEC), 2 * NSEC_PER_SEC, NSEC_PER_SEC / 4);
	dispatch_source_set_event_handler(t, ^{
		goSecureInputTick();
	});
	dispatch_resume(t);
	static dispatch_source_t keep; // the timer lives as long as wimy
	keep = t;
}

static CGEventRef keytap_cb(CGEventTapProxy proxy, CGEventType type, CGEventRef ev, void *ctx) {
	if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
		CGEventTapEnable(keytap, true);
		return ev;
	}
	if (type != kCGEventKeyDown) return ev;
	uint16_t code = (uint16_t)CGEventGetIntegerValueField(ev, kCGKeyboardEventKeycode);
	int repeat = CGEventGetIntegerValueField(ev, kCGKeyboardEventAutorepeat) != 0;
	return goKeyDown(code, CGEventGetFlags(ev), repeat) ? NULL : ev;
}

// keytap_thread runs the tap's run loop. An active tap holds every
// key-down system-wide until its callback returns, so it must not share
// the main thread with AX calls that can block for a second each on an
// unresponsive app.
static void *keytap_thread(void *arg) {
	CFRunLoopSourceRef src = arg;
	CFRunLoopAddSource(CFRunLoopGetCurrent(), src, kCFRunLoopCommonModes);
	CFRelease(src);
	CFRunLoopRun();
	return NULL;
}

int wimy_start_keytap(void) {
	keytap = CGEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap, kCGEventTapOptionDefault,
	                          CGEventMaskBit(kCGEventKeyDown), keytap_cb, NULL);
	if (!keytap) return -1;
	CFRunLoopSourceRef src = CFMachPortCreateRunLoopSource(NULL, keytap, 0);
	CGEventTapEnable(keytap, true);
	pthread_t th;
	if (pthread_create(&th, NULL, keytap_thread, (void *)src) != 0) {
		CFRelease(src);
		return -1;
	}
	pthread_detach(th);
	return 0;
}

// wimy_secure_input_pid returns the pid of the process holding secure
// event input (which blinds the event tap), or 0.
int wimy_secure_input_pid(void) {
	CFDictionaryRef d = CGSessionCopyCurrentDictionary();
	if (!d) return 0;
	int pid = 0;
	CFNumberRef n = CFDictionaryGetValue(d, CFSTR("kCGSSessionSecureInputPID"));
	if (n) CFNumberGetValue(n, kCFNumberIntType, &pid);
	CFRelease(d);
	return pid;
}

static EventHotKeyRef *hotkeys;
static int nhotkeys, caphotkeys;
static int hotkey_handler_installed;

static OSStatus hotkey_handler(EventHandlerCallRef next, EventRef ev, void *ctx) {
	EventHotKeyID hk;
	if (GetEventParameter(ev, kEventParamDirectObject, typeEventHotKeyID, NULL, sizeof hk, NULL, &hk) == noErr &&
	    hk.signature == 'wimy')
		goHotKey(hk.id);
	return noErr;
}

int wimy_hotkey_register(uint32_t id, uint16_t code, uint32_t mods) {
	if (!hotkey_handler_installed) {
		EventTypeSpec spec = {kEventClassKeyboard, kEventHotKeyPressed};
		InstallApplicationEventHandler(NewEventHandlerUPP(hotkey_handler), 1, &spec, NULL, NULL);
		hotkey_handler_installed = 1;
	}
	EventHotKeyRef ref = NULL;
	EventHotKeyID hk = {'wimy', id};
	OSStatus st = RegisterEventHotKey(code, mods, hk, GetApplicationEventTarget(), 0, &ref);
	if (st != noErr) return (int)st;
	if (nhotkeys == caphotkeys) {
		caphotkeys = caphotkeys ? caphotkeys * 2 : 64;
		hotkeys = realloc(hotkeys, caphotkeys * sizeof *hotkeys);
	}
	hotkeys[nhotkeys++] = ref;
	return 0;
}

void wimy_hotkeys_clear(void) {
	for (int i = 0; i < nhotkeys; i++)
		UnregisterEventHotKey(hotkeys[i]);
	nhotkeys = 0;
}

int wimy_screens(wimy_screen *out, int max) {
	@autoreleasepool {
		NSArray<NSScreen *> *screens = [NSScreen screens];
		int n = 0;
		for (NSScreen *s in screens) {
			if (n == max) break;
			NSRect f = s.frame, v = s.visibleFrame;
			out[n].frame = (wimy_rect){f.origin.x, f.origin.y, f.size.width, f.size.height};
			out[n].visible = (wimy_rect){v.origin.x, v.origin.y, v.size.width, v.size.height};
			out[n].display = [s.deviceDescription[@"NSScreenNumber"] unsignedIntValue];
			strlcpy(out[n].name, s.localizedName.UTF8String ?: "", sizeof out[n].name);
			out[n].scale = s.backingScaleFactor;
			n++;
		}
		return n;
	}
}

int wimy_window_frame(uint32_t wid, wimy_rect *out) {
	int i = find_win(wid);
	if (i < 0) return -1;
	CFTypeRef pos = NULL, size = NULL;
	CGPoint p;
	CGSize s;
	int ok = AXUIElementCopyAttributeValue(wins[i].el, kAXPositionAttribute, &pos) == kAXErrorSuccess &&
	         AXUIElementCopyAttributeValue(wins[i].el, kAXSizeAttribute, &size) == kAXErrorSuccess &&
	         AXValueGetValue(pos, kAXValueCGPointType, &p) && AXValueGetValue(size, kAXValueCGSizeType, &s);
	if (pos) CFRelease(pos);
	if (size) CFRelease(size);
	if (!ok) return -1;
	*out = (wimy_rect){p.x, p.y, s.width, s.height};
	return 0;
}

static AXError set_size(AXUIElementRef el, double w, double h) {
	CGSize s = {w, h};
	AXValueRef v = AXValueCreate(kAXValueCGSizeType, &s);
	AXError err = AXUIElementSetAttributeValue(el, kAXSizeAttribute, v);
	CFRelease(v);
	return err;
}

int wimy_window_set_frame(uint32_t wid, double x, double y, double w, double h, int *perr_out, int *serr_out) {
	int i = find_win(wid);
	if (i < 0) return -1;
	AXUIElementRef el = wins[i].el;
	// size, position, size (as AeroSpace does): macOS clamps a move
	// that would push the old size off-screen, and clamps a resize
	// that doesn't fit at the old position.
	set_size(el, w, h);
	CGPoint p = {x, y};
	AXValueRef pv = AXValueCreate(kAXValueCGPointType, &p);
	AXError perr = AXUIElementSetAttributeValue(el, kAXPositionAttribute, pv);
	CFRelease(pv);
	AXError serr = set_size(el, w, h);
	*perr_out = perr;
	*serr_out = serr;
	return (perr == kAXErrorSuccess && serr == kAXErrorSuccess) ? 0 : -1;
}

int wimy_window_set_position(uint32_t wid, double x, double y) {
	int i = find_win(wid);
	if (i < 0) return -1;
	CGPoint p = {x, y};
	AXValueRef pv = AXValueCreate(kAXValueCGPointType, &p);
	AXError err = AXUIElementSetAttributeValue(wins[i].el, kAXPositionAttribute, pv);
	CFRelease(pv);
	return err == kAXErrorSuccess ? 0 : (int)err;
}

void wimy_window_focus(uint32_t wid) {
	int i = find_win(wid);
	if (i < 0) return;
	AXUIElementPerformAction(wins[i].el, kAXRaiseAction);
	AXUIElementSetAttributeValue(wins[i].el, kAXMainAttribute, kCFBooleanTrue);
	[[NSRunningApplication runningApplicationWithProcessIdentifier:wins[i].pid] activateWithOptions:0];
}

// wimy_focus_none takes keyboard focus off every window, as clicking the
// desktop does, by activating Finder: after switching to an empty view
// the previously focused window sits parked in a corner and must not
// receive keystrokes. Returns Finder's pid (0 if it isn't running).
int wimy_focus_none(void) {
	@autoreleasepool {
		NSArray *finders = [NSRunningApplication runningApplicationsWithBundleIdentifier:@"com.apple.finder"];
		NSRunningApplication *finder = finders.firstObject;
		if (!finder) return 0;
		[finder activateWithOptions:0];
		return finder.processIdentifier;
	}
}

void wimy_window_close(uint32_t wid) {
	int i = find_win(wid);
	if (i < 0) return;
	CFTypeRef btn = NULL;
	if (AXUIElementCopyAttributeValue(wins[i].el, kAXCloseButtonAttribute, &btn) == kAXErrorSuccess && btn) {
		AXUIElementPerformAction((AXUIElementRef)btn, kAXPressAction);
	}
	if (btn) CFRelease(btn);
}

// --- decorations ---

// WimyDecoView is a frame panel's content: a layer filled with the
// border color and a "bar" sublayer showing the titlebar image. Clicks
// focus the window it decorates.
@interface WimyDecoView : NSView
@property uint32_t wid;
@property(strong) CAShapeLayer *ring;
@property(strong) CALayer *bar;
@end

@implementation WimyDecoView
- (BOOL)acceptsFirstMouse:(NSEvent *)e {
	return YES;
}
- (BOOL)isFlipped {
	return YES; // bar at the top, like the model's coordinates
}
- (void)mouseDown:(NSEvent *)e {
	goDecoClicked(self.wid);
}
@end

static NSMutableDictionary<NSNumber *, NSPanel *> *decos;

static NSPanel *deco_panel(uint32_t wid) {
	if (!decos) decos = [NSMutableDictionary dictionary];
	NSPanel *p = decos[@(wid)];
	if (p) return p;
	p = [[NSPanel alloc] initWithContentRect:NSMakeRect(0, 0, 1, 1)
	                               styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
	                                 backing:NSBackingStoreBuffered
	                                   defer:NO];
	p.backgroundColor = [NSColor clearColor];
	p.opaque = NO;
	p.hasShadow = NO;
	p.releasedWhenClosed = NO;
	p.hidesOnDeactivate = NO;
	p.becomesKeyOnlyIfNeeded = YES;
	p.collectionBehavior = NSWindowCollectionBehaviorManaged | NSWindowCollectionBehaviorIgnoresCycle |
	                       NSWindowCollectionBehaviorFullScreenNone;
	WimyDecoView *v = [[WimyDecoView alloc] initWithFrame:NSMakeRect(0, 0, 1, 1)];
	v.wid = wid;
	v.wantsLayer = YES;
	v.ring = [CAShapeLayer layer];
	v.ring.fillRule = kCAFillRuleEvenOdd;
	[v.layer addSublayer:v.ring];
	v.bar = [CALayer layer];
	v.bar.contentsGravity = kCAGravityResize;
	[v.layer addSublayer:v.bar];
	p.contentView = v;
	decos[@(wid)] = p;
	return p;
}

static CGColorRef argb_color(uint32_t c) {
	return CGColorCreateSRGB(((c >> 16) & 0xff) / 255.0, ((c >> 8) & 0xff) / 255.0, (c & 0xff) / 255.0,
	                         ((c >> 24) & 0xff) / 255.0);
}

void wimy_deco_update(uint32_t wid, wimy_rect frame, double barH, uint32_t fill_argb, int fill, wimy_rect content,
                      double radius, int front, uint32_t above_wid) {
	@autoreleasepool {
		NSPanel *p = deco_panel(wid);
		WimyDecoView *v = (WimyDecoView *)p.contentView;
		[CATransaction begin];
		[CATransaction setDisableActions:YES];
		[p setFrame:NSMakeRect(frame.x, frame.y, frame.w, frame.h) display:NO];
		v.ring.frame = CGRectMake(0, 0, frame.w, frame.h);
		v.ring.hidden = !fill;
		if (fill) {
			// the panel minus the window's rounded shape: the border
			// ring plus the corners the rounded window leaves open
			CGMutablePathRef path = CGPathCreateMutable();
			CGPathAddRect(path, NULL, CGRectMake(0, 0, frame.w, frame.h));
			CGRect in = CGRectMake(content.x, content.y, content.w, content.h);
			double r = fmin(radius, fmin(in.size.width, in.size.height) / 2);
			CGPathAddRoundedRect(path, NULL, in, r, r);
			v.ring.path = path;
			CGPathRelease(path);
			CGColorRef c = argb_color(fill_argb);
			v.ring.fillColor = c;
			CGColorRelease(c);
		}
		v.bar.frame = CGRectMake(0, 0, frame.w, barH);
		v.bar.hidden = barH <= 0;
		[CATransaction commit];
		if (front)
			[p orderWindow:NSWindowAbove relativeTo:(NSInteger)above_wid];
		else
			[p orderWindow:NSWindowBelow relativeTo:(NSInteger)wid];
	}
}

void wimy_deco_image(uint32_t wid, const void *bgra, int pw, int ph) {
	@autoreleasepool {
		NSPanel *p = deco_panel(wid);
		WimyDecoView *v = (WimyDecoView *)p.contentView;
		CFDataRef data = CFDataCreate(NULL, bgra, (CFIndex)pw * ph * 4);
		CGDataProviderRef prov = CGDataProviderCreateWithCFData(data);
		CGColorSpaceRef cs = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
		CGImageRef img = CGImageCreate(pw, ph, 8, 32, (size_t)pw * 4, cs,
		                               kCGBitmapByteOrder32Little | kCGImageAlphaPremultipliedFirst, prov, NULL, false,
		                               kCGRenderingIntentDefault);
		[CATransaction begin];
		[CATransaction setDisableActions:YES];
		v.bar.contents = (__bridge id)img;
		v.bar.contentsScale = p.backingScaleFactor;
		[CATransaction commit];
		CGImageRelease(img);
		CGColorSpaceRelease(cs);
		CGDataProviderRelease(prov);
		CFRelease(data);
	}
}

void wimy_deco_hide(uint32_t wid) {
	@autoreleasepool {
		[decos[@(wid)] orderOut:nil];
	}
}

void wimy_deco_destroy(uint32_t wid) {
	NSPanel *p = decos[@(wid)];
	if (!p) return;
	[p orderOut:nil];
	[p close];
	[decos removeObjectForKey:@(wid)];
}

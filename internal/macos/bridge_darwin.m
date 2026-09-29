// AppKit/Accessibility side of the macOS backend. Everything here
// runs on the main thread: AX observer sources are added to the main
// run loop, workspace notifications and Carbon hotkey events are
// delivered there too.
#import <AppKit/AppKit.h>
#include <ApplicationServices/ApplicationServices.h>
#include <Carbon/Carbon.h>
#include <stdlib.h>
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

static void track_window(pid_t pid, AXUIElementRef win, AXObserverRef obs) {
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
	wins[nwins++] = (tracked_win){wid, pid, (AXUIElementRef)CFRetain(win)};
	AXObserverAddNotification(obs, win, kAXUIElementDestroyedNotification, NULL);
	AXObserverAddNotification(obs, win, kAXTitleChangedNotification, NULL);

	char *title = copy_str(win, kAXTitleAttribute);
	char *subrole = copy_str(win, kAXSubroleAttribute);
	goWindowAdded(wid, pid, (char *)bundle_of(pid), title, subrole,
	              zoom_enabled(win), bool_attr(win, kAXMinimizedAttribute));
	free(title);
	free(subrole);
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
		track_window(pid, el, obs);
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
// often isn't ready for AX yet; retry a few times.
static void watch_pid(pid_t pid, int attempts) {
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
			dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 500 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
				watch_pid(pid, attempts - 1);
			});
		}
		return;
	}
	CFRunLoopAddSource(CFRunLoopGetMain(), AXObserverGetRunLoopSource(obs), kCFRunLoopDefaultMode);
	if (napps == capapps) {
		capapps = capapps ? capapps * 2 : 32;
		apps = realloc(apps, capapps * sizeof *apps);
	}
	apps[napps++] = (tracked_app){pid, obs, app};

	CFArrayRef list = NULL;
	if (AXUIElementCopyAttributeValue(app, kAXWindowsAttribute, (CFTypeRef *)&list) == kAXErrorSuccess && list) {
		for (CFIndex i = 0; i < CFArrayGetCount(list); i++)
			track_window(pid, (AXUIElementRef)CFArrayGetValueAtIndex(list, i), obs);
		CFRelease(list);
	}
}

static void watch_app(NSRunningApplication *app) {
	if (app.activationPolicy != NSApplicationActivationPolicyRegular) return;
	watch_pid(app.processIdentifier, 5);
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

void wimy_app_init(void) {
	[NSApplication sharedApplication];
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
	AXUIElementRef sys = AXUIElementCreateSystemWide();
	AXUIElementSetMessagingTimeout(sys, 1.0);
	CFRelease(sys);
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

void wimy_start_tracking(void) {
	NSNotificationCenter *wc = [[NSWorkspace sharedWorkspace] notificationCenter];
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
	NSArray<NSScreen *> *screens = [NSScreen screens];
	int n = 0;
	for (NSScreen *s in screens) {
		if (n == max) break;
		NSRect f = s.frame, v = s.visibleFrame;
		out[n].frame = (wimy_rect){f.origin.x, f.origin.y, f.size.width, f.size.height};
		out[n].visible = (wimy_rect){v.origin.x, v.origin.y, v.size.width, v.size.height};
		out[n].display = [s.deviceDescription[@"NSScreenNumber"] unsignedIntValue];
		strlcpy(out[n].name, s.localizedName.UTF8String ?: "", sizeof out[n].name);
		n++;
	}
	return n;
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

void wimy_window_focus(uint32_t wid) {
	int i = find_win(wid);
	if (i < 0) return;
	AXUIElementPerformAction(wins[i].el, kAXRaiseAction);
	AXUIElementSetAttributeValue(wins[i].el, kAXMainAttribute, kCFBooleanTrue);
	[[NSRunningApplication runningApplicationWithProcessIdentifier:wins[i].pid] activateWithOptions:0];
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

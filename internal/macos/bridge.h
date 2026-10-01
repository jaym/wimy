// C API between the Go backend and bridge_darwin.m. Unless noted,
// every function must be called on the main thread.
#pragma once
#include <stdint.h>

typedef struct { double x, y, w, h; } wimy_rect;

typedef struct {
	wimy_rect frame;   // AppKit coordinates (bottom-left origin)
	wimy_rect visible; // frame minus menu bar and Dock
	uint32_t display;  // CGDirectDisplayID
	char name[128];    // localizedName, UTF-8
	double scale;      // backingScaleFactor
} wimy_screen;

int wimy_ax_trusted(int prompt);
void wimy_app_init(void);
void wimy_app_run(void);   // returns after wimy_app_stop
void wimy_app_stop(void);
void wimy_dispatch(uintptr_t handle); // any thread: goRunDispatched(handle) on the main queue
void wimy_schedule_apply(void);       // goApply on the next main-queue pass
void wimy_schedule_apply_after(int ms); // goApply after a delay
void wimy_start_tracking(void);       // workspace + AX observers; reports existing windows
int wimy_screens(wimy_screen *out, int max);

// Event tap for bindings Carbon can't deliver (Option-only combos). It
// runs on its own thread: goKeyDown is called there and decides whether
// to swallow each key-down. 0 ok, -1 failed.
int wimy_start_keytap(void);
int wimy_start_gesturetap(void); // listen-only trackpad tap (swipes), main thread
int wimy_secure_input_pid(void); // 0 when no app holds secure input
// app_name writes pid's app name (UTF-8, NUL-terminated) to out.
void wimy_app_name(int pid, char *out, int max);

// Carbon hotkeys: goHotKey(id) is called on each press. Returns 0 or
// the OSStatus (e.g. eventHotKeyExistsErr when another app owns it).
int wimy_hotkey_register(uint32_t id, uint16_t code, uint32_t mods);
void wimy_hotkeys_clear(void);

int wimy_window_frame(uint32_t wid, wimy_rect *out); // AX coordinates (top-left origin)
int wimy_window_set_position(uint32_t wid, double x, double y); // 0 or the AXError
uint32_t wimy_focused_window(void); // frontmost app's focused tracked window, or 0
// The windows app pid lists right now (only the visible tab of each tab
// group): their CGWindowIDs, up to max. Returns how many.
int wimy_app_windows(int pid, uint32_t *out, int max);
void wimy_report_window(uint32_t wid); // goWindowAdded for a tracked window
void wimy_start_secure_input_poll(void); // goSecureInputTick every 2s
int wimy_focus_none(void); // activate Finder (keys go nowhere); returns its pid or 0

// Decorations: one frame panel per window, directly behind it (or in
// front, for a stack strip whose window is parked). frame is in AppKit
// coordinates; the titlebar image fills the top barH points; fill is
// the border color (0xAARRGGBB) drawn behind the window. A click calls
// goDecoClicked(wid).
// content is the window's area inside the panel (top-left origin): the
// fill is a ring around it plus its corners outside a rounded rect of
// the given radius, so translucent windows aren't tinted. A strip
// (front) is ordered just above window above_wid (0: in front of all).
void wimy_deco_update(uint32_t wid, wimy_rect frame, double barH, uint32_t fill_argb, int fill, wimy_rect content,
                      double radius, int front, uint32_t above_wid);
void wimy_deco_image(uint32_t wid, const void *bgra, int pw, int ph); // premultiplied BGRA pixels
void wimy_deco_hide(uint32_t wid);
void wimy_deco_destroy(uint32_t wid);

// Menu bar item. flags per item: 1 enabled, 2 checked, 4 separator.
// A click calls goMenuItem(index).
void wimy_status_set(const char *title, int n, const char **labels, const int *flags);
void wimy_status_remove(void);

// Start at login via SMAppService (Contents/Library/LaunchAgents/
// io.github.jaym.wimy.plist). Returns -1 when not running from
// Wimy.app, else 0 off, 1 on, 2 needs approval.
int wimy_in_app_bundle(void);           // running from Wimy.app
int wimy_login_legacy_unregister(void);  // drop an SMAppService login item: 1 done, 0 none, -1 failed
void wimy_open_accessibility_settings(void);
void wimy_start_trust_poll(void); // goTrustTick every second
// Returns 0 on success; the AXError of the position and final size
// calls go to *perr and *serr (apps sometimes report an error for a
// frame they did apply, so callers check by reading the frame back).
int wimy_window_set_frame(uint32_t wid, double x, double y, double w, double h, int *perr, int *serr);
void wimy_window_focus(uint32_t wid);
void wimy_window_close(uint32_t wid);

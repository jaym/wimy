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
} wimy_screen;

int wimy_ax_trusted(int prompt);
void wimy_app_init(void);
void wimy_app_run(void);   // returns after wimy_app_stop
void wimy_app_stop(void);
void wimy_dispatch(uintptr_t handle); // any thread: goRunDispatched(handle) on the main queue
void wimy_schedule_apply(void);       // goApply on the next main-queue pass
void wimy_start_tracking(void);       // workspace + AX observers; reports existing windows
int wimy_start_keytap(void);          // 0 ok, -1 failed
int wimy_screens(wimy_screen *out, int max);

int wimy_window_frame(uint32_t wid, wimy_rect *out); // AX coordinates (top-left origin)
int wimy_window_set_frame(uint32_t wid, double x, double y, double w, double h);
void wimy_window_focus(uint32_t wid);
void wimy_window_close(uint32_t wid);

# macOS port, Phase 1: tiling + keys on one screen (go/no-go spike) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A first `internal/macos` backend that tiles the primary screen's windows through the Accessibility API and runs wimy's key bindings from a CGEventTap, so we can answer the spec's go/no-go question: is AX fast enough to feel like wimy?

**Architecture:** `macos.Backend` embeds `*backend.Core` (Phase 0) and implements `backend.Platform`. The Go main goroutine is locked to the main thread and runs `NSApplication` (accessory policy); every AX/AppKit call happens there. An Objective-C file (`bridge_darwin.m`) owns AX observers, the window table keyed by `CGWindowID`, the key event tap and frame setting, and calls exported Go functions for events. Go coalesces events and commands into one `apply()` per main-queue pass: drain the queue → `State.Layout()` → set frames only for windows whose placement changed → focus. Pure translation logic (key codes, modifier flags, coordinate flip, float heuristic, frame diffing) lives in cgo-free files with unit tests that also run on Linux.

**Tech Stack:** Go 1.26 + cgo, Objective-C (ARC), AppKit, ApplicationServices (AX, CGEventTap), macOS 26.

**Spec:** `plans/macos-port.md` ("Phase 1 — macOS spike", "Concept mapping", "Coordinate systems", "Option key gotcha").

## Global Constraints

- All cgo lives behind `//go:build darwin` (file suffix `_darwin`); `CGO_ENABLED=0 GOOS=linux go build ./...` keeps working.
- Linux behavior unchanged; the Linux CI job (all e2e suites) stays green.
- Every AX/AppKit call runs on the main thread. Other goroutines reach it only through `dispatch_async` to the main queue; never block the main thread waiting on itself.
- One command registry: key bindings enqueue command strings into `backend.Core` exactly like river's `Enqueue`.
- `internal/wm` unchanged.
- Bindings match modifiers exactly (Shift, Control, Option, Command); Caps Lock, Fn and the numeric-pad flag are ignored (macOS sets Fn/numeric-pad on arrow and F keys).
- `Mod` defaults to Option on darwin (Phase 0).
- `go vet`, gofmt clean on darwin and `CGO_ENABLED=0 GOOS=linux`.
- Commits end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.

## Scope (from the spec) and what is deliberately left for later

In: main-thread NSApplication, DoSync equivalent, coalesced apply, AX permission check, enumerate + track windows of regular apps, tile standard windows / float the rest, CGEventTap key bindings, the darwin keycode table (moved here from Phase 0), apply the model's focus to the real window, close a window (Mod-Shift-c), retile timing log for the go/no-go measurement.

Out (later phases, per spec): hiding windows of other views (Phase 2 — until then `Hidden` placements are left where they are, so no window can get lost and the crash-safety machinery isn't needed yet), syncing external focus changes back into the model (Phase 2), multiple screens (Phase 2 — only the primary screen is an output), minimized windows (ignored), borders/titlebars (Phase 3 — `TitlebarHeight` forced to 0), mouse (Phase 4), app bundle (Phase 4).

## Review Focus

1. Arrow and F-key bindings: macOS sets the Fn and numeric-pad flags on those keys, so an exact-flags match would never fire. Pinned by `TestMatchIgnoresFnAndNumpad`.
2. A key held down: autorepeat must not run the command again, but must still be swallowed. Pinned by `TestKeyRepeatSwallowedNotRun`.
3. An unbound Option combo (typing é with Option-e) must pass through to the app. Pinned by `TestMatchUnboundPassesThrough`.
4. A config binding whose key has no macOS keycode must be reported and skipped, not crash or silently bind the wrong key. Pinned by `TestNewBindingsReportsUnsupported`.
5. The primary screen's `visibleFrame` flip: the model's usable area must start below the menu bar, not above the Dock. Pinned by `TestToModelFlipsY`.

## File structure

| file | responsibility |
|---|---|
| `internal/macos/doc.go` | package doc |
| `internal/macos/keys.go` + `keys_test.go` | keysym → macOS keycode table, CGEventFlags → modifier mask, `Bindings` |
| `internal/macos/geom.go` + `geom_test.go` | AppKit → model coordinates, float heuristic, applied-frame cache |
| `internal/macos/bridge.h` | C API between Go and Objective-C |
| `internal/macos/bridge_darwin.m` | AppKit app, AX observers, window table, event tap, frames, focus, close |
| `internal/macos/backend_darwin.go` | `Backend`: Run/Shutdown/Snapshot/Wake/apply + exported callbacks |
| `cmd/wimy/backend_darwin.go` | `newBackend` → `macos.New` |
| `plans/macos-port.md`, `AGENTS.md` | Phase 1 status, measurements, macOS gotchas |

---

### Task 1: Key codes, modifier flags and bindings (pure)

**Interfaces:**
- Produces: `type Bindings map[combo]string`; `func NewBindings(binds []config.Bind) (Bindings, []string)`; `func (b Bindings) Match(code uint16, flags uint64) (string, bool)`; `func keyDown(b Bindings, code uint16, flags uint64, repeat bool) (cmd string, run, swallow bool)`; `func modsFromFlags(flags uint64) uint32`.

- [ ] **Step 1: Write the failing tests**

File: internal/macos/keys_test.go
```go
package macos

import (
	"slices"
	"testing"

	"wimy/internal/config"
)

// CGEventFlags bits, as delivered by the event tap.
const (
	tShift   = 1 << 17
	tControl = 1 << 18
	tOption  = 1 << 19
	tCommand = 1 << 20
	tNumpad  = 1 << 21
	tFn      = 1 << 23
	tCaps    = 1 << 16
)

func binds(t *testing.T, cfgSrc map[string]string) []config.Bind {
	t.Helper()
	var out []config.Bind
	for combo, cmd := range cfgSrc {
		b, err := config.ParseBind("Mod1", combo, cmd)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func TestModsFromFlags(t *testing.T) {
	got := modsFromFlags(tShift | tOption | tCaps | tFn | tNumpad)
	if got != config.ModShift|config.Mod1 {
		t.Errorf("modsFromFlags = %d, want Shift|Mod1", got)
	}
	if got := modsFromFlags(tControl | tCommand); got != config.ModCtrl|config.Mod4 {
		t.Errorf("ctrl|cmd = %d", got)
	}
}

func TestMatchExactModifiers(t *testing.T) {
	b, bad := NewBindings(binds(t, map[string]string{"Mod-h": "focus left", "Mod-Shift-h": "move left"}))
	if len(bad) != 0 {
		t.Fatalf("unsupported: %v", bad)
	}
	if cmd, ok := b.Match(0x04, tOption); !ok || cmd != "focus left" {
		t.Errorf("Option-h = %q %v", cmd, ok)
	}
	if cmd, ok := b.Match(0x04, tOption|tShift); !ok || cmd != "move left" {
		t.Errorf("Option-Shift-h = %q %v", cmd, ok)
	}
	if _, ok := b.Match(0x04, tOption|tCommand); ok {
		t.Errorf("Option-Cmd-h matched; modifiers must match exactly")
	}
}

func TestMatchIgnoresFnAndNumpad(t *testing.T) {
	b, _ := NewBindings(binds(t, map[string]string{"Mod-Left": "focus left", "Mod-F1": "view 1"}))
	if cmd, ok := b.Match(0x7B, tOption|tFn|tNumpad); !ok || cmd != "focus left" {
		t.Errorf("Option-Left with fn/numpad flags = %q %v", cmd, ok)
	}
	if _, ok := b.Match(0x7A, tOption|tFn); !ok {
		t.Errorf("Option-F1 with fn flag did not match")
	}
}

func TestMatchUnboundPassesThrough(t *testing.T) {
	b, _ := NewBindings(binds(t, map[string]string{"Mod-h": "focus left"}))
	if _, ok := b.Match(0x0E, tOption); ok { // Option-e types an accent
		t.Errorf("unbound Option-e was matched")
	}
}

func TestKeyRepeatSwallowedNotRun(t *testing.T) {
	b, _ := NewBindings(binds(t, map[string]string{"Mod-h": "focus left"}))
	if cmd, run, swallow := keyDown(b, 0x04, tOption, false); !run || !swallow || cmd != "focus left" {
		t.Errorf("first press: cmd=%q run=%v swallow=%v", cmd, run, swallow)
	}
	if _, run, swallow := keyDown(b, 0x04, tOption, true); run || !swallow {
		t.Errorf("autorepeat: run=%v swallow=%v, want false true", run, swallow)
	}
	if _, run, swallow := keyDown(b, 0x0E, tOption, false); run || swallow {
		t.Errorf("unbound key: run=%v swallow=%v, want false false", run, swallow)
	}
}

func TestNewBindingsReportsUnsupported(t *testing.T) {
	bs := binds(t, map[string]string{"Mod-h": "focus left"})
	bs = append(bs, config.Bind{Combo: "Mod-ä", Mods: config.Mod1, Keysym: 0xe4, Command: "x"})
	b, bad := NewBindings(bs)
	if !slices.Equal(bad, []string{"Mod-ä"}) {
		t.Errorf("unsupported = %v", bad)
	}
	if len(b) != 1 {
		t.Errorf("%d bindings, want 1", len(b))
	}
}

func TestDefaultBindingsAllMap(t *testing.T) {
	_, bad := NewBindings(config.Default().Binds)
	if len(bad) != 0 {
		t.Errorf("default bindings without a macOS keycode: %v", bad)
	}
}
```

`config.ParseBind` does not exist yet: `parseBind` is an unexported method using the config's `ModMask`. Add a small exported wrapper in `internal/config/config.go` (tests in other packages need to build binds without writing KDL files):

File: internal/config/parsebind.go
```go
package config

// ParseBind parses a key combination like "Mod-Shift-h" with the
// given primary modifier name (e.g. "Mod4", "Option") into a Bind.
func ParseBind(mod, combo, cmd string) (Bind, error) {
	mask, err := parseModName(mod)
	if err != nil {
		return Bind{}, err
	}
	c := &Config{Mod: mod, ModMask: mask}
	return c.parseBind(combo, cmd)
}
```

Run: `go test ./internal/macos/`
Expected: FAIL to compile (`undefined: modsFromFlags`, `NewBindings`, `keyDown`).

- [ ] **Step 2: Implement**

File: internal/macos/doc.go
```go
// Package macos is wimy's macOS backend: an ordinary app with the
// Accessibility permission that tiles other apps' windows through the
// AX API and runs wimy's key bindings from a CGEventTap. Pure
// translation logic (key codes, coordinates, heuristics) is in
// cgo-free files so it is unit-tested on every OS; the AppKit/AX side
// is in the _darwin files. See plans/macos-port.md.
package macos
```

File: internal/macos/keys.go
```go
package macos

import "wimy/internal/config"

// CGEventFlags modifier bits (CGEventTypes.h). Caps Lock (1<<16), the
// numeric-pad bit (1<<21, set on arrow keys) and Fn (1<<23, set on
// arrow and F keys) are deliberately not modifiers for bindings.
const (
	flagShift   = 1 << 17
	flagControl = 1 << 18
	flagOption  = 1 << 19
	flagCommand = 1 << 20
)

// modsFromFlags converts CGEventFlags to config modifier masks:
// Option is Mod1 (Alt) and Command is Mod4 (Super).
func modsFromFlags(flags uint64) uint32 {
	var m uint32
	if flags&flagShift != 0 {
		m |= config.ModShift
	}
	if flags&flagControl != 0 {
		m |= config.ModCtrl
	}
	if flags&flagOption != 0 {
		m |= config.Mod1
	}
	if flags&flagCommand != 0 {
		m |= config.Mod4
	}
	return m
}

// keycodes maps the keysyms config.parseKeysym produces to macOS
// virtual key codes (Carbon kVK_*). The kVK_ANSI_* codes are physical
// positions on an ANSI keyboard, independent of the active layout, so
// combos keep naming the physical key as on Linux.
var keycodes = map[uint32]uint16{
	'a': 0x00, 's': 0x01, 'd': 0x02, 'f': 0x03, 'h': 0x04, 'g': 0x05,
	'z': 0x06, 'x': 0x07, 'c': 0x08, 'v': 0x09, 'b': 0x0B, 'q': 0x0C,
	'w': 0x0D, 'e': 0x0E, 'r': 0x0F, 'y': 0x10, 't': 0x11, 'o': 0x1F,
	'u': 0x20, 'i': 0x22, 'p': 0x23, 'l': 0x25, 'j': 0x26, 'k': 0x28,
	'n': 0x2D, 'm': 0x2E,
	'1': 0x12, '2': 0x13, '3': 0x14, '4': 0x15, '5': 0x17, '6': 0x16,
	'7': 0x1A, '8': 0x1C, '9': 0x19, '0': 0x1D,
	'=': 0x18, '-': 0x1B, ']': 0x1E, '[': 0x21, '\'': 0x27, ';': 0x29,
	'\\': 0x2A, ',': 0x2B, '/': 0x2C, '.': 0x2F, '`': 0x32,
	0x0020: 0x31, // space
	0xff0d: 0x24, // Return
	0xff09: 0x30, // Tab
	0xff08: 0x33, // BackSpace (kVK_Delete)
	0xff1b: 0x35, // Escape
	0xffff: 0x75, // Delete (kVK_ForwardDelete)
	0xff63: 0x72, // Insert (kVK_Help)
	0xff50: 0x73, // Home
	0xff57: 0x77, // End
	0xff55: 0x74, // Prior / Page Up
	0xff56: 0x79, // Next / Page Down
	0xff51: 0x7B, // Left
	0xff53: 0x7C, // Right
	0xff54: 0x7D, // Down
	0xff52: 0x7E, // Up
	0xffbe: 0x7A, 0xffbf: 0x78, 0xffc0: 0x63, 0xffc1: 0x76, // F1-F4
	0xffc2: 0x60, 0xffc3: 0x61, 0xffc4: 0x62, 0xffc5: 0x64, // F5-F8
	0xffc6: 0x65, 0xffc7: 0x6D, 0xffc8: 0x67, 0xffc9: 0x6F, // F9-F12
}

// combo is a physical key plus an exact modifier mask.
type combo struct {
	code uint16
	mods uint32
}

// Bindings maps physical key combos to command strings.
type Bindings map[combo]string

// NewBindings resolves config bindings to macOS key codes. Combos
// whose key has no macOS key code are returned so they can be
// reported; they are not bound.
func NewBindings(binds []config.Bind) (Bindings, []string) {
	out := make(Bindings, len(binds))
	var unsupported []string
	for _, b := range binds {
		code, ok := keycodes[b.Keysym]
		if !ok {
			unsupported = append(unsupported, b.Combo)
			continue
		}
		out[combo{code: code, mods: b.Mods}] = b.Command
	}
	return out, unsupported
}

// Match returns the command bound to a key-down with the given
// CGEventFlags. Modifiers must match exactly.
func (b Bindings) Match(code uint16, flags uint64) (string, bool) {
	cmd, ok := b[combo{code: code, mods: modsFromFlags(flags)}]
	return cmd, ok
}

// keyDown decides what the event tap does with a key-down: swallow
// it when it is bound, and run the command only on the initial press,
// not on autorepeat. Unbound keys pass through to the app.
func keyDown(b Bindings, code uint16, flags uint64, repeat bool) (cmd string, run, swallow bool) {
	cmd, ok := b.Match(code, flags)
	if !ok {
		return "", false, false
	}
	return cmd, !repeat, true
}
```

Run: `go test ./internal/macos/ ./internal/config/`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/macos internal/config/parsebind.go
git commit -m "Add macOS key code table and binding matcher

Maps config keysyms to layout-independent kVK codes and CGEventFlags
to wimy's modifier masks. Fn and numeric-pad flags are ignored because
macOS sets them on arrow and F keys; autorepeat is swallowed but not
re-run. Pure Go, so the tests run on Linux CI as well.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Coordinates, float heuristic, applied frames (pure)

**Interfaces:**
- Produces: `type Frame struct{ X, Y, W, H float64 }`; `func toModel(f Frame, primaryH float64) wm.Rect`; `func ShouldFloat(subrole string, hasZoom bool) bool`; `type frames map[wm.WindowID]wm.Rect` with `func (f frames) changed(id wm.WindowID, r wm.Rect) bool` and `func (f frames) forget(id wm.WindowID)`; `func sameFrame(a, b wm.Rect) bool`.

- [ ] **Step 1: Write the failing tests**

File: internal/macos/geom_test.go
```go
package macos

import (
	"testing"

	"wimy/internal/wm"
)

func TestToModelFlipsY(t *testing.T) {
	// 1512x982 primary screen, 33pt menu bar, 70pt Dock at the bottom:
	// AppKit visibleFrame origin is bottom-left, above the Dock.
	visible := Frame{X: 0, Y: 70, W: 1512, H: 982 - 70 - 33}
	got := toModel(visible, 982)
	want := wm.Rect{X: 0, Y: 33, W: 1512, H: 879}
	if got != want {
		t.Errorf("toModel = %+v, want %+v", got, want)
	}
}

func TestToModelSecondaryScreenAbove(t *testing.T) {
	// a screen placed above the primary one has a negative top-left y
	got := toModel(Frame{X: 200, Y: 982, W: 1920, H: 1080}, 982)
	want := wm.Rect{X: 200, Y: -1080, W: 1920, H: 1080}
	if got != want {
		t.Errorf("toModel = %+v, want %+v", got, want)
	}
}

func TestToModelRounds(t *testing.T) {
	got := toModel(Frame{X: 0.4, Y: 0.6, W: 100.5, H: 50.4}, 100)
	want := wm.Rect{X: 0, Y: 49, W: 101, H: 50}
	if got != want {
		t.Errorf("toModel = %+v, want %+v", got, want)
	}
}

func TestShouldFloat(t *testing.T) {
	cases := []struct {
		subrole string
		zoom    bool
		want    bool
	}{
		{"AXStandardWindow", true, false},
		{"AXStandardWindow", false, true}, // fixed-size window (e.g. a preferences pane)
		{"AXDialog", true, true},
		{"AXFloatingWindow", false, true},
		{"", true, true},
	}
	for _, c := range cases {
		if got := ShouldFloat(c.subrole, c.zoom); got != c.want {
			t.Errorf("ShouldFloat(%q, %v) = %v, want %v", c.subrole, c.zoom, got, c.want)
		}
	}
}

func TestFramesChanged(t *testing.T) {
	f := frames{}
	r := wm.Rect{X: 1, Y: 2, W: 3, H: 4}
	if !f.changed(7, r) {
		t.Errorf("first placement not reported as changed")
	}
	if f.changed(7, r) {
		t.Errorf("same placement reported as changed")
	}
	if !f.changed(7, wm.Rect{X: 1, Y: 2, W: 3, H: 5}) {
		t.Errorf("resized placement not reported as changed")
	}
	f.forget(7)
	if !f.changed(7, r) {
		t.Errorf("forgotten window not reported as changed")
	}
}

func TestSameFrameTolerance(t *testing.T) {
	a := wm.Rect{X: 10, Y: 10, W: 100, H: 100}
	if !sameFrame(a, wm.Rect{X: 11, Y: 9, W: 101, H: 99}) {
		t.Errorf("1pt rounding difference treated as different")
	}
	if sameFrame(a, wm.Rect{X: 10, Y: 10, W: 100, H: 140}) {
		t.Errorf("40pt difference treated as same")
	}
}
```

Run: `go test ./internal/macos/`
Expected: FAIL to compile (`undefined: Frame`).

- [ ] **Step 2: Implement**

File: internal/macos/geom.go
```go
package macos

import (
	"math"

	"wimy/internal/wm"
)

// Frame is a rectangle in AppKit screen coordinates, in points: the
// origin is the bottom-left corner of the primary screen and y grows
// upwards.
type Frame struct{ X, Y, W, H float64 }

// toModel converts an AppKit frame to the model's coordinates: global
// points with the origin at the primary screen's top-left and y
// growing downwards — the same space the AX API uses for window
// positions. primaryH is the primary screen's height.
func toModel(f Frame, primaryH float64) wm.Rect {
	return wm.Rect{
		X: int32(math.Round(f.X)),
		Y: int32(math.Round(primaryH - f.Y - f.H)),
		W: int32(math.Round(f.W)),
		H: int32(math.Round(f.H)),
	}
}

// ShouldFloat reports whether a new window floats instead of tiling:
// anything that isn't a standard window (dialogs, sheets, panels), and
// standard windows without a zoom button (fixed-size windows such as
// preference panes), the same heuristic AeroSpace uses.
func ShouldFloat(subrole string, hasZoom bool) bool {
	return subrole != "AXStandardWindow" || !hasZoom
}

// frames remembers the last frame requested for each window so apply
// only talks to AX for windows whose placement changed.
type frames map[wm.WindowID]wm.Rect

// changed records r for id and reports whether it differs from the
// last recorded frame.
func (f frames) changed(id wm.WindowID, r wm.Rect) bool {
	if old, ok := f[id]; ok && old == r {
		return false
	}
	f[id] = r
	return true
}

// forget drops id, so its next placement is applied again.
func (f frames) forget(id wm.WindowID) { delete(f, id) }

// sameFrame reports whether an app's actual frame matches the
// requested one, allowing 1pt of rounding on every edge.
func sameFrame(a, b wm.Rect) bool {
	near := func(x, y int32) bool { return x-y <= 1 && y-x <= 1 }
	return near(a.X, b.X) && near(a.Y, b.Y) && near(a.W, b.W) && near(a.H, b.H)
}
```

Run: `go test ./internal/macos/ && CGO_ENABLED=0 GOOS=linux go vet ./internal/macos/`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/macos
git commit -m "Add macOS coordinate conversion and float heuristic

AppKit screen frames are bottom-left origin; the model and AX use
top-left global points. Non-standard windows and windows without a
zoom button float (AeroSpace's heuristic). A frame cache lets apply
skip AX calls for windows whose placement didn't change.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: The AppKit/AX bridge and the backend

This task has no unit-testable surface: it is verified by building on darwin (with cgo) and by the live run in Task 4. Keep all logic that *can* be tested in Tasks 1–2's files.

**Interfaces:**
- Consumes: `Bindings`, `NewBindings`, `keyDown` (Task 1); `Frame`, `toModel`, `ShouldFloat`, `frames`, `sameFrame` (Task 2); `backend.Core`, `backend.Platform` (Phase 0).
- Produces: `func New(cfg *config.Config, configArg string, notify func()) *Backend` with `Run(ctx) error`, `Shutdown()`, `Snapshot(func(*wm.State))` — the `wmBackend` interface in `cmd/wimy`.

- [ ] **Step 1: C API**

File: internal/macos/bridge.h
```c
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
```

- [ ] **Step 2: Objective-C bridge**

File: internal/macos/bridge_darwin.m
```objc
// AppKit/Accessibility side of the macOS backend. Everything here
// runs on the main thread: AX observer sources and the key event tap
// are added to the main run loop, workspace notifications are
// delivered on the main queue.
#import <AppKit/AppKit.h>
#include <ApplicationServices/ApplicationServices.h>
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

static int has_attr(AXUIElementRef el, CFStringRef attr) {
	CFTypeRef v = NULL;
	int ok = AXUIElementCopyAttributeValue(el, attr, &v) == kAXErrorSuccess && v;
	if (v) CFRelease(v);
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
	              has_attr(win, kAXZoomButtonAttribute), bool_attr(win, kAXMinimizedAttribute));
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

int wimy_start_keytap(void) {
	keytap = CGEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap, kCGEventTapOptionDefault,
	                          CGEventMaskBit(kCGEventKeyDown), keytap_cb, NULL);
	if (!keytap) return -1;
	CFRunLoopSourceRef src = CFMachPortCreateRunLoopSource(NULL, keytap, 0);
	CFRunLoopAddSource(CFRunLoopGetMain(), src, kCFRunLoopCommonModes);
	CFRelease(src);
	CGEventTapEnable(keytap, true);
	return 0;
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

int wimy_window_set_frame(uint32_t wid, double x, double y, double w, double h) {
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
```

Note on `activateWithOptions:0`: it is deprecated since macOS 14 in favor of `-activate`, but `-activate` on its own respects cooperative activation and may be refused for an app activated by a background agent. If the build warns, keep it and record the warning; Task 4 checks whether focusing works at all, and the spec's fallback (`_SLPSSetFrontProcessWithOptions`) is Phase 2 material.

- [ ] **Step 3: Go backend**

File: internal/macos/backend_darwin.go
```go
package macos

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework AppKit -framework ApplicationServices
#include "bridge.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/cgo"
	"time"

	"wimy/internal/backend"
	"wimy/internal/config"
	"wimy/internal/wm"
)

// AppKit must run on the main thread. Package initialization runs on
// the main goroutine, which is on the main thread; keep it there so
// Run (called from main) owns the thread.
func init() { runtime.LockOSThread() }

// current receives the callbacks exported to Objective-C. There is one
// backend per process.
var current *Backend

// Backend is the macOS backend. All fields except Core's queue are
// owned by the main thread.
type Backend struct {
	*backend.Core

	bindings  Bindings
	applied   frames
	known     map[wm.WindowID]bool
	output    string // model name of the managed (primary) screen
	scheduled bool   // an apply pass is queued on the main queue
	lastFocus wm.WindowID
	notify    func()
}

var (
	_ backend.Platform = (*Backend)(nil)
)

// New creates the macOS backend. notify is called on the main thread
// after every apply pass; it must not block.
func New(cfg *config.Config, configArg string, notify func()) *Backend {
	b := &Backend{applied: frames{}, known: make(map[wm.WindowID]bool), notify: notify}
	b.Core = backend.NewCore(cfg, configArg, b)
	b.rebind()
	current = b
	return b
}

func (b *Backend) rebind() {
	bs, unsupported := NewBindings(b.Cfg.Binds)
	for _, combo := range unsupported {
		log.Printf("bind %q: no macOS key code for this key; ignored", combo)
	}
	b.bindings = bs
}

// Run checks the Accessibility permission, starts tracking windows and
// keys, and runs the AppKit event loop until Quit or Shutdown. It must
// be called from the main goroutine.
func (b *Backend) Run(ctx context.Context) error {
	if C.wimy_ax_trusted(1) == 0 {
		exe, _ := os.Executable()
		return fmt.Errorf("wimy needs the Accessibility permission: allow %s (or the terminal that starts it) "+
			"in System Settings → Privacy & Security → Accessibility, then start wimy again", exe)
	}
	C.wimy_app_init()
	b.syncScreens()
	if C.wimy_start_keytap() != 0 {
		return errors.New("could not install the keyboard event tap; check the Accessibility permission")
	}
	C.wimy_start_tracking()
	b.StartAutostart()
	b.markDirty()
	C.wimy_app_run()
	return nil
}

// dispatch runs fn on the main thread's queue. Safe from any
// goroutine; it does not wait.
func dispatch(fn func()) {
	C.wimy_dispatch(C.uintptr_t(cgo.NewHandle(fn)))
}

//export goRunDispatched
func goRunDispatched(h C.uintptr_t) {
	handle := cgo.Handle(h)
	fn := handle.Value().(func())
	handle.Delete()
	fn()
}

// Shutdown stops the event loop. Safe from any goroutine.
func (b *Backend) Shutdown() { dispatch(func() { C.wimy_app_stop() }) }

// Snapshot runs fn on the main thread with exclusive access to the
// model. It must not be called from the main thread.
func (b *Backend) Snapshot(fn func(*wm.State)) {
	done := make(chan struct{})
	dispatch(func() {
		fn(b.State)
		close(done)
	})
	<-done
}

// Wake implements backend.Platform: schedule an apply pass, in which
// the command queue is drained.
func (b *Backend) Wake() { dispatch(b.markDirty) }

// markDirty schedules one apply pass for the next main-queue turn;
// further events before it runs coalesce into it. Main thread only.
func (b *Backend) markDirty() {
	if b.scheduled {
		return
	}
	b.scheduled = true
	C.wimy_schedule_apply()
}

// ApplyConfigChange implements backend.Platform. Borders and
// titlebars are drawn from Phase 3 on; only bindings apply here.
func (b *Backend) ApplyConfigChange(ch backend.ConfigChange) {
	if ch.Binds || ch.Mod {
		b.rebind()
	}
}

// Kill implements backend.Platform by pressing the window's close
// button.
func (b *Backend) Kill(id wm.WindowID) { C.wimy_window_close(C.uint32_t(id)) }

// Quit implements backend.Platform: wimy exits; windows stay where
// they are.
func (b *Backend) Quit() { C.wimy_app_stop() }

//export goApply
func goApply() { current.apply() }

// apply is one manage pass: run queued commands, lay out, and push
// changed frames and focus to AX.
func (b *Backend) apply() {
	b.scheduled = false
	b.DrainQueue()
	// wimy draws no titlebars on macOS until Phase 3: reserve no space
	b.State.TitlebarHeight = 0

	var moved []wm.Placement
	start := time.Now()
	for _, p := range b.State.Layout() {
		// hiding the windows of other views is Phase 2
		if p.Hidden || !b.applied.changed(p.ID, p.Rect) {
			continue
		}
		r := p.Rect
		if C.wimy_window_set_frame(C.uint32_t(p.ID), C.double(r.X), C.double(r.Y), C.double(r.W), C.double(r.H)) != 0 {
			log.Printf("window %d: setting its frame failed", p.ID)
		}
		moved = append(moved, p)
	}
	if len(moved) > 0 {
		elapsed := time.Since(start)
		log.Printf("retile: %d windows in %s (%s per window)", len(moved),
			elapsed.Round(time.Millisecond), (elapsed / time.Duration(len(moved))).Round(time.Millisecond))
		b.checkFrames(moved)
	}

	if f := b.State.Focused; f != 0 && f != b.lastFocus {
		C.wimy_window_focus(C.uint32_t(f))
	}
	b.lastFocus = b.State.Focused
	if b.notify != nil {
		b.notify()
	}
}

// checkFrames reads back the windows just moved and logs apps that
// refused the requested frame (minimum sizes, slow apps).
func (b *Backend) checkFrames(ps []wm.Placement) {
	for _, p := range ps {
		var f C.wimy_rect
		if C.wimy_window_frame(C.uint32_t(p.ID), &f) != 0 {
			continue
		}
		got := wm.Rect{X: int32(f.x), Y: int32(f.y), W: int32(f.w), H: int32(f.h)}
		if !sameFrame(got, p.Rect) {
			app := ""
			if w := b.State.Windows[p.ID]; w != nil {
				app = w.AppID
			}
			log.Printf("window %d (%s) is %+v, wanted %+v", p.ID, app, got, p.Rect)
		}
	}
}

// syncScreens makes the primary screen the model's only output.
// Multiple screens are Phase 2.
func (b *Backend) syncScreens() {
	var buf [16]C.wimy_screen
	n := int(C.wimy_screens(&buf[0], C.int(len(buf))))
	if n == 0 {
		return
	}
	s := buf[0]
	primaryH := float64(s.frame.h)
	name := C.GoString(&s.name[0])
	if name == "" {
		name = fmt.Sprintf("display-%d", uint32(s.display))
	}
	full := toModel(frameOf(s.frame), primaryH)
	usable := toModel(frameOf(s.visible), primaryH)
	switch {
	case b.output == "":
		b.State.AddOutput(name)
	case b.output != name:
		b.State.RenameOutput(b.output, name)
	}
	b.output = name
	b.State.SetOutputGeometry(name, full.X, full.Y, full.W, full.H)
	b.State.SetOutputUsable(name, usable.X, usable.Y, usable.W, usable.H)
}

func frameOf(r C.wimy_rect) Frame {
	return Frame{X: float64(r.x), Y: float64(r.y), W: float64(r.w), H: float64(r.h)}
}

//export goScreensChanged
func goScreensChanged() {
	current.syncScreens()
	current.markDirty()
}

//export goWindowAdded
func goWindowAdded(wid C.uint32_t, pid C.int, bundle, title, subrole *C.char, hasZoom, minimized C.int) {
	current.windowAdded(wm.WindowID(wid), C.GoString(bundle), C.GoString(title), C.GoString(subrole),
		hasZoom != 0, minimized != 0)
}

func (b *Backend) windowAdded(id wm.WindowID, bundle, title, subrole string, hasZoom, minimized bool) {
	// minimized windows are left alone; tracking (de)miniaturize is Phase 2
	if minimized || b.known[id] {
		return
	}
	b.known[id] = true
	floating := ShouldFloat(subrole, hasZoom)
	b.State.AddWindow(id, floating)
	b.State.SetAppID(id, bundle)
	b.State.SetTitle(id, title)
	if floating {
		// keep floating windows where the app put them
		var f C.wimy_rect
		if C.wimy_window_frame(C.uint32_t(id), &f) == 0 {
			b.State.SetFloatRect(id, wm.Rect{X: int32(f.x), Y: int32(f.y), W: int32(f.w), H: int32(f.h)})
		}
	}
	b.markDirty()
}

//export goWindowRemoved
func goWindowRemoved(wid C.uint32_t) {
	b, id := current, wm.WindowID(wid)
	if !b.known[id] {
		return
	}
	delete(b.known, id)
	b.applied.forget(id)
	b.State.RemoveWindow(id)
	b.markDirty()
}

//export goTitleChanged
func goTitleChanged(wid C.uint32_t, title *C.char) {
	b, id := current, wm.WindowID(wid)
	if b.known[id] {
		b.State.SetTitle(id, C.GoString(title))
		b.markDirty()
	}
}

//export goKeyDown
func goKeyDown(code C.uint16_t, flags C.uint64_t, repeat C.int) C.int {
	b := current
	cmd, run, swallow := keyDown(b.bindings, uint16(code), uint64(flags), repeat != 0)
	if run {
		b.Enqueue(cmd)
		b.markDirty()
	}
	if swallow {
		return 1
	}
	return 0
}
```

Check `wm.State.RenameOutput` exists (`grep -n "func (s \*State) RenameOutput" internal/wm/*.go`; river uses it). If `-Wno-deprecated-declarations` is enough to silence the `activateWithOptions:` warning, keep it; else note the warning.

- [ ] **Step 4: Wire `cmd/wimy`**

File: cmd/wimy/backend_darwin.go
```go
//go:build darwin

package main

import (
	"wimy/internal/config"
	"wimy/internal/macos"
)

// newBackend returns the macOS (Accessibility API) backend.
func newBackend(cfg *config.Config, configArg string, notify func()) (wmBackend, error) {
	return macos.New(cfg, configArg, notify), nil
}
```

- [ ] **Step 5: Build and vet**

Run: `go build ./... && go vet ./... && go test -race ./... && CGO_ENABLED=0 GOOS=linux go build ./... && CGO_ENABLED=0 GOOS=linux go vet ./... && gofmt -l cmd internal`
Expected: all pass; no C compiler warnings printed; gofmt lists only `gen.go`.

- [ ] **Step 6: Smoke test without the permission**

Run from a terminal that does *not* have Accessibility: `go build -o bin/wimy ./cmd/wimy && bin/wimy -log /dev/null; echo exit=$?`
Expected: the permission error message and `exit=1` (and macOS may show its permission prompt).

- [ ] **Step 7: Commit and push**

```bash
git add internal/macos cmd/wimy
git commit -m "Add the macOS backend spike: AX tiling and key bindings

internal/macos runs NSApplication on the locked main thread, tracks
regular apps' windows through AX observers keyed by CGWindowID
(_AXUIElementGetWindow), tiles the primary screen's windows via
kAXPosition/kAXSize, and runs bindings from a CGEventTap. Events and
queued commands coalesce into one apply pass per main-queue turn,
which logs retile timing for the Phase 1 go/no-go measurement.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
git push
```

---

### Task 4: Live run and the go/no-go measurement

This moves the user's real windows. **Ask the user before starting wimy on their desktop**, and tell them how to stop it (`wimyctl quit`, or Ctrl-C in the terminal).

- [ ] **Step 1: Grant permission.** The user adds their terminal app in System Settings → Privacy & Security → Accessibility (bare-binary runs are attributed to the terminal; the app bundle is Phase 4).

- [ ] **Step 2: Run.** `bin/wimy` in the foreground of that terminal; its log is `$TMPDIR/wimy.log` (and stderr).

- [ ] **Step 3: Checks** (record each result in `plans/macos-port.md`):
  - Existing standard windows on the primary screen tile into columns below the menu bar and above the Dock; dialogs/panels stay floating.
  - Option-Return spawns the terminal; the new window is tiled.
  - Option-h/l/j/k move focus (the real app focus follows); Option-Shift-h/l move windows between columns; Option-s/d/m switch column modes; Option-Shift-c closes the focused window.
  - Option-e still types an accent in a text field (unbound combos pass through).
  - `wimyctl state` works (socket `$TMPDIR/wimy.sock`).
  - `wimyctl quit` exits wimy.

- [ ] **Step 4: Measure.** Open 6 windows (Terminal, Safari, Finder, an Electron app such as VS Code or Slack, plus two more), then run `for m in stack default max default; do wimyctl run "mode $m"; sleep 1; done` a few times and collect the `retile:` lines and any `wanted` lines from the log. Go if a full re-tile of 6 windows stays well under ~100 ms and apps mostly accept their sizes; otherwise record the numbers and apps and stop for the Swift-fallback discussion.

- [ ] **Step 5: Record and commit.** Add a "Phase 1 results" subsection to `plans/macos-port.md` (timings, apps that refused sizes, focus reliability, anything surprising) and macOS gotchas to AGENTS.md. Commit and push.

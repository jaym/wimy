# macOS port, Phase 2: views, focus, multiple screens, SketchyBar — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the macOS backend usable day to day: switching views hides and shows windows (without ever losing one), focus changes made outside wimy (clicks, Cmd-Tab, Dock) flow into the model, every connected screen is an output, and the user's SketchyBar shows wimy's views and column mode.

**Architecture:** Everything decidable without AppKit stays pure and unit-tested in `internal/macos` (screen → output mapping, hide-corner geometry, hidden-window store, focus-echo suppression) or `internal/wm` (per-view column spread). The Objective-C bridge grows focus/minimize/hide observers and a position-only move; `backend_darwin.go` wires them. SketchyBar integration is a streaming shell plugin fed by `wimyctl subscribe`, like the waybar modules.

**Tech Stack:** Go 1.26 + cgo, Objective-C (ARC), AppKit, ApplicationServices (AX), bash + jq (SketchyBar plugin), KDL config.

**Spec:** `plans/macos-port.md` — "Phase 2", "Safety: never lose windows", "Concept mapping" (tags/views, focus, outputs, layer-shell usable area, waybar), "Phase 1 results".

## Global Constraints

- All cgo behind `_darwin` files; `CGO_ENABLED=0 GOOS=linux go build ./... && go vet ./...` keeps passing; Linux behavior and e2e suites unchanged.
- `internal/wm` stays pure; new model behavior there comes with tests.
- Every AX/AppKit call on the main thread; other threads use `dispatch`. `Snapshot` never on the main thread.
- One command registry; SketchyBar clicks go through `wimyctl run`.
- **Never lose a window**: every window wimy moves off-screen is recorded before it moves, restored on quit/SIGTERM/recovered panic, and on the next start if wimy died.
- The user's `~/nix-config` has uncommitted work: edit files there only as this plan says, never commit or run `darwin-rebuild` there — tell the user what to run.
- Commits end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.

## Scope

In (spec Phase 2): hide/show windows of other views via a screen corner, with the safety requirements; external focus sync (AX focused-window changes, app activation); all screens as outputs with screen-change handling; float heuristics already in place are kept. Pulled forward because the user installed SketchyBar: `bar-gap` config, a SketchyBar plugin (spec Phase 4 contrib), wiring into `~/nix-config`. Folded in from the Phase 1 review's deferred minors because they touch the same code: minimized and Cmd-H-hidden windows, startup focus seeded from the frontmost app, longer app-launch retry plus re-watch on activation, checked observer registration, `@autoreleasepool` around bridge entry points, a `darwin && !cgo` build stub, periodic secure-input check.

Out: titlebars/borders (Phase 3), mouse (Phase 4), app bundle/LaunchAgent (Phase 4), concurrent per-app AX calls.

## Review Focus

1. A screen whose right/bottom neighbor is another screen: a hidden window must not land on the neighbor. Pinned by `TestHidePositionAvoidsNeighbor`.
2. wimy killed with windows hidden: the next start restores them — by the hidden-window store, or by recognizing the hide corner when the store is gone. Pinned by `TestHiddenStoreRoundTrip`, `TestInHideCorner`.
3. Two focus changes in quick succession (Option-j twice): the AX echo of the first must not yank focus back. Pinned by `TestFocusEchoSuppressed`.
4. Menu bar visible *and* a 37pt SketchyBar at the top: the bar gap must not be added twice. Pinned by `TestOutputsForBarGapMenuBarVisible`.
5. Two identical monitors: output names stay unique. Pinned by `TestOutputsForDuplicateNames`.

## File structure

| file | responsibility |
|---|---|
| `internal/rpc/rpc.go` (+test) | darwin socket in a per-user 0700 dir, `/tmp/wimy-<uid>/wimy.sock` |
| `internal/config/config.go` (+test) | `bar-gap` setting |
| `internal/backend/reload.go` (+test) | `ConfigChange.BarGap` |
| `internal/wm/wm.go` (+test) | `SpreadColumns(view, maxCols)` |
| `internal/macos/screens.go` (+test) | screens → outputs (names, usable area, bar gap), output under a rect |
| `internal/macos/hide.go` (+test) | hide-corner position, hide-corner detection |
| `internal/macos/store.go` (+test) | hidden-window store (JSON, atomic write) |
| `internal/macos/focus.go` (+test) | focus-echo suppression |
| `internal/macos/bridge.h`, `bridge_darwin.m` | new observers and calls |
| `internal/macos/backend_darwin.go` | wiring |
| `cmd/wimy/backend_darwin.go`, `backend_darwin_nocgo.go` | cgo / no-cgo split |
| `contrib/macos/sketchybar/wimy.sh`, `README.md` | SketchyBar plugin |
| `~/nix-config/dotfiles/sketchybar/{sketchybarrc,plugins/wimy.sh}`, `~/nix-config/dotfiles/wimy/config.kdl`, `~/nix-config/modules/home-manager/default.nix` | user's setup |
| `README.md`, `AGENTS.md`, `plans/macos-port.md` | docs |

---

### Task 1: Per-user socket directory on macOS

launchd agents (SketchyBar) get no `TMPDIR`, so `$TMPDIR/wimy.sock` differs between wimy and a bar plugin. Use a fixed per-user directory, as tmux does.

**Interfaces:** Produces `socketPath(goos string, getenv func(string) string, tmp string, uid int) string`; `Listen` creates the directory 0700 and refuses one it doesn't own or that others can access.

- [ ] **Step 1: Tests (RED).** In `internal/rpc/rpc_test.go`, change `TestSocketPath`'s calls to `socketPath(c.goos, env(c.env), "/tmp", 501)` and the darwin expectations to:

```go
		{"darwin", "darwin", map[string]string{}, "/tmp/wimy-501/wimy.sock"},
		{"darwin ignores TMPDIR", "darwin", map[string]string{"TMPDIR": "/var/folders/x/T/"}, "/tmp/wimy-501/wimy.sock"},
```

and add:

```go
func TestPrepareSocketDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "wimy-test")
	if err := prepareSocketDir(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, %v; want 0700", fi.Mode().Perm(), err)
	}
	if err := prepareSocketDir(dir); err != nil {
		t.Errorf("existing private dir rejected: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := prepareSocketDir(dir); err == nil {
		t.Errorf("dir readable by others accepted")
	}
}
```

(imports `os`, `path/filepath`). Run `go test ./internal/rpc/` → FAIL.

- [ ] **Step 2: Implement.** In `rpc.go`: `SocketPath()` passes `os.Getuid()`. In `socketPath`, the darwin branch becomes `return filepath.Join("/tmp", fmt.Sprintf("wimy-%d", uid), "wimy.sock")` (comment: per-user dir like tmux; launchd agents such as SketchyBar get no TMPDIR). Add:

```go
// prepareSocketDir creates the socket's directory if needed and
// refuses one that isn't a private directory of this user: the socket
// accepts commands, so nobody else may create or replace it.
func prepareSocketDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 || (ok && int(st.Uid) != os.Getuid()) {
		return fmt.Errorf("socket directory %s must be a directory private to this user (mode 0700)", dir)
	}
	return nil
}
```

In `Listen`, before `os.RemoveAll(path)`: on darwin (`runtime.GOOS == "darwin"`) and when `WIMY_SOCKET` is unset, `prepareSocketDir(filepath.Dir(path))`. Run tests → PASS; `CGO_ENABLED=0 GOOS=linux go vet ./internal/rpc`.

- [ ] **Step 3: Commit** ("macOS: put the control socket in a private per-user directory").

---

### Task 2: `bar-gap` setting

**Interfaces:** Produces `config.Config.BarGap int32` (points, default 0, parsed from `bar-gap <n>`), `backend.ConfigChange.BarGap bool`.

- [ ] **Step 1: Tests (RED).** `internal/config/config_test.go`:

```go
func TestBarGap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.kdl")
	if err := os.WriteFile(path, []byte("bar-gap 37\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil || c.BarGap != 37 {
		t.Fatalf("bar-gap: %d, %v", c.BarGap, err)
	}
	if Default().BarGap != 0 {
		t.Errorf("default bar-gap %d, want 0", Default().BarGap)
	}
	if err := os.WriteFile(path, []byte("bar-gap -1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Errorf("negative bar-gap accepted")
	}
}
```

`internal/backend/reload_test.go`:

```go
func TestReloadBarGap(t *testing.T) {
	c, p, _, path := newReloadCore(t, "")
	if err := os.WriteFile(path, []byte("bar-gap 37\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Reload(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.changes, []ConfigChange{{BarGap: true}}) {
		t.Errorf("platform changes = %+v, want BarGap", p.changes)
	}
}
```

Run → FAIL.

- [ ] **Step 2: Implement.** Config field `BarGap int32` with doc "points reserved at the top of every screen for a bar the window manager doesn't know about (SketchyBar); macOS only — on Linux bars reserve space through the layer shell". Parse `case "bar-gap":` like `stack-strip` but allowing 0 (`v < 0` → error "bar-gap: want a non-negative integer"). `ConfigChange` gains `BarGap bool // reserved top space: re-read screen areas`, set in `applyConfig` from `old.BarGap != newCfg.BarGap`, reported as `"bar-gap"` right after `"stack-strip"`. river's `ApplyConfigChange` ignores it (no code change). Tests → PASS; full `go test ./...`.

- [ ] **Step 3: Commit** ("Add bar-gap: reserve space for a bar wimy doesn't manage").

---

### Task 3: Screens → outputs, per-view spread

**Interfaces:**
- `internal/macos/screens.go`: `type screenInfo struct{ Frame, Visible Frame; Display uint32; Name string }`; `type outputSpec struct{ Name string; Display uint32; Full, Usable wm.Rect }`; `func outputsFor(screens []screenInfo, barGap int32) []outputSpec`; `func outputAt(outs []outputSpec, r wm.Rect) int`.
- `internal/wm`: `func (s *State) SpreadColumns(view string, maxCols int)` (replaces the active-view variant).

- [ ] **Step 1: Tests (RED)** — `internal/macos/screens_test.go`:

```go
package macos

import (
	"testing"

	"wimy/internal/wm"
)

// laptop: 1512x982 primary; BenQ 2560x1707 to its right, top-aligned.
func twoScreens(visibleTopInset float64) []screenInfo {
	return []screenInfo{
		{Name: "Built-in", Display: 1, Frame: Frame{0, 0, 1512, 982}, Visible: Frame{0, 0, 1512, 982 - visibleTopInset}},
		{Name: "BenQ", Display: 2, Frame: Frame{1512, 982 - 1707, 2560, 1707}, Visible: Frame{1512, 982 - 1707, 2560, 1707 - visibleTopInset}},
	}
}

func TestOutputsForMenuBarHidden(t *testing.T) {
	outs := outputsFor(twoScreens(0), 37)
	if len(outs) != 2 {
		t.Fatalf("%d outputs", len(outs))
	}
	if want := (wm.Rect{X: 0, Y: 0, W: 1512, H: 982}); outs[0].Full != want {
		t.Errorf("full = %+v, want %+v", outs[0].Full, want)
	}
	if want := (wm.Rect{X: 0, Y: 37, W: 1512, H: 945}); outs[0].Usable != want {
		t.Errorf("usable = %+v, want %+v (bar gap below the top edge)", outs[0].Usable, want)
	}
	if want := (wm.Rect{X: 1512, Y: 37, W: 2560, H: 1670}); outs[1].Usable != want {
		t.Errorf("second usable = %+v, want %+v", outs[1].Usable, want)
	}
}

func TestOutputsForBarGapMenuBarVisible(t *testing.T) {
	// visible frame already excludes a 24pt menu bar; SketchyBar (37pt)
	// covers it, so the usable top is 37, not 24+37
	outs := outputsFor(twoScreens(24), 37)
	if outs[0].Usable.Y != 37 || outs[0].Usable.H != 945 {
		t.Errorf("usable = %+v, want Y 37 H 945", outs[0].Usable)
	}
	if outs := outputsFor(twoScreens(24), 0); outs[0].Usable.Y != 24 {
		t.Errorf("no gap: usable Y = %d, want 24 (menu bar)", outs[0].Usable.Y)
	}
}

func TestOutputsForDuplicateNames(t *testing.T) {
	s := twoScreens(0)
	s[0].Name, s[1].Name = "DELL U2720Q", "DELL U2720Q"
	outs := outputsFor(s, 0)
	if outs[0].Name == outs[1].Name {
		t.Errorf("duplicate output names: %q", outs[0].Name)
	}
	if outs[0].Name != "DELL U2720Q" || outs[1].Name != "DELL U2720Q (2)" {
		t.Errorf("names = %q, %q", outs[0].Name, outs[1].Name)
	}
}

func TestOutputsForUnnamed(t *testing.T) {
	s := twoScreens(0)
	s[1].Name = ""
	if got := outputsFor(s, 0)[1].Name; got != "display-2" {
		t.Errorf("unnamed screen = %q, want display-2", got)
	}
}

func TestOutputAt(t *testing.T) {
	outs := outputsFor(twoScreens(0), 0)
	if i := outputAt(outs, wm.Rect{X: 2000, Y: 100, W: 400, H: 300}); i != 1 {
		t.Errorf("window on the BenQ -> output %d", i)
	}
	if i := outputAt(outs, wm.Rect{X: 1400, Y: 100, W: 400, H: 300}); i != 1 {
		t.Errorf("window straddling, centre on the BenQ -> output %d", i)
	}
	if i := outputAt(outs, wm.Rect{X: -5000, Y: -5000, W: 10, H: 10}); i != 0 {
		t.Errorf("window off every screen -> output %d, want 0", i)
	}
}
```

`internal/wm/wm_test.go`: change the four `SpreadColumns(n)` calls to `SpreadColumns("1", n)` and add:

```go
func TestSpreadColumnsNamedView(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.TagSpec("web") // window 2 now only on view "web" (not shown)
	s.AddWindow(3, false, "web")
	s.SpreadColumns("web", 3)
	v := s.View("web")
	if len(v.Columns) != 2 {
		t.Errorf("web view columns = %d, want 2", len(v.Columns))
	}
	if len(s.View("1").Columns) != 1 {
		t.Errorf("view 1 changed")
	}
}
```

(If `TagSpec("web")` doesn't retag the focused window to exactly `web`, build the view with `s.AddWindow(2, false, "web")`, `s.AddWindow(3, false, "web")` instead — the point is a view that isn't active.) Run → FAIL.

- [ ] **Step 2: Implement** `screens.go`:

```go
package macos

import (
	"fmt"

	"wimy/internal/wm"
)

// screenInfo is one NSScreen as the bridge reports it (AppKit
// coordinates). screens[0] is the primary screen (menu bar, origin).
type screenInfo struct {
	Frame, Visible Frame
	Display        uint32 // CGDirectDisplayID, stable while connected
	Name           string // localizedName
}

// outputSpec is a screen as a model output, in model coordinates.
type outputSpec struct {
	Name    string
	Display uint32
	Full    wm.Rect
	Usable  wm.Rect
}

// outputsFor converts screens to outputs. Names are the screens'
// localized names, made unique with " (2)", " (3)"; a nameless screen
// is display-<id>. The usable area is the visible frame (no menu bar,
// no Dock), with its top edge at least barGap points below the
// screen's top: a bar such as SketchyBar covers the menu bar strip,
// so the gap is measured from the screen edge, not added to it.
func outputsFor(screens []screenInfo, barGap int32) []outputSpec {
	if len(screens) == 0 {
		return nil
	}
	primaryH := screens[0].Frame.H
	seen := make(map[string]int)
	outs := make([]outputSpec, 0, len(screens))
	for _, s := range screens {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("display-%d", s.Display)
		}
		seen[name]++
		if n := seen[name]; n > 1 {
			name = fmt.Sprintf("%s (%d)", name, n)
		}
		full := toModel(s.Frame, primaryH)
		usable := toModel(s.Visible, primaryH)
		if top := full.Y + barGap; usable.Y < top {
			usable.H -= top - usable.Y
			usable.Y = top
		}
		outs = append(outs, outputSpec{Name: name, Display: s.Display, Full: full, Usable: usable})
	}
	return outs
}

// outputAt returns the index of the output containing r's centre, or
// 0 when no output does.
func outputAt(outs []outputSpec, r wm.Rect) int {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	for i, o := range outs {
		f := o.Full
		if cx >= f.X && cx < f.X+f.W && cy >= f.Y && cy < f.Y+f.H {
			return i
		}
	}
	return 0
}
```

`wm.go`: rename to `SpreadColumns(view string, maxCols int)` with `v := s.View(view)`; doc: "redistributes the named view's tiled windows…". Update the one caller in `backend_darwin.go` (it becomes a per-output loop in Task 7). Tests → PASS.

- [ ] **Step 3: Commit** ("macOS: map every screen to an output; spread a named view").

---

### Task 4: Hide corner

**Interfaces:** `func hidePosition(screens []wm.Rect, si int, w, h int32) (x, y int32)`; `func inHideCorner(screens []wm.Rect, r wm.Rect) bool`. `screens` are full frames in model coordinates.

- [ ] **Step 1: Tests (RED)** — `internal/macos/hide_test.go`:

```go
package macos

import (
	"testing"

	"wimy/internal/wm"
)

var laptop = wm.Rect{X: 0, Y: 0, W: 1512, H: 982}

func TestHidePositionBottomRight(t *testing.T) {
	x, y := hidePosition([]wm.Rect{laptop}, 0, 800, 600)
	if x != 1511 || y != 981 {
		t.Errorf("hide at %d,%d, want 1511,981 (1pt of the window stays on screen)", x, y)
	}
}

func TestHidePositionAvoidsNeighbor(t *testing.T) {
	right := wm.Rect{X: 1512, Y: -725, W: 2560, H: 1707} // BenQ to the right
	x, y := hidePosition([]wm.Rect{laptop, right}, 0, 800, 600)
	if x != -799 || y != 981 {
		t.Errorf("hide at %d,%d, want bottom-left -799,981 (bottom-right would be on the BenQ)", x, y)
	}
	// the BenQ itself has nothing to its right or below
	x, y = hidePosition([]wm.Rect{laptop, right}, 1, 800, 600)
	if x != 1512+2560-1 || y != -725+1707-1 {
		t.Errorf("BenQ hide at %d,%d", x, y)
	}
}

func TestHidePositionBothCornersTaken(t *testing.T) {
	left := wm.Rect{X: -1920, Y: 0, W: 1920, H: 1080}
	right := wm.Rect{X: 1512, Y: 0, W: 1920, H: 1080}
	x, y := hidePosition([]wm.Rect{laptop, left, right}, 0, 800, 600)
	if x != 1511 || y != 981 {
		t.Errorf("hide at %d,%d, want the bottom-right fallback", x, y)
	}
}

func TestInHideCorner(t *testing.T) {
	screens := []wm.Rect{laptop}
	if !inHideCorner(screens, wm.Rect{X: 1511, Y: 981, W: 800, H: 600}) {
		t.Errorf("window at the bottom-right hide spot not recognized")
	}
	if !inHideCorner(screens, wm.Rect{X: -799, Y: 980, W: 800, H: 600}) {
		t.Errorf("window at the bottom-left hide spot (1pt clamp) not recognized")
	}
	if inHideCorner(screens, wm.Rect{X: 100, Y: 100, W: 800, H: 600}) {
		t.Errorf("ordinary window taken for hidden")
	}
}
```

Run → FAIL.

- [ ] **Step 2: Implement** `hide.go`:

```go
package macos

import "wimy/internal/wm"

// macOS has no API to hide one window of an app, and minimizing
// animates and shows up in the Dock. Like AeroSpace, wimy parks windows
// of views that aren't shown in a screen corner with 1pt left on
// screen (macOS clamps windows that would leave it entirely).

// hidePosition returns the top-left corner at which a w×h window is
// parked on screen si: the bottom-right corner, or the bottom-left one
// when the window would reach into a neighboring screen there. If both
// would, bottom-right.
func hidePosition(screens []wm.Rect, si int, w, h int32) (x, y int32) {
	s := screens[si]
	y = s.Y + s.H - 1
	right := wm.Rect{X: s.X + s.W - 1, Y: y, W: w, H: h}
	if !overlapsOther(screens, si, right) {
		return right.X, y
	}
	left := wm.Rect{X: s.X - w + 1, Y: y, W: w, H: h}
	if !overlapsOther(screens, si, left) {
		return left.X, y
	}
	return right.X, y
}

func overlapsOther(screens []wm.Rect, si int, r wm.Rect) bool {
	for i, s := range screens {
		if i != si && intersects(s, r) {
			return true
		}
	}
	return false
}

func intersects(a, b wm.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// inHideCorner reports whether r sits at a hide spot of some screen
// (within 2pt, since macOS may clamp): a window left there by a wimy
// that died before it could restore it.
func inHideCorner(screens []wm.Rect, r wm.Rect) bool {
	near := func(a, b int32) bool { return a-b <= 2 && b-a <= 2 }
	for _, s := range screens {
		bottom := s.Y + s.H - 1
		if !near(r.Y, bottom) {
			continue
		}
		if near(r.X, s.X+s.W-1) || near(r.X, s.X-r.W+1) {
			return true
		}
	}
	return false
}
```

Tests → PASS.

- [ ] **Step 3: Commit** ("macOS: hide-corner geometry for windows of other views").

---

### Task 5: Hidden-window store

**Interfaces:** `type hiddenStore struct{ path string }`; `func (s hiddenStore) save(m map[wm.WindowID]wm.Rect) error` (atomic; empty map removes the file); `func (s hiddenStore) load() (map[wm.WindowID]wm.Rect, error)` (missing file → empty map, nil); `func stateDir(getenv func(string) string, home string) string`.

- [ ] **Step 1: Tests (RED)** — `internal/macos/store_test.go`:

```go
package macos

import (
	"os"
	"path/filepath"
	"testing"

	"wimy/internal/wm"
)

func TestHiddenStoreRoundTrip(t *testing.T) {
	s := hiddenStore{path: filepath.Join(t.TempDir(), "sub", "hidden.json")}
	if m, err := s.load(); err != nil || len(m) != 0 {
		t.Fatalf("missing file: %v %v", m, err)
	}
	want := map[wm.WindowID]wm.Rect{44: {X: 0, Y: 37, W: 756, H: 945}, 60: {X: 756, Y: 37, W: 756, H: 945}}
	if err := s.save(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.load()
	if err != nil || len(got) != 2 || got[44] != want[44] || got[60] != want[60] {
		t.Fatalf("load = %v, %v", got, err)
	}
	if err := s.save(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Errorf("empty save left the file: %v", err)
	}
}

func TestHiddenStoreCorruptFile(t *testing.T) {
	s := hiddenStore{path: filepath.Join(t.TempDir(), "hidden.json")}
	if err := os.WriteFile(s.path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.load(); err == nil {
		t.Errorf("corrupt store loaded without error")
	}
}

func TestStateDir(t *testing.T) {
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	if got := stateDir(env(map[string]string{"XDG_STATE_HOME": "/s"}), "/Users/me"); got != "/s/wimy" {
		t.Errorf("XDG_STATE_HOME: %q", got)
	}
	if got := stateDir(env(nil), "/Users/me"); got != "/Users/me/.local/state/wimy" {
		t.Errorf("default: %q", got)
	}
}
```

Run → FAIL.

- [ ] **Step 2: Implement** `store.go`:

```go
package macos

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"wimy/internal/wm"
)

// hiddenStore persists the last on-screen frame of every window wimy
// has parked in a hide corner, so a wimy that died (SIGKILL, crash in
// C) can put them back on its next start.
type hiddenStore struct{ path string }

type storedWindow struct {
	ID   wm.WindowID `json:"id"`
	Rect wm.Rect     `json:"rect"`
}

// save writes m atomically; an empty m removes the file.
func (s hiddenStore) save(m map[wm.WindowID]wm.Rect) error {
	if len(m) == 0 {
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	list := make([]storedWindow, 0, len(m))
	for id, r := range m {
		list = append(list, storedWindow{ID: id, Rect: r})
	}
	data, err := json.Marshal(struct {
		Windows []storedWindow `json:"windows"`
	}{list})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// load reads the store; a missing file is an empty store.
func (s hiddenStore) load() (map[wm.WindowID]wm.Rect, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[wm.WindowID]wm.Rect{}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Windows []storedWindow `json:"windows"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	m := make(map[wm.WindowID]wm.Rect, len(doc.Windows))
	for _, w := range doc.Windows {
		m[w.ID] = w.Rect
	}
	return m, nil
}

// stateDir is $XDG_STATE_HOME/wimy, else ~/.local/state/wimy.
func stateDir(getenv func(string) string, home string) string {
	if d := getenv("XDG_STATE_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, "wimy")
	}
	return filepath.Join(home, ".local", "state", "wimy")
}
```

Tests → PASS.

- [ ] **Step 3: Commit** ("macOS: persist hidden windows' frames so a crash can't lose them").

---

### Task 6: Focus-echo suppression

When wimy focuses window A and the user immediately moves on to B, the AX "focused window changed → A" notification arrives after the model already points at B. Taking it at face value yanks focus back to A.

**Interfaces:** `type focusEcho struct{ ... }`; `func (e *focusEcho) sent(id wm.WindowID, now time.Time)`; `func (e *focusEcho) isEcho(id wm.WindowID, now time.Time) bool`.

- [ ] **Step 1: Tests (RED)** — `internal/macos/focus_test.go`:

```go
package macos

import (
	"testing"
	"time"
)

func TestFocusEchoSuppressed(t *testing.T) {
	var e focusEcho
	t0 := time.Unix(1000, 0)
	e.sent(1, t0)
	e.sent(2, t0.Add(50*time.Millisecond))
	if !e.isEcho(1, t0.Add(120*time.Millisecond)) {
		t.Errorf("late notification for window 1, which wimy focused 120ms ago, not treated as echo")
	}
	if !e.isEcho(2, t0.Add(130*time.Millisecond)) {
		t.Errorf("notification for window 2 not treated as echo")
	}
	if e.isEcho(3, t0.Add(130*time.Millisecond)) {
		t.Errorf("a window wimy never focused treated as echo")
	}
}

func TestFocusEchoExpires(t *testing.T) {
	var e focusEcho
	t0 := time.Unix(1000, 0)
	e.sent(1, t0)
	if e.isEcho(1, t0.Add(2*time.Second)) {
		t.Errorf("a click on window 1 two seconds later ignored")
	}
}

func TestFocusEchoConsumed(t *testing.T) {
	var e focusEcho
	t0 := time.Unix(1000, 0)
	e.sent(1, t0)
	if !e.isEcho(1, t0.Add(10*time.Millisecond)) {
		t.Fatal("first echo not recognized")
	}
	if e.isEcho(1, t0.Add(20*time.Millisecond)) {
		t.Errorf("second notification for the same focus still treated as echo (a real click right after is lost)")
	}
}
```

Run → FAIL.

- [ ] **Step 2: Implement** `focus.go`:

```go
package macos

import (
	"time"

	"wimy/internal/wm"
)

// echoWindow is how long after wimy focuses a window a focus
// notification for it is taken as the echo of that request.
const echoWindow = 500 * time.Millisecond

// focusEcho tells focus notifications caused by wimy's own focus
// requests (which can arrive after the model has moved on) from real
// user focus changes. Each request absorbs one notification.
type focusEcho struct {
	pending map[wm.WindowID]time.Time
}

// sent records that wimy asked for id to be focused.
func (e *focusEcho) sent(id wm.WindowID, now time.Time) {
	if e.pending == nil {
		e.pending = make(map[wm.WindowID]time.Time)
	}
	e.pending[id] = now
}

// isEcho reports whether a focus notification for id is the echo of a
// recent request, consuming it.
func (e *focusEcho) isEcho(id wm.WindowID, now time.Time) bool {
	at, ok := e.pending[id]
	if !ok {
		return false
	}
	delete(e.pending, id)
	return now.Sub(at) <= echoWindow
}
```

Tests → PASS.

- [ ] **Step 3: Commit** ("macOS: tell focus echoes from real focus changes").

---

### Task 7: Bridge and backend wiring

No unit-testable surface beyond Tasks 3–6; verified by build and the live run (Task 9).

**Bridge (`bridge.h` / `bridge_darwin.m`):**
- `wimy_screens` fills `wimy_screen` as today (unchanged API).
- `int wimy_window_set_position(uint32_t wid, double x, double y)` — position only (hiding keeps the size).
- Observers per app: add `kAXFocusedWindowChangedNotification` on the app element; per window: `kAXWindowMiniaturizedNotification`, `kAXWindowDeminiaturizedNotification`. Callback: focused-window change → `goFocusChanged(wid)` if tracked; miniaturized → `goWindowGone(wid)`; deminiaturized → `report_window(i, 0)` (same info as `track_window` sends, refactored into `static void report_window(int i, int minimized)`), which calls `goWindowAdded`.
- `NSWorkspaceDidActivateApplicationNotification`: `watch_app(app)` (idempotent re-watch of slow launchers), then the app's `kAXFocusedWindowAttribute` → `goFocusChanged(wid)` if tracked.
- `NSWorkspaceDidHideApplicationNotification` → `goWindowGone(wid)` for each tracked window of the pid; `...DidUnhide...` → `report_window(i, 0)` for each.
- `uint32_t wimy_focused_window(void)`: the frontmost app's focused window's wid (0 if none/untracked), for seeding focus at startup.
- Check `AXObserverAddNotification` results in `track_window`; on failure of the destroyed notification, retry once after 500ms (`dispatch_after`, look the window up by wid).
- Launch retry: `watch_pid(pid, 12)` with the delay doubling from 250ms up to 2s (~20s total).
- Secure-input poll: a 2s `dispatch_source` timer on the main queue calling `goSecureInputTick()`; started by `wimy_start_secure_input_poll()`.
- Wrap the bodies of `wimy_app_init`, `wimy_screens`, `wimy_start_tracking`, `wimy_focused_window` in `@autoreleasepool {}`.

**Backend (`backend_darwin.go`):**
- State: `outputs []outputSpec`; `screens []wm.Rect` (Full of each output); `hidden map[wm.WindowID]wm.Rect` (last on-screen frame of every parked window) + `store hiddenStore`; `lastShown map[wm.WindowID]wm.Rect`; `restore map[wm.WindowID]wm.Rect` (loaded store at startup); `echo focusEcho`; `startup bool`; `minimized map[wm.WindowID]bool` handled by `known` + `goWindowGone`.
- `syncScreens`: `outputsFor(screens, b.Cfg.BarGap)`; diff by `Display` against the previous list: new → `AddOutput`; name changed → `RenameOutput`; gone → `RemoveOutput`; then `SetOutputGeometry`/`SetOutputUsable` for all. `ApplyConfigChange` with `ch.BarGap` → `syncScreens()` + `markDirty()`.
- `windowAdded`: if `restore[id]` exists, set that frame first (a window wimy parked before dying) and delete it; else if the current frame is `inHideCorner`, move it to the centre of the output it belongs to (store lost). During startup (`b.startup`), tag the window with the view of `outputAt(b.outputs, frame)`: `AddWindow(id, floating, view)`.
- `Run`: store path `stateDir(os.Getenv, home)/hidden.json` (home via `os.UserHomeDir`, falling back to `user.Current()`); `restore = store.load()` (log and continue on error); `syncScreens`; `rebind`; `startup = true`; `wimy_start_tracking`; `startup = false`; for each output `SpreadColumns(o.View, spreadColumnCount(o.Usable.W))`; seed focus: `if id := wimy_focused_window(); known → State.FocusWindow(id)`; `b.lastFocus = State.Focused` (don't re-activate what's already in front); start secure-input poll; `markDirty`; `wimy_app_run()`; after it returns: `b.unhideAll()`.
- `apply` per placement:
  - `Hidden`: if not already in `hidden`, record `hidden[id] = lastShown[id]` (fall back to the current frame), save the store **before** moving, move with `wimy_window_set_position` to `hidePosition(b.screens, outputAt(b.outputs, lastShown), w, h)`, `applied.forget(id)`.
  - shown: if in `hidden`, delete it and save the store; then the existing frame logic; record `lastShown[id] = p.Rect` for tiled and floating windows.
- `apply` focus: when calling `wimy_window_focus(f)`, also `b.echo.sent(f, time.Now())`.
- `goFocusChanged(wid)`: ignore unknown windows; ignore if `echo.isEcho`; ignore if already `State.Focused`; else `State.FocusWindow(id)` (selects the window's view if it's hidden — wm does that), `b.lastFocus = id`, `markDirty`.
- `goWindowGone(wid)`: like `goWindowRemoved` but keeps the C tracking (minimized / app hidden); also drop it from `hidden` if present.
- `unhideAll()`: move every window in `hidden` to its recorded frame, then `store.save(nil)`. Called after the run loop returns (Quit, Shutdown/SIGTERM) and from a `guard` wrapper around every exported callback: `defer func() { if r := recover(); r != nil { b.unhideAll(); panic(r) } }()`.
- `goSecureInputTick()` → `checkSecureInput()`.

**cmd/wimy:** `backend_darwin.go` gets `//go:build darwin && cgo`; new `backend_darwin_nocgo.go` (`//go:build darwin && !cgo`) returns `errors.New("the macOS backend needs cgo (build with CGO_ENABLED=1)")`.

- [ ] Implement the above; `go build ./... && go vet ./... && go test -race ./...`, `CGO_ENABLED=0 go build ./...` on darwin (must now pass), Linux cross-build/vet, gofmt.
- [ ] Commit ("macOS: hide other views' windows, sync focus, manage every screen").

---

### Task 8: SketchyBar plugin and the user's setup

**Plugin `contrib/macos/sketchybar/wimy.sh`** (bash, `/usr/bin/jq`, `${WIMYCTL:-wimyctl}`):
- `wimy.sh` with no arguments: kill a previous instance (pid file under `${TMPDIR:-/tmp}`), then loop forever: `"$WIMYCTL" subscribe | jq --unbuffered -c '<summary>'`, where the summary is `{views: [ {name, state} ... ] sorted like wimy-tags.sh (numbers first), mode}` with `state` ∈ `focused|shown|occupied|empty` — reading each line, skipping a line identical to the previous one, and rendering; when `subscribe` exits (wimy not running) sleep 2s and retry.
- Rendering: one SketchyBar item `wimy.view.<name>` per view, created with `--add item … left` and `--move … before wimy.anchor` (an invisible anchor item the plugin adds after `sep_app` if missing), `click_script="$WIMYCTL run view <name>"`; items for views that disappeared are `--remove`d; colors per state (focused: accent background + dark text; shown on another screen: lavender background; occupied: text; empty: dim) — Catppuccin Macchiato defaults, overridable through the `colors.sh` variables if the environment has them (`$BLUE`, `$LAVENDER`, `$TEXT`, `$OVERLAY0`, `$BASE`). One item `wimy.mode` shows the focused view's column mode, click cycles default → stack → max (`wimyctl run mode …`).
- `contrib/macos/sketchybar/README.md`: install (copy the plugin, add the three sketchybarrc lines), `bar-gap` to match the bar height, `WIMYCTL` when `wimyctl` isn't on the agent's PATH.

**User's setup in `~/nix-config`** (edit only; no commit, no rebuild):
- Copy the plugin to `dotfiles/sketchybar/plugins/wimy.sh` (header comment: copied from wimy `contrib/macos/sketchybar/wimy.sh`).
- `dotfiles/sketchybar/sketchybarrc`: after the `front_app` block, a `# --- wimy views ---` section that exports `WIMYCTL` (the dev build `$HOME/workspace/wimy/bin/wimyctl` when it exists, else `wimyctl`), kills a stale instance and starts `"$PLUGIN_DIR/wimy.sh" &` — placed so its items sit between the Apple item and `front_app`.
- `dotfiles/wimy/config.kdl`: `bar-gap 37` with a comment (SketchyBar height in sketchybarrc).
- `modules/home-manager/default.nix`: link `~/.config/wimy` out-of-store like `sketchybar` (`"wimy".source = mkConfigSym "wimy";` next to the sketchybar entry) — takes effect after the user runs `darwin-rebuild switch`; until then, symlink it by hand for testing only if the user agrees.
- The sketchybarrc part applies with `sketchybar --reload` (out-of-store symlink); run it after asking.

- [ ] Write the plugin + README in the repo; `bash -n`; commit ("contrib: SketchyBar plugin for wimy's views and column mode").
- [ ] Make the `~/nix-config` edits; show the user the diff.

---

### Task 9: Live run, docs

Ask the user before starting wimy. Checks (record in `plans/macos-port.md` "Phase 2 results"):
- Both screens are outputs (`wimyctl state`), windows start on the view of the screen they're on, spread per screen; tiling respects the 37pt bar gap.
- Option-2 hides view 1's windows (parked in a corner, not visible, not on the other screen) and shows view 2; Option-1 brings them back at their frames.
- Clicking a window / Cmd-Tab moves wimy's focus (bar and borders follow in later phases); Option-j twice quickly doesn't bounce back.
- Cmd-Tab to an app whose window is on a hidden view switches to that view.
- Minimize a window → it leaves the tiling; restore → it returns. Cmd-H an app → its windows leave; unhide → back.
- `wimyctl quit` with windows on a hidden view → they're back on screen. `kill -9` wimy with windows hidden, start again → restored.
- SketchyBar shows the views, highlights the focused one, clicking switches views, the mode item cycles.

Docs: README macOS section (views/hiding, bar-gap, SketchyBar, the state file, the socket path), AGENTS.md macOS gotchas (hide corner, focus echo, socket dir, launchd env), `plans/macos-port.md` Phase 2 status + deviations (SketchyBar pulled forward; `/tmp/wimy-<uid>` socket instead of `$TMPDIR`). Commit and push.

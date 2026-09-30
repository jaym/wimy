# macOS port, Phase 3: titlebars and borders — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** wimy draws its own titlebar and border for every managed window on macOS, like on Linux, and stack mode shows collapsed windows as clickable titlebar strips.

**Architecture:** One borderless, non-activating "frame panel" per window, ordered directly **behind** the window (`orderWindow:NSWindowBelow relativeTo:<CGWindowID>` — verified on macOS 26 with a probe: the panel lands immediately behind the foreign window). The panel covers the window's content rect plus the border width left/right/bottom and the titlebar height on top; it is filled with the border color and shows the titlebar image (rendered by `internal/titlebar`, premultiplied BGRA — CGImage-ready) in its top strip. The window hides the middle, so what remains visible is a border ring and a titlebar — and Tahoe's rounded window corners show border color instead of wallpaper, so no corner-radius matching is needed. Stack mode: collapsed windows are parked like hidden ones (same crash-safe store) and their panel shows only the titlebar strip, ordered in front. Clicking a panel focuses its window.

**Tech Stack:** Go + cgo, Objective-C (AppKit NSPanel, Core Animation layers, CGImage).

**Spec:** `plans/macos-port.md` "Phase 3 — decorations", concept mapping rows "borders", "titlebars", "stack-mode collapsed strip".

## Global Constraints

- All cgo in `_darwin` files; Linux build/vet and e2e unchanged (river gets only the shared-renderer refactor in Task 1 — verify with CI).
- `internal/wm` unchanged; titlebar space comes from the existing `State.TitlebarHeight` (set from config by `backend.Core`), which macOS stops forcing to 0.
- Every AppKit call on the main thread.
- Titlebars stay unconditional (AGENTS.md); `titlebar "off"` (height 0) still works: borders only, and stack strips are drawn at `stack-strip` height.
- Never lose a window: collapsed stack windows are parked through the existing `hide` path (store before move, restored on exit).
- Commits end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.

## Review Focus

1. Stack mode strips when titlebars are off (`titlebar "off"`): a collapsed window still gets a clickable strip, not nothing. Pinned by `TestDecoCollapsedNoBar`.
2. A window's titlebar frame lines line up with its border (titlebar image as wide as content + both borders). Pinned by `TestDecoNormal`.
3. Border width 0 and titlebar off: no panel at all (nothing to draw). Pinned by `TestDecoNothingToDraw`.
4. Retina (scale 2) vs a scale-1 external screen: images are rendered at the placement's output scale and re-rendered when a window moves between them. Pinned by `TestDecoCacheScale`.
5. AppKit's bottom-left coordinates for panel frames. Pinned by `TestFromModel`.

---

### Task 1: Shared titlebar renderer constructor

**Files:** `internal/config/config.go` (+test), `internal/backend/titlebar.go` (+test), `internal/river/reload.go`, `internal/river/river.go`.

- [ ] **Tests (RED)** — `internal/config/config_test.go`:

```go
func TestColorRGBA(t *testing.T) {
	c, _ := ParseColor("#8aadf480")
	if got := c.RGBA(); got != (color.RGBA{0x8a, 0xad, 0xf4, 0x80}) {
		t.Errorf("RGBA() = %v", got)
	}
}
```

(import `image/color`). `internal/backend/titlebar_test.go`:

```go
package backend

import (
	"testing"

	"wimy/internal/config"
)

func TestNewTitlebarRenderer(t *testing.T) {
	cfg := config.Default()
	r := NewTitlebarRenderer(cfg, 30)
	if r.Height != 30 || r.Border != cfg.Border.Width {
		t.Errorf("height %d border %d", r.Height, r.Border)
	}
	if r.Colors.FocusedBg != cfg.Titlebar.FocusedBg.RGBA() || r.Colors.BorderNormal != cfg.Border.Normal.RGBA() {
		t.Errorf("colors not taken from the config: %+v", r.Colors)
	}
}
```

- [ ] **Implement:** `func (c Color) RGBA() color.RGBA` (top 8 bits of each channel, as river's `toRGBA`). `backend.NewTitlebarRenderer(cfg *config.Config, height int32) *titlebar.Renderer` (colors from `Titlebar` and `Border`, border width `cfg.Border.Width`). river: `newTitlebarRenderer(cfg)` becomes `backend.NewTitlebarRenderer(cfg, cfg.Titlebar.Height)`; `toRGBA` is removed in favor of `Color.RGBA`. Run all tests, Linux cross-build/vet.
- [ ] **Commit** ("Share the titlebar renderer constructor between backends").

### Task 2: macOS fonts for titlebars

**Files:** `internal/titlebar/titlebar.go` (+test).

- [ ] **Test (RED)** — `internal/titlebar/titlebar_test.go`:

```go
func TestSystemFontOnMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS font paths")
	}
	if r := testRenderer(); r.fontData == nil {
		t.Errorf("no system font found: titlebars would use the bitmap fallback")
	}
}
```

- [ ] **Implement:** append to `fontCandidates` (after the Linux ones): `/System/Library/Fonts/SFNS.ttf` (the system UI font; parses with x/image/opentype, verified), `/System/Library/Fonts/Supplemental/Arial.ttf`, `/Library/Fonts/Arial Unicode.ttf`. `.ttc` collections (Helvetica) don't parse with `opentype.Parse`; don't list them.
- [ ] **Commit** ("titlebar: use SF on macOS").

### Task 3: Decoration geometry and render cache (pure)

**Files:** `internal/macos/deco.go` (+test).

**Interfaces:** `type decoLayout struct{ Panel wm.Rect; BarH int32; Fill, Front bool }`; `func decoFor(p wm.Placement, bar, border int32) (decoLayout, bool)`; `func fromModel(r wm.Rect, primaryH float64) Frame`; `type decoKey struct{ Title string; Focused bool; W, H, Scale int32 }`; `type decoCache map[wm.WindowID]decoKey` with `func (c decoCache) stale(id wm.WindowID, k decoKey) bool` (records k).

- [ ] **Tests (RED)** — `internal/macos/deco_test.go`:

```go
package macos

import (
	"testing"

	"wimy/internal/wm"
)

func TestDecoNormal(t *testing.T) {
	// content 800x600 at (100,137) below a 22pt bar, 2pt border
	p := wm.Placement{ID: 1, Rect: wm.Rect{X: 100, Y: 137, W: 800, H: 600}, Bar: true}
	d, ok := decoFor(p, 22, 2)
	if !ok {
		t.Fatal("no decoration")
	}
	want := decoLayout{Panel: wm.Rect{X: 98, Y: 115, W: 804, H: 624}, BarH: 22, Fill: true}
	if d != want {
		t.Errorf("deco = %+v, want %+v (titlebar as wide as content + both borders)", d, want)
	}
}

func TestDecoNoBar(t *testing.T) {
	p := wm.Placement{ID: 1, Rect: wm.Rect{X: 100, Y: 100, W: 800, H: 600}}
	d, _ := decoFor(p, 0, 2)
	want := decoLayout{Panel: wm.Rect{X: 98, Y: 98, W: 804, H: 604}, Fill: true}
	if d != want {
		t.Errorf("border-only deco = %+v, want %+v", d, want)
	}
}

func TestDecoCollapsed(t *testing.T) {
	// stack strip: content rect is the full focus-size box below the strip
	p := wm.Placement{ID: 1, Rect: wm.Rect{X: 0, Y: 59, W: 756, H: 700}, Bar: true, Collapsed: true, Strip: 22}
	d, _ := decoFor(p, 22, 2)
	want := decoLayout{Panel: wm.Rect{X: -2, Y: 37, W: 760, H: 22}, BarH: 22, Front: true}
	if d != want {
		t.Errorf("strip deco = %+v, want %+v", d, want)
	}
}

func TestDecoCollapsedNoBar(t *testing.T) {
	p := wm.Placement{ID: 1, Rect: wm.Rect{X: 0, Y: 37, W: 756, H: 700}, Collapsed: true, Strip: 28}
	d, ok := decoFor(p, 0, 2)
	want := decoLayout{Panel: wm.Rect{X: -2, Y: 37, W: 760, H: 28}, BarH: 28, Front: true}
	if !ok || d != want {
		t.Errorf("strip without titlebars = %+v %v, want %+v (still a clickable strip)", d, ok, want)
	}
}

func TestDecoHidden(t *testing.T) {
	if _, ok := decoFor(wm.Placement{ID: 1, Hidden: true}, 22, 2); ok {
		t.Errorf("hidden window got a decoration")
	}
}

func TestDecoNothingToDraw(t *testing.T) {
	if _, ok := decoFor(wm.Placement{ID: 1, Rect: wm.Rect{W: 10, H: 10}}, 0, 0); ok {
		t.Errorf("no titlebar and no border: decoration anyway")
	}
}

func TestFromModel(t *testing.T) {
	got := fromModel(wm.Rect{X: 98, Y: 115, W: 804, H: 624}, 982)
	if want := (Frame{X: 98, Y: 243, W: 804, H: 624}); got != want {
		t.Errorf("fromModel = %+v, want %+v", got, want)
	}
	if back := toModel(got, 982); back != (wm.Rect{X: 98, Y: 115, W: 804, H: 624}) {
		t.Errorf("round trip = %+v", back)
	}
}

func TestDecoCacheScale(t *testing.T) {
	c := decoCache{}
	k := decoKey{Title: "t", W: 804, H: 22, Scale: 2}
	if !c.stale(1, k) || c.stale(1, k) {
		t.Fatal("first render not stale / identical render stale")
	}
	k.Scale = 1 // moved to the scale-1 screen
	if !c.stale(1, k) {
		t.Errorf("scale change did not re-render")
	}
	k.Focused = true
	if !c.stale(1, k) {
		t.Errorf("focus change did not re-render")
	}
}
```

- [ ] **Implement** `deco.go`:

```go
package macos

import "wimy/internal/wm"

// A window's decoration is one panel directly behind it: the content
// rect plus the border on the left, right and bottom and the titlebar
// on top, filled with the border color, the titlebar image in its top
// strip. The window covers the middle.

// decoLayout is where a window's decoration panel goes.
type decoLayout struct {
	Panel wm.Rect // model coordinates
	BarH  int32   // titlebar strip at the top of the panel, 0 for none
	Fill  bool    // fill with the border color (behind the window)
	Front bool    // order in front (a stack strip: the window is parked)
}

// decoFor returns the decoration of a placement; false for none
// (hidden, or nothing to draw). bar is the titlebar height, border the
// border width. A collapsed stack window shows only a strip — its
// titlebar, or with titlebars off a strip-high bar — since macOS can't
// clip another app's window.
func decoFor(p wm.Placement, bar, border int32) (decoLayout, bool) {
	if p.Hidden {
		return decoLayout{}, false
	}
	r := p.Rect
	if p.Collapsed {
		h := p.Strip
		top := r.Y
		if p.Bar {
			h = bar
			top = r.Y - bar
		}
		if h < 1 {
			return decoLayout{}, false
		}
		return decoLayout{Panel: wm.Rect{X: r.X - border, Y: top, W: r.W + 2*border, H: h}, BarH: h, Front: true}, true
	}
	b := int32(0)
	if p.Bar {
		b = bar
	}
	if b == 0 && border == 0 {
		return decoLayout{}, false
	}
	top := border
	if b > 0 {
		top = b // the titlebar image draws the top border itself
	}
	return decoLayout{
		Panel: wm.Rect{X: r.X - border, Y: r.Y - top, W: r.W + 2*border, H: r.H + top + border},
		BarH:  b,
		Fill:  true,
	}, true
}

// fromModel converts a model rect to AppKit screen coordinates (origin
// bottom-left of the primary screen): the inverse of toModel.
func fromModel(r wm.Rect, primaryH float64) Frame {
	return Frame{X: float64(r.X), Y: primaryH - float64(r.Y) - float64(r.H), W: float64(r.W), H: float64(r.H)}
}

// decoKey is what a rendered titlebar image depends on.
type decoKey struct {
	Title   string
	Focused bool
	W, H    int32 // points
	Scale   int32
}

// decoCache remembers the key of each window's current titlebar image.
type decoCache map[wm.WindowID]decoKey

// stale records k for id and reports whether the image must be
// re-rendered.
func (c decoCache) stale(id wm.WindowID, k decoKey) bool {
	if old, ok := c[id]; ok && old == k {
		return false
	}
	c[id] = k
	return true
}
```

- [ ] **Commit** ("macOS: decoration geometry and titlebar render cache").

### Task 4: Frame panels in the bridge and backend

**Bridge:**
- `wimy_screen` gains `double scale` (`backingScaleFactor`); `screenInfo`/`outputSpec` gain `Scale int32` (rounded, ≥1). Update `outputsFor` to copy it (extend `TestOutputsForMenuBarHidden` with a scale assertion first).
- A `WimyDecoView : NSView` (layer-backed; `acceptsFirstMouse:` YES; `mouseDown:` → `goDecoClicked(wid)`), layer `backgroundColor` = fill color (clear when no fill), sublayer `bar` pinned to the top `barH` points with `contents` = the titlebar CGImage, `contentsScale` = scale, `contentsGravity` = resize.
- Panels: `NSPanel` borderless | non-activating, `backgroundColor` clear, `opaque` NO, `hasShadow` NO, `collectionBehavior` = managed + ignoresCycle + fullScreenNone, `hidesOnDeactivate` NO, `releasedWhenClosed` NO, stored in an `NSMutableDictionary<NSNumber*, NSPanel*>` keyed by wid.
- `void wimy_deco_update(uint32_t wid, wimy_rect frame /*AppKit*/, double barH, uint32_t fillARGB, int fill, int front)`: create if needed, set frame, fill, bar layer frame; order `NSWindowAbove relativeTo:0` when `front`, else `NSWindowBelow relativeTo:wid`; `orderWindow` also shows it. Disable implicit layer animations (`CATransaction setDisableActions:YES`).
- `void wimy_deco_image(uint32_t wid, const void *bgra, int pw, int ph)`: CGImage from a copy of the pixels (`CGDataProviderCreateWithCFData`, BGRA premultiplied = `kCGBitmapByteOrder32Little | kCGImageAlphaPremultipliedFirst`), set as the bar layer's contents.
- `void wimy_deco_hide(uint32_t wid)` (orderOut), `void wimy_deco_destroy(uint32_t wid)` (orderOut, close, remove).

**Backend:**
- Stop forcing `State.TitlebarHeight = 0`.
- `renderers map[int32]*titlebar.Renderer` keyed by height (titlebar height, or stack-strip height when titlebars are off), built with `backend.NewTitlebarRenderer`; cleared on `ApplyConfigChange` with `Border` or `Titlebar`, together with the `decoCache`, so every deco re-renders.
- `apply`: `Hidden` or `Collapsed` → `hide(id)` (parks the window; collapsed stays parked while collapsed). Then for every placement: `decoFor(p, bar, border)`; none → `wimy_deco_hide`; else `wimy_deco_update(fromModel(Panel, primaryH), BarH, color(p.Focused), Fill, Front)` **after** the window frames are set (so "below" has its final position), and if `BarH > 0` and `decoCache.stale` → render `Panel.W`×`BarH` at the output's scale and `wimy_deco_image`.
- `goDecoClicked(wid)`: `State.FocusWindow(id)`, `markDirty` (a strip click expands that window).
- `dropWindow` → `wimy_deco_destroy` + forget cache; `unhideAll` hides every deco.
- [ ] Build/vet/test matrix; commit ("macOS: draw titlebars and borders; stack strips").

### Task 5: Live checks and docs

Ask the user before running. Check: titlebars above every tiled/floating window with the title and focus colors; a border ring; clicking a titlebar focuses; stack mode (Option-s) shows strips, clicking a strip expands it; views hide panels too; retina + BenQ render crisp; `wimyctl run reload` after changing colors re-renders; panels don't show in Cmd-Tab/Mission Control; typing still goes to windows (panels never take key focus). Measure retile time with decorations. Docs: README macOS section, AGENTS.md gotchas (panel behind foreign window; frame-panel design), `plans/macos-port.md` Phase 3 status.

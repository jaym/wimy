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
	want := decoLayout{Panel: wm.Rect{X: 98, Y: 115, W: 804, H: 624}, Content: wm.Rect{X: 2, Y: 22, W: 800, H: 600}, BarH: 22, Fill: true}
	if d != want {
		t.Errorf("deco = %+v, want %+v (titlebar as wide as content + both borders)", d, want)
	}
}

func TestDecoNoBar(t *testing.T) {
	p := wm.Placement{ID: 1, Rect: wm.Rect{X: 100, Y: 100, W: 800, H: 600}}
	d, _ := decoFor(p, 0, 2)
	want := decoLayout{Panel: wm.Rect{X: 98, Y: 98, W: 804, H: 604}, Content: wm.Rect{X: 2, Y: 2, W: 800, H: 600}, Fill: true}
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

func TestFloatOuter(t *testing.T) {
	// a dialog the app put at (300,200) 400x300: its titlebar goes above
	// it, so the window itself doesn't move or shrink
	if got, want := floatOuter(wm.Rect{X: 300, Y: 200, W: 400, H: 300}, 22), (wm.Rect{X: 300, Y: 178, W: 400, H: 322}); got != want {
		t.Errorf("floatOuter = %+v, want %+v", got, want)
	}
	if got := floatOuter(wm.Rect{X: 1, Y: 2, W: 3, H: 4}, 0); got != (wm.Rect{X: 1, Y: 2, W: 3, H: 4}) {
		t.Errorf("no titlebar: %+v", got)
	}
}

func TestStripAnchor(t *testing.T) {
	ps := []wm.Placement{
		{ID: 1, Rect: wm.Rect{X: 0, Y: 59, W: 756, H: 700}, Collapsed: true, Layer: wm.LayerTiled, Output: "a"},
		{ID: 2, Rect: wm.Rect{X: 0, Y: 81, W: 756, H: 700}, Layer: wm.LayerTiled, Output: "a"},    // expanded
		{ID: 3, Rect: wm.Rect{X: 756, Y: 37, W: 756, H: 900}, Layer: wm.LayerTiled, Output: "a"},  // other column
		{ID: 4, Rect: wm.Rect{X: 0, Y: 81, W: 756, H: 700}, Layer: wm.LayerFloating, Output: "a"}, // floating on top
		{ID: 5, Rect: wm.Rect{X: 0, Y: 81, W: 756, H: 700}, Hidden: true, Layer: wm.LayerTiled},   // other view
	}
	if got := stripAnchor(ps, ps[0]); got != 2 {
		t.Errorf("strip anchored to %d, want 2 (its column's expanded window)", got)
	}
}

func TestDecoContentInset(t *testing.T) {
	p := wm.Placement{ID: 1, Rect: wm.Rect{X: 100, Y: 137, W: 800, H: 600}, Bar: true}
	d, _ := decoFor(p, 22, 2)
	// the window's own area inside the panel (panel coordinates, top-left)
	if want := (wm.Rect{X: 2, Y: 22, W: 800, H: 600}); d.Content != want {
		t.Errorf("content = %+v, want %+v", d.Content, want)
	}
}

func TestDecoFloatingNone(t *testing.T) {
	// floating windows (dialogs, Calculator) keep only their native
	// titlebar on macOS
	p := wm.Placement{ID: 1, Rect: wm.Rect{X: 300, Y: 200, W: 400, H: 300}, Bar: true, Layer: wm.LayerFloating}
	if d, ok := decoFor(p, 22, 2); ok {
		t.Errorf("floating window decorated: %+v", d)
	}
}

func TestDecoFullscreenNone(t *testing.T) {
	p := wm.Placement{ID: 1, Rect: wm.Rect{X: 0, Y: 37, W: 1512, H: 945}, Fullscreen: true}
	if d, ok := decoFor(p, 22, 2); ok {
		t.Errorf("fullscreen window decorated: %+v", d)
	}
}

func TestFloatsToRaise(t *testing.T) {
	ps := []wm.Placement{
		{ID: 1, Layer: wm.LayerTiled},
		{ID: 2, Layer: wm.LayerFloating},
		{ID: 3, Layer: wm.LayerFloating, Hidden: true}, // other view
		{ID: 4, Layer: wm.LayerFloating},
	}
	got := floatsToRaise(ps)
	if len(got) != 2 || got[0] != 2 || got[1] != 4 {
		t.Errorf("floatsToRaise = %v, want [2 4] (bottom to top, visible only)", got)
	}
}

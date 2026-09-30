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

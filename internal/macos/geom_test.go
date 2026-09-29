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

func TestSpreadColumnCount(t *testing.T) {
	for w, want := range map[int32]int{1512: 3, 2560: 5, 999: 1, 400: 1, 0: 1} {
		if got := spreadColumnCount(w); got != want {
			t.Errorf("spreadColumnCount(%d) = %d, want %d", w, got, want)
		}
	}
}

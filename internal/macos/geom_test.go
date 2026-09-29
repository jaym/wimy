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
	f := newFrames()
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

func TestFramesMismatchRetriesBounded(t *testing.T) {
	f := newFrames()
	r := wm.Rect{X: 0, Y: 0, W: 100, H: 100}
	f.changed(7, r)
	for i := 0; i < maxFrameRetries; i++ {
		if !f.mismatch(7) {
			t.Fatalf("retry %d refused, want %d retries", i+1, maxFrameRetries)
		}
		if !f.changed(7, r) {
			t.Fatalf("retry %d: same placement not re-applied after a mismatch", i+1)
		}
	}
	if f.mismatch(7) {
		t.Errorf("retry %d allowed: an app that keeps refusing (minimum size) must not cause endless AX calls", maxFrameRetries+1)
	}
	if f.changed(7, r) {
		t.Errorf("same placement re-applied after giving up")
	}
}

func TestFramesNewPlacementResetsRetries(t *testing.T) {
	f := newFrames()
	f.changed(7, wm.Rect{W: 100, H: 100})
	for i := 0; i < maxFrameRetries; i++ {
		f.mismatch(7)
		f.changed(7, wm.Rect{W: 100, H: 100})
	}
	f.changed(7, wm.Rect{W: 200, H: 100}) // layout moved the window
	if !f.mismatch(7) {
		t.Errorf("new placement did not reset the retry budget")
	}
}

func TestFramesMatchResetsRetries(t *testing.T) {
	f := newFrames()
	r := wm.Rect{W: 100, H: 100}
	f.changed(7, r)
	f.mismatch(7)
	f.changed(7, r)
	f.matched(7)
	for i := 0; i < maxFrameRetries; i++ {
		if !f.mismatch(7) {
			t.Fatalf("after a match, retry %d refused", i+1)
		}
		f.changed(7, r)
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

func TestFramesNewPlacementAfterMismatchResetsRetries(t *testing.T) {
	f := newFrames()
	f.changed(7, wm.Rect{W: 100, H: 100})
	f.mismatch(7)                         // one retry used, frame pending re-send
	f.changed(7, wm.Rect{W: 300, H: 100}) // but the layout moved the window meanwhile
	for i := 0; i < maxFrameRetries; i++ {
		if !f.mismatch(7) {
			t.Fatalf("new placement: retry %d refused, want a full budget of %d", i+1, maxFrameRetries)
		}
		f.changed(7, wm.Rect{W: 300, H: 100})
	}
}

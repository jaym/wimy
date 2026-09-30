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

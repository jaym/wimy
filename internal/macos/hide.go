package macos

import "wimy/internal/wm"

// macOS has no API to hide one window of an app, and minimizing
// animates and shows up in the Dock. Like AeroSpace, wimy parks windows
// of views that aren't shown in a screen corner with 1pt left on
// screen (macOS clamps windows that would leave it entirely).

// hidePosition returns the top-left corner at which a w×h window is
// parked: 1pt from the bottom-right or bottom-left corner of screen si,
// or, when both would reach into a neighboring screen (a screen in the
// middle of a row), a free bottom corner of another screen. macOS
// pushes a window parked at the bottom edge up by as much as maxClamp,
// so a spot counts as free only if the window, extended that far up,
// touches no other screen. If no spot is free, bottom-right of si.
func hidePosition(screens []wm.Rect, si int, w, h int32) (x, y int32) {
	order := []int{si}
	for i := range screens {
		if i != si {
			order = append(order, i)
		}
	}
	for _, i := range order {
		s := screens[i]
		y := s.Y + s.H - 1
		for _, x := range []int32{s.X + s.W - 1, s.X - w + 1} {
			reach := wm.Rect{X: x, Y: y - maxClamp, W: w, H: h + maxClamp}
			if !overlapsOther(screens, i, reach) {
				return x, y
			}
		}
	}
	s := screens[si]
	return s.X + s.W - 1, s.Y + s.H - 1
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

// maxClamp is how far up macOS may push a window parked at a screen's
// bottom edge (observed: 39pt on macOS 26, keeping a strip within
// reach).
const maxClamp = 64

// inHideCorner reports whether r sits at a hide spot of some screen: 1pt
// from its left or right edge (within 2pt), and within maxClamp of its
// bottom edge. That is a window left there by a wimy that died before
// it could restore it.
func inHideCorner(screens []wm.Rect, r wm.Rect) bool {
	near := func(a, b int32) bool { return a-b <= 2 && b-a <= 2 }
	for _, s := range screens {
		bottom := s.Y + s.H - 1
		if r.Y > bottom+2 || r.Y < bottom-maxClamp {
			continue
		}
		if near(r.X, s.X+s.W-1) || near(r.X, s.X-r.W+1) {
			return true
		}
	}
	return false
}

// centeredIn returns a w×h rect centred in area, shrunk to fit: where
// a window found stranded in a hide corner (and not in the store) is
// put back.
func centeredIn(area wm.Rect, w, h int32) wm.Rect {
	w, h = min(w, area.W), min(h, area.H)
	return wm.Rect{X: area.X + (area.W-w)/2, Y: area.Y + (area.H-h)/2, W: w, H: h}
}

// onScreen returns r if its centre is on a connected screen, else r's
// size centred on the primary screen's usable area: the frame a parked
// window was recorded with may be on a screen that has since been
// unplugged.
func onScreen(outs []outputSpec, r wm.Rect) wm.Rect {
	if len(outs) == 0 {
		return r
	}
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	for _, o := range outs {
		f := o.Full
		if cx >= f.X && cx < f.X+f.W && cy >= f.Y && cy < f.Y+f.H {
			return r
		}
	}
	return centeredIn(outs[0].Usable, r.W, r.H)
}

// backOnScreen reports whether a window wimy is showing again really
// left its hide corner: only then may its saved frame be forgotten. A
// show that fails (or an app that refuses) must not drop it from the
// store while it still sits in the corner.
func backOnScreen(screens []wm.Rect, got wm.Rect, readable bool) bool {
	return readable && !inHideCorner(screens, got)
}

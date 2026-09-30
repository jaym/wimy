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

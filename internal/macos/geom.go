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

// minSpreadWidth is the narrowest column, in points, that spreading
// existing windows at startup creates.
const minSpreadWidth = 500

// spreadColumnCount is how many columns the windows found at startup
// are spread over on a usable area w points wide.
func spreadColumnCount(w int32) int { return max(1, int(w/minSpreadWidth)) }

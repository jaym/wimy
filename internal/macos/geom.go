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

// maxFrameRetries is how often apply re-sends a frame an app didn't
// take (it moved the window itself after creation, or AX was busy)
// before giving up until the layout moves the window again. Apps with
// a minimum size never take a smaller frame; the bound keeps them from
// causing an AX call storm.
const maxFrameRetries = 3

// frames remembers the last frame requested for each window so apply
// only talks to AX for windows whose placement changed, and budgets
// retries for windows that didn't end up where they were put.
type frames struct {
	applied map[wm.WindowID]wm.Rect
	stale   map[wm.WindowID]bool // applied, but the app didn't take it
	retries map[wm.WindowID]int
}

func newFrames() *frames {
	return &frames{
		applied: make(map[wm.WindowID]wm.Rect),
		stale:   make(map[wm.WindowID]bool),
		retries: make(map[wm.WindowID]int),
	}
}

// changed records r for id and reports whether it must be sent: it
// differs from the last recorded frame, or that one didn't take and
// is being retried. A different placement resets the retry budget.
func (f *frames) changed(id wm.WindowID, r wm.Rect) bool {
	old, ok := f.applied[id]
	if ok && old == r && !f.stale[id] {
		return false
	}
	if !ok || old != r {
		delete(f.retries, id)
	}
	delete(f.stale, id)
	f.applied[id] = r
	return true
}

// mismatch records that id's actual frame differs from the requested
// one. It reports whether to try again; if so the next apply re-sends
// the frame.
func (f *frames) mismatch(id wm.WindowID) bool {
	if f.retries[id] >= maxFrameRetries {
		return false
	}
	f.retries[id]++
	f.stale[id] = true
	return true
}

// matched records that id took its frame, restoring the retry budget.
func (f *frames) matched(id wm.WindowID) { delete(f.retries, id) }

// forget drops id, so its next placement is applied again.
func (f *frames) forget(id wm.WindowID) {
	delete(f.applied, id)
	delete(f.stale, id)
	delete(f.retries, id)
}

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

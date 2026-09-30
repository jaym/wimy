package macos

import (
	"fmt"

	"wimy/internal/wm"
)

// screenInfo is one NSScreen as the bridge reports it (AppKit
// coordinates). screens[0] is the primary screen (menu bar, origin).
type screenInfo struct {
	Frame, Visible Frame
	Display        uint32 // CGDirectDisplayID, stable while connected
	Name           string // localizedName
}

// outputSpec is a screen as a model output, in model coordinates.
type outputSpec struct {
	Name    string
	Display uint32
	Full    wm.Rect
	Usable  wm.Rect
}

// outputsFor converts screens to outputs. Names are the screens'
// localized names, made unique with " (2)", " (3)"; a nameless screen
// is display-<id>. The usable area is the visible frame (no menu bar,
// no Dock), with its top edge at least barGap points below the
// screen's top: a bar such as SketchyBar covers the menu bar strip,
// so the gap is measured from the screen edge, not added to it.
func outputsFor(screens []screenInfo, barGap int32) []outputSpec {
	if len(screens) == 0 {
		return nil
	}
	primaryH := screens[0].Frame.H
	seen := make(map[string]int)
	outs := make([]outputSpec, 0, len(screens))
	for _, s := range screens {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("display-%d", s.Display)
		}
		seen[name]++
		if n := seen[name]; n > 1 {
			name = fmt.Sprintf("%s (%d)", name, n)
		}
		full := toModel(s.Frame, primaryH)
		usable := toModel(s.Visible, primaryH)
		if top := full.Y + barGap; usable.Y < top {
			usable.H -= top - usable.Y
			usable.Y = top
		}
		outs = append(outs, outputSpec{Name: name, Display: s.Display, Full: full, Usable: usable})
	}
	return outs
}

// outputAt returns the index of the output containing r's centre, or
// 0 when no output does.
func outputAt(outs []outputSpec, r wm.Rect) int {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	for i, o := range outs {
		f := o.Full
		if cx >= f.X && cx < f.X+f.W && cy >= f.Y && cy < f.Y+f.H {
			return i
		}
	}
	return 0
}

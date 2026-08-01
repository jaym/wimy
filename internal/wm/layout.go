package wm

// Placement describes where and how a single window should be
// rendered: its geometry in global compositor coordinates, an optional
// content clip box in window-local coordinates, and whether the window
// should be hidden entirely.
type Placement struct {
	ID      WindowID
	Rect    Rect  // global coordinates; W,H are the proposed dimensions
	Clip    *Rect // window-local content clip; nil = no clip
	Hidden  bool
	Layer   Layer
	Focused bool
}

// Layout computes the placements for every window for the current
// state, in bottom-to-top rendering order. A window appears at most
// once: on the first output whose selected view contains it. Windows
// not placed anywhere are returned as hidden placements so the
// backend can hide them.
func (s *State) Layout() []Placement {
	var out []Placement
	placed := make(map[WindowID]bool)
	for _, o := range s.Outputs {
		for _, p := range s.layoutOutput(o, placed) {
			out = append(out, p)
			placed[p.ID] = true
		}
	}
	for id := range s.Windows {
		if !placed[id] {
			out = append(out, Placement{ID: id, Hidden: true})
		}
	}
	return out
}

// layoutOutput computes the placements of windows rendered on the
// given output. Windows already placed on an earlier output (per the
// placed set) are skipped.
func (s *State) layoutOutput(o *Output, placed map[WindowID]bool) []Placement {
	v := s.View(o.View)
	if v == nil || o.Rect.W <= 0 || o.Rect.H <= 0 {
		return nil
	}
	area := o.tilingArea()
	var out []Placement

	// --- tiled columns ---
	widths := columnWidths(v.Columns, area.W)
	x := area.X
	for ci, c := range v.Columns {
		w := widths[ci]
		box := Rect{X: x, Y: area.Y, W: w, H: area.H}
		out = append(out, s.layoutColumn(v, c, box, placed)...)
		x += w
	}

	// --- floating layer, bottom to top, focused last ---
	floats := make([]WindowID, 0, len(v.Float))
	for _, id := range v.Float {
		if !placed[id] && id != v.focusedWindow() {
			floats = append(floats, id)
		}
	}
	if v.FocusLayer == LayerFloating {
		if id := v.focusedWindow(); id != 0 && !placed[id] {
			floats = append(floats, id)
		}
	}
	for _, id := range floats {
		win := s.Windows[id]
		if win == nil {
			continue
		}
		out = append(out, Placement{
			ID:      id,
			Rect:    win.FloatRect,
			Layer:   LayerFloating,
			Focused: id == s.Focused,
		})
	}
	return out
}

// columnWidths distributes the total width among columns according to
// their factors. The last column absorbs rounding remainder.
func columnWidths(cols []*Column, total int32) []int32 {
	widths := make([]int32, len(cols))
	var sumFactor float64
	for _, c := range cols {
		if c.Factor <= 0 {
			c.Factor = 1
		}
		sumFactor += c.Factor
	}
	var acc int32
	for i, c := range cols {
		if i == len(cols)-1 {
			widths[i] = total - acc
		} else {
			widths[i] = int32(float64(total) * c.Factor / sumFactor)
			acc += widths[i]
		}
	}
	return widths
}

// layoutColumn computes placements for one column's windows.
func (s *State) layoutColumn(v *View, c *Column, box Rect, placed map[WindowID]bool) []Placement {
	ids := make([]WindowID, 0, len(c.Windows))
	for _, id := range c.Windows {
		if !placed[id] {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	focused := v.focusedWindow()
	var out []Placement
	put := func(id WindowID, r Rect, clip *Rect, hidden bool) {
		out = append(out, Placement{
			ID:      id,
			Rect:    r,
			Clip:    clip,
			Hidden:  hidden,
			Layer:   LayerTiled,
			Focused: id == s.Focused,
		})
	}

	mode := c.Mode
	if mode == ModeStack && len(ids) == 1 {
		mode = ModeDefault
	}
	if mode == ModeMax && len(ids) == 1 {
		mode = ModeDefault
	}

	switch mode {
	case ModeDefault:
		h := box.H / int32(len(ids))
		y := box.Y
		for i, id := range ids {
			ih := h
			if i == len(ids)-1 {
				ih = box.Y + box.H - y // remainder
			}
			put(id, Rect{X: box.X, Y: y, W: box.W, H: ih}, nil, false)
			y += ih
		}

	case ModeStack:
		fi := 0
		for i, id := range ids {
			if id == focused {
				fi = i
				break
			}
		}
		strip := s.StackStrip
		if strip < 1 {
			strip = 1
		}
		// keep at least half the height for the focused window
		if maxStrip := (box.H / 2) / int32(len(ids)-1); strip > maxStrip {
			strip = maxStrip
			if strip < 1 {
				strip = 1
			}
		}
		focusH := box.H - int32(len(ids)-1)*strip
		// windows above the focused one: strips at the top
		for i := 0; i < fi; i++ {
			r := Rect{X: box.X, Y: box.Y + int32(i)*strip, W: box.W, H: focusH}
			put(ids[i], r, &Rect{X: 0, Y: 0, W: box.W, H: strip}, false)
		}
		focusY := box.Y + int32(fi)*strip
		put(ids[fi], Rect{X: box.X, Y: focusY, W: box.W, H: focusH}, nil, false)
		// windows below: strips at the bottom
		for i := fi + 1; i < len(ids); i++ {
			y := focusY + focusH + int32(i-fi-1)*strip
			r := Rect{X: box.X, Y: y, W: box.W, H: focusH}
			put(ids[i], r, &Rect{X: 0, Y: 0, W: box.W, H: strip}, false)
		}

	case ModeMax:
		for _, id := range ids {
			put(id, box, nil, id != focused)
		}
	}
	return out
}

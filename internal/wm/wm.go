package wm

import (
	"fmt"
	"strconv"
)

// Direction for focus/move operations.
type Direction int

const (
	DirLeft Direction = iota
	DirRight
	DirUp
	DirDown
)

// ParseDirection parses a direction name.
func ParseDirection(s string) (Direction, error) {
	switch s {
	case "left":
		return DirLeft, nil
	case "right":
		return DirRight, nil
	case "up":
		return DirUp, nil
	case "down":
		return DirDown, nil
	}
	return 0, fmt.Errorf("unknown direction %q", s)
}

// --- outputs ---

// AddOutput registers a new output. The output selects the first view
// not shown on any other output, or a fresh numbered view if all
// existing views are taken.
func (s *State) AddOutput(name string) *Output {
	for _, o := range s.Outputs {
		if o.Name == name {
			return o
		}
	}
	out := &Output{Name: name}
	for _, v := range s.Views {
		if s.outputShowing(v.Name) < 0 {
			out.View = v.Name
			break
		}
	}
	if out.View == "" {
		out.View = s.ensureView(s.freeViewName()).Name
	}
	s.Outputs = append(s.Outputs, out)
	s.refocus()
	return out
}

// RemoveOutput unregisters an output. Its selected view is retained;
// the view is destroyed later if it becomes empty.
func (s *State) RemoveOutput(name string) {
	for i, o := range s.Outputs {
		if o.Name == name {
			s.Outputs = append(s.Outputs[:i], s.Outputs[i+1:]...)
			break
		}
	}
	if s.FocusOutput >= len(s.Outputs) {
		s.FocusOutput = len(s.Outputs) - 1
	}
	if s.FocusOutput < 0 {
		s.FocusOutput = 0
	}
	s.gcViews()
	s.refocus()
}

// SetOutputGeometry updates an output's position and dimensions.
func (s *State) SetOutputGeometry(name string, x, y, w, h int32) {
	for _, o := range s.Outputs {
		if o.Name == name {
			o.Rect = Rect{X: x, Y: y, W: w, H: h}
			return
		}
	}
}

// SetOutputUsable updates an output's usable (non-exclusive) area.
func (s *State) SetOutputUsable(name string, x, y, w, h int32) {
	for _, o := range s.Outputs {
		if o.Name == name {
			o.Usable = Rect{X: x, Y: y, W: w, H: h}
			return
		}
	}
}

// RenameOutput renames an output (when its real name is learned).
func (s *State) RenameOutput(old, new string) {
	for _, o := range s.Outputs {
		if o.Name == old {
			o.Name = new
			return
		}
	}
}

// SetAppID sets a window's application ID.
func (s *State) SetAppID(id WindowID, appID string) {
	if w := s.Windows[id]; w != nil {
		w.AppID = appID
	}
}

// SetTitle sets a window's title.
func (s *State) SetTitle(id WindowID, title string) {
	if w := s.Windows[id]; w != nil {
		w.Title = title
	}
}

// --- windows ---

// AddWindow registers a new window. If tags is empty, the window
// inherits the focused window's tag set, falling back to the active
// view. The window is appended to the active view's focused column, or
// to the floating layer if floating is true, and receives focus.
func (s *State) AddWindow(id WindowID, floating bool, tags ...string) *Window {
	if len(tags) == 0 {
		if f := s.Windows[s.Focused]; f != nil && len(f.Tags) > 0 {
			tags = f.TagList()
		} else if v := s.activeView(); v != nil {
			tags = []string{v.Name}
		} else {
			tags = []string{s.ensureView(s.freeViewName()).Name}
		}
	}
	w := &Window{
		ID:        id,
		Tags:      make(map[string]bool),
		FloatRect: defaultFloatRect(s.activeOutput()),
	}
	for _, t := range tags {
		w.Tags[t] = true
	}
	s.Windows[id] = w
	for t := range w.Tags {
		s.addToView(s.ensureView(t), id, floating)
	}
	// only steal focus if the window appears on the active view
	if v := s.activeView(); v != nil && v.contains(id) {
		v.focusWindow(id)
		s.Focused = id
	}
	return w
}

// RemoveWindow unregisters a window, removing it from all views.
// Empty unselected views are destroyed.
func (s *State) RemoveWindow(id WindowID) {
	if _, ok := s.Windows[id]; !ok {
		return
	}
	delete(s.Windows, id)
	for _, v := range s.Views {
		v.removeFromView(id)
	}
	s.gcViews()
	s.refocus()
}

// --- focus ---

// FocusDir moves the focus within the active view.
func (s *State) FocusDir(d Direction) {
	v := s.activeView()
	if v == nil {
		return
	}
	if v.FocusLayer == LayerFloating {
		switch d {
		case DirUp:
			if v.FocusFloat > 0 {
				v.FocusFloat--
			}
		case DirDown:
			if v.FocusFloat < len(v.Float)-1 {
				v.FocusFloat++
			}
		}
		s.refocus()
		return
	}
	switch d {
	case DirLeft:
		if v.FocusCol > 0 {
			v.FocusCol--
		}
	case DirRight:
		if v.FocusCol < len(v.Columns)-1 {
			v.FocusCol++
		}
	case DirUp:
		if v.FocusRow > 0 {
			v.FocusRow--
		}
	case DirDown:
		if c := v.Columns[v.FocusCol]; v.FocusRow < len(c.Windows)-1 {
			v.FocusRow++
		}
	}
	v.clampFocus()
	s.refocus()
}

// FocusToggleLayer switches focus between the tiled and floating layers.
func (s *State) FocusToggleLayer() {
	v := s.activeView()
	if v == nil {
		return
	}
	if v.FocusLayer == LayerTiled && len(v.Float) > 0 {
		v.FocusLayer = LayerFloating
	} else if v.FocusLayer == LayerFloating {
		v.FocusLayer = LayerTiled
	}
	s.refocus()
}

// CycleOutput switches the active output by delta (wrapping) and
// focuses its view's focused window.
func (s *State) CycleOutput(delta int) {
	if len(s.Outputs) == 0 {
		return
	}
	n := len(s.Outputs)
	s.FocusOutput = ((s.FocusOutput+delta)%n + n) % n
	s.refocus()
}

// FocusWindow focuses the given window: if it is visible on some
// output's selected view, that output becomes active and the view
// focuses the window. If it is not visible anywhere, its first tag's
// view is selected on the active output.
func (s *State) FocusWindow(id WindowID) {
	w := s.Windows[id]
	if w == nil {
		return
	}
	for t := range w.Tags {
		if oi := s.outputShowing(t); oi >= 0 {
			s.FocusOutput = oi
			if v := s.View(t); v != nil {
				v.focusWindow(id)
			}
			s.refocus()
			return
		}
	}
	// hidden window: select a view showing it
	for _, t := range w.TagList() {
		s.SelectView(t)
		break
	}
	if v := s.activeView(); v != nil && v.contains(id) {
		v.focusWindow(id)
	}
	s.refocus()
}

// --- moving windows ---

// MoveDir moves the focused window. Left/right move it to the adjacent
// column, creating one at the edge if necessary; up/down swap it with
// its neighbor within the column.
func (s *State) MoveDir(d Direction) {
	v := s.activeView()
	if v == nil || s.Focused == 0 || v.FocusLayer == LayerFloating {
		return
	}
	id := s.Focused
	col, row := v.FocusCol, v.FocusRow
	if col < 0 || col >= len(v.Columns) {
		return
	}
	c := v.Columns[col]
	if row < 0 || row >= len(c.Windows) || c.Windows[row] != id {
		return
	}
	switch d {
	case DirUp:
		if row > 0 {
			c.Windows[row], c.Windows[row-1] = c.Windows[row-1], c.Windows[row]
			v.FocusRow--
		}
	case DirDown:
		if row < len(c.Windows)-1 {
			c.Windows[row], c.Windows[row+1] = c.Windows[row+1], c.Windows[row]
			v.FocusRow++
		}
	case DirLeft, DirRight:
		delta := -1
		if d == DirRight {
			delta = 1
		}
		// remove from current column
		c.Windows = append(c.Windows[:row], c.Windows[row+1:]...)
		removed := false
		if len(c.Windows) == 0 {
			v.Columns = append(v.Columns[:col], v.Columns[col+1:]...)
			removed = true
		}
		target := col + delta
		if removed && delta > 0 {
			// columns right of the removed one shifted down
			target--
		}
		if target < 0 || target >= len(v.Columns) {
			// create a new column at the edge
			nc := &Column{Mode: ModeDefault, Factor: 1, Windows: []WindowID{id}}
			if d == DirLeft {
				v.Columns = append([]*Column{nc}, v.Columns...)
				target = 0
			} else {
				v.Columns = append(v.Columns, nc)
				target = len(v.Columns) - 1
			}
		} else {
			tc := v.Columns[target]
			tc.Windows = append(tc.Windows, id)
		}
		v.FocusCol = target
		v.FocusRow = len(v.Columns[target].Windows) - 1
	}
	v.clampFocus()
	s.refocus()
}

// ToggleFloat moves the focused window between the tiled and floating
// layers of the active view.
func (s *State) ToggleFloat() {
	v := s.activeView()
	if v == nil || s.Focused == 0 {
		return
	}
	id := s.Focused
	if v.FocusLayer == LayerFloating {
		// float -> tiled: append to (previously) focused column
		for fi, w := range v.Float {
			if w == id {
				v.Float = append(v.Float[:fi], v.Float[fi+1:]...)
				break
			}
		}
		v.FocusLayer = LayerTiled
		v.clampFocus()
		c := v.Columns[v.FocusCol]
		c.Windows = append(c.Windows, id)
		v.FocusRow = len(c.Windows) - 1
	} else {
		// tiled -> float
		v.removeFromView(id)
		v.Float = append(v.Float, id)
		v.FocusLayer = LayerFloating
		v.FocusFloat = len(v.Float) - 1
		w := s.Windows[id]
		if w != nil && (w.FloatRect.W == 0 || w.FloatRect.H == 0) {
			w.FloatRect = defaultFloatRect(s.activeOutput())
		}
	}
	v.clampFocus()
	s.refocus()
}

// SetMode sets the display mode of the focused column.
func (s *State) SetMode(m Mode) {
	v := s.activeView()
	if v == nil || v.FocusLayer != LayerTiled {
		return
	}
	if v.FocusCol >= 0 && v.FocusCol < len(v.Columns) {
		v.Columns[v.FocusCol].Mode = m
	}
}

// Grow adjusts the width factors around the focused column: the
// boundary in the given direction moves by amt percent of the usable
// width (negative amt reverses).
func (s *State) Grow(d Direction, pct float64) {
	v := s.activeView()
	if v == nil || v.FocusLayer != LayerTiled || len(v.Columns) < 2 {
		return
	}
	ci := v.FocusCol
	var ni int
	switch d {
	case DirLeft:
		ni = ci - 1
	case DirRight:
		ni = ci + 1
	default:
		return
	}
	if ni < 0 || ni >= len(v.Columns) {
		return
	}
	ResizeColumns(v.Columns[ci], v.Columns[ni], pct/100)
}

// ResizeColumns shifts width factor from column n to column c by
// shift (negative reverses). Factors never drop below 0.1.
func ResizeColumns(c, n *Column, shift float64) {
	const minFactor = 0.1
	if c.Factor+shift < minFactor {
		shift = minFactor - c.Factor
	}
	if n.Factor-shift < minFactor {
		shift = n.Factor - minFactor
	}
	c.Factor += shift
	n.Factor -= shift
}

// --- mouse geometry ---

// ColumnBoundaries returns the x positions of the boundaries between
// the view's columns in global coordinates: the left edge of the
// tiling area, each inter-column boundary, and the right edge.
func (s *State) ColumnBoundaries(v *View, area Rect) []int32 {
	widths := columnWidths(v.Columns, area.W)
	out := make([]int32, 0, len(widths)+1)
	x := area.X
	out = append(out, x)
	for _, w := range widths {
		x += w
		out = append(out, x)
	}
	return out
}

// NearestColumnBoundary returns the index into ColumnBoundaries of
// the boundary closest to x.
func NearestColumnBoundary(boundaries []int32, x int32) int {
	best, bestDist := 0, int32(1<<30)
	for i, b := range boundaries {
		d := b - x
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// ResizeColumnBoundary shifts the boundary at index i (1..len-2 are
// inter-column boundaries; 0 and len-1 are the outer edges and
// ignored) by dx pixels relative to the tiling area width.
func (s *State) ResizeColumnBoundary(v *View, i int, dx int32, areaW int32) {
	if v == nil || i <= 0 || i >= len(v.Columns) || areaW <= 0 {
		return
	}
	var sum float64
	for _, c := range v.Columns {
		sum += c.Factor
	}
	if sum <= 0 {
		return
	}
	ResizeColumns(v.Columns[i-1], v.Columns[i], float64(dx)/float64(areaW)*sum)
}

// FloatRectOf returns the floating geometry of a window.
func (s *State) FloatRectOf(id WindowID) Rect {
	if w := s.Windows[id]; w != nil {
		return w.FloatRect
	}
	return Rect{}
}

// SetFloatRect sets the floating geometry of a window (clamped to a
// minimum size).
func (s *State) SetFloatRect(id WindowID, r Rect) {
	if w := s.Windows[id]; w != nil {
		if r.W < 50 {
			r.W = 50
		}
		if r.H < 50 {
			r.H = 50
		}
		w.FloatRect = r
	}
}

// OutputArea returns the tiling area of the output showing the given
// view (for boundary math during pointer ops).
func (s *State) OutputArea(view string) Rect {
	for _, o := range s.Outputs {
		if o.View == view {
			return o.tilingArea()
		}
	}
	return Rect{}
}

// --- views and tags ---

// SelectView selects the named view on the active output, creating the
// view if necessary. If the view is already shown on another output,
// the two outputs swap views.
func (s *State) SelectView(name string) {
	out := s.activeOutput()
	if out == nil {
		return
	}
	if out.View == name {
		s.refocus()
		return
	}
	v := s.ensureView(name)
	if oi := s.outputShowing(name); oi >= 0 {
		// swap the two outputs' views; focus stays with the user
		s.Outputs[oi].View, out.View = out.View, name
	} else {
		out.View = v.Name
	}
	// leaving an empty view destroys it (unless selected elsewhere)
	s.gcViews()
	s.refocus()
}

// CycleView selects the next (delta=1) or previous (delta=-1) view in
// creation order on the active output, skipping views shown on other
// outputs.
func (s *State) CycleView(delta int) {
	out := s.activeOutput()
	if out == nil || len(s.Views) == 0 {
		return
	}
	cur := s.viewIndex(out.View)
	n := len(s.Views)
	for i := 1; i <= n; i++ {
		idx := ((cur+delta*i)%n + n) % n
		cand := s.Views[idx]
		if oi := s.outputShowing(cand.Name); oi >= 0 && oi != s.FocusOutput {
			continue
		}
		s.SelectView(cand.Name)
		return
	}
}

// viewByNumber resolves a 1-based view number to the view named after
// the number, creating it on demand. n==0 means 10. Resolution is by
// name, not position: gcViews compacts the Views slice as empty views
// come and go, so positions do not correspond to numbers.
func (s *State) viewByNumber(n int) *View {
	if n == 0 {
		n = 10
	}
	if n < 0 {
		return nil
	}
	return s.ensureView(strconv.Itoa(n))
}

// SelectViewN selects the nth view (see viewByNumber).
func (s *State) SelectViewN(n int) {
	if v := s.viewByNumber(n); v != nil {
		s.SelectView(v.Name)
	}
}

// MoveToView replaces the focused window's tag set with the single
// given tag. The window disappears from the active view unless the
// target is the active view.
func (s *State) MoveToView(name string) {
	w := s.Windows[s.Focused]
	if w == nil {
		return
	}
	id := w.ID
	w.Tags = map[string]bool{name: true}
	s.syncWindowViews(id, s.vFocus(id))
	s.gcViews()
	s.refocus()
}

// vFocus returns a preference for the layer when re-inserting a window
// into a view: true if the window floated in the active view.
func (s *State) vFocusFloating(id WindowID) bool {
	if v := s.activeView(); v != nil {
		for _, f := range v.Float {
			if f == id {
				return true
			}
		}
	}
	return false
}

// vFocus is a helper capturing floating membership before sync.
func (s *State) vFocus(id WindowID) bool { return s.vFocusFloating(id) }

// syncWindowViews makes view membership match the window's tag set:
// the window is removed from views it is no longer tagged with and
// added to views it is newly tagged with, creating views as needed.
func (s *State) syncWindowViews(id WindowID, floating bool) {
	w := s.Windows[id]
	if w == nil {
		return
	}
	for t := range w.Tags {
		s.ensureView(t)
	}
	for _, v := range s.Views {
		in := v.contains(id)
		want := w.Tags[v.Name]
		switch {
		case in && !want:
			v.removeFromView(id)
		case !in && want:
			s.addToView(v, id, floating)
		}
	}
}

// MoveToViewN moves the focused window to the nth view.
func (s *State) MoveToViewN(n int) {
	if v := s.viewByNumber(n); v != nil {
		s.MoveToView(v.Name)
	}
}

// TagSpec applies tag specifications to the focused window: "+x" adds
// tag x, "-x" removes it, and a bare "x" resets the set to just x
// before applying further specs. The tag set is never emptied: if the
// result would be empty, the active view's tag is kept.
func (s *State) TagSpec(specs ...string) {
	w := s.Windows[s.Focused]
	if w == nil || len(specs) == 0 {
		return
	}
	floating := s.vFocusFloating(w.ID)
	tags := make(map[string]bool, len(w.Tags))
	for t := range w.Tags {
		tags[t] = true
	}
	for _, sp := range specs {
		if sp == "" {
			continue
		}
		switch sp[0] {
		case '+':
			if len(sp) > 1 {
				tags[sp[1:]] = true
			}
		case '-':
			if len(sp) > 1 {
				delete(tags, sp[1:])
			}
		default:
			tags = map[string]bool{sp: true}
		}
	}
	if len(tags) == 0 {
		if v := s.activeView(); v != nil {
			tags[v.Name] = true
		} else {
			return
		}
	}
	w.Tags = tags
	s.syncWindowViews(w.ID, floating)
	s.gcViews()
	s.refocus()
}

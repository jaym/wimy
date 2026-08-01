// Package wm implements the pure window-management model of wimy:
// tag-based views, columns with default/stack/max modes, a floating
// layer per view, focus tracking, and a layout solver.
//
// The package contains no Wayland code; the river backend translates
// between protocol events and State operations and applies the results
// of the layout solver.
package wm

import (
	"fmt"
	"strconv"
)

// Mode is the arrangement mode of a column.
type Mode int

const (
	// ModeDefault splits the column height equally between windows.
	ModeDefault Mode = iota
	// ModeStack gives the focused window the remaining height while
	// unfocused windows are clipped to strips above and below it.
	ModeStack
	// ModeMax gives every window the full column height; only the
	// focused window is visible.
	ModeMax
)

func (m Mode) String() string {
	switch m {
	case ModeDefault:
		return "default"
	case ModeStack:
		return "stack"
	case ModeMax:
		return "max"
	}
	return "unknown"
}

// ParseMode parses a mode name.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "default":
		return ModeDefault, nil
	case "stack":
		return ModeStack, nil
	case "max":
		return ModeMax, nil
	}
	return 0, fmt.Errorf("unknown mode %q", s)
}

// Layer is either the tiled (managed) layer or the floating layer.
type Layer int

const (
	LayerTiled Layer = iota
	LayerFloating
)

func (l Layer) String() string {
	if l == LayerFloating {
		return "floating"
	}
	return "tiled"
}

// Rect is an axis-aligned rectangle in the compositor's logical
// coordinate space.
type Rect struct{ X, Y, W, H int32 }

// WindowID identifies a window within the model.
type WindowID uint32

// Window is a single client window. A window carries a set of tags and
// appears on every view whose name is in the set.
type Window struct {
	ID    WindowID
	Tags  map[string]bool
	AppID string
	Title string
	// FloatRect is the geometry used while the window is floating, in
	// global compositor coordinates.
	FloatRect Rect
}

// HasTag reports whether the window is tagged with name.
func (w *Window) HasTag(name string) bool { return w.Tags[name] }

// TagList returns the window's tags in a stable (sorted) order.
func (w *Window) TagList() []string {
	var out []string
	for t := range w.Tags {
		out = append(out, t)
	}
	// simple insertion sort, tag sets are tiny
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Column is an ordered list of tiled windows with a display mode and a
// relative width factor.
type Column struct {
	Windows []WindowID // top to bottom
	Mode    Mode
	Factor  float64 // 1.0 == equal share of the usable width
}

// View is a named tag view: every window whose tag set contains the
// view's name is arranged here in columns or in the floating layer.
type View struct {
	Name    string
	Columns []*Column
	Float   []WindowID // bottom to top

	FocusCol   int
	FocusRow   int
	FocusFloat int
	FocusLayer Layer
}

// FocusedWindow returns the window the view's focus markers point
// at, or 0 if the view is empty.
func (v *View) FocusedWindow() WindowID { return v.focusedWindow() }

// focusedWindow returns the window the view's focus markers point at,
// or 0 if the view is empty.
func (v *View) focusedWindow() WindowID {
	if v.FocusLayer == LayerFloating {
		if v.FocusFloat >= 0 && v.FocusFloat < len(v.Float) {
			return v.Float[v.FocusFloat]
		}
		return 0
	}
	if v.FocusCol >= 0 && v.FocusCol < len(v.Columns) {
		c := v.Columns[v.FocusCol]
		if v.FocusRow >= 0 && v.FocusRow < len(c.Windows) {
			return c.Windows[v.FocusRow]
		}
	}
	return 0
}

// empty reports whether no window is arranged on the view.
func (v *View) empty() bool {
	for _, c := range v.Columns {
		if len(c.Windows) > 0 {
			return false
		}
	}
	return len(v.Float) == 0
}

// Output is a logical output with its position in the compositor's
// logical coordinate space and the name of its selected view.
type Output struct {
	Name string
	Rect Rect // X,Y = global position; W,H = dimensions
	// Usable is the area available for tiling after subtracting layer
	// shell exclusive zones. Zero means "not set": Rect is used.
	Usable Rect
	View   string
}

// tilingArea returns the rectangle the layout solver should use.
func (o *Output) tilingArea() Rect {
	if o.Usable.W > 0 && o.Usable.H > 0 {
		return o.Usable
	}
	return o.Rect
}

// State is the complete window-management state.
type State struct {
	Views   []*View   // creation order
	Outputs []*Output // compositor order
	Windows map[WindowID]*Window

	// Focused is the globally focused window (0 = none). Each view
	// keeps its own focus position which is restored on selection.
	Focused WindowID
	// FocusOutput is the index of the output commands operate on.
	FocusOutput int

	// StackStrip is the height in pixels of unfocused window strips
	// in stack mode when titlebars are disabled.
	StackStrip int32
	// TitlebarHeight is the height in pixels of the titlebar drawn
	// above each window; 0 disables titlebars (dwm-style).
	TitlebarHeight int32
}

// NewState returns an empty state with sane defaults.
func NewState() *State {
	return &State{
		Windows:    make(map[WindowID]*Window),
		StackStrip: 28,
	}
}

// --- helpers ---

func (s *State) viewIndex(name string) int {
	for i, v := range s.Views {
		if v.Name == name {
			return i
		}
	}
	return -1
}

// View returns the view with the given name, or nil.
func (s *State) View(name string) *View {
	if i := s.viewIndex(name); i >= 0 {
		return s.Views[i]
	}
	return nil
}

// ensureView returns the named view, creating it if necessary.
func (s *State) ensureView(name string) *View {
	if v := s.View(name); v != nil {
		return v
	}
	v := &View{
		Name:    name,
		Columns: []*Column{{Mode: ModeDefault, Factor: 1}},
	}
	s.Views = append(s.Views, v)
	return v
}

// activeOutput returns the output commands operate on, or nil if there
// are no outputs.
func (s *State) activeOutput() *Output {
	if len(s.Outputs) == 0 {
		return nil
	}
	if s.FocusOutput < 0 || s.FocusOutput >= len(s.Outputs) {
		s.FocusOutput = 0
	}
	return s.Outputs[s.FocusOutput]
}

// activeView returns the view selected on the active output, or nil.
func (s *State) activeView() *View {
	out := s.activeOutput()
	if out == nil {
		return nil
	}
	return s.View(out.View)
}

// outputShowing returns the index of the first output showing the
// named view, or -1.
func (s *State) outputShowing(view string) int {
	for i, o := range s.Outputs {
		if o.View == view {
			return i
		}
	}
	return -1
}

// smallestFreeViewName returns the smallest positive integer not used
// by an existing view, as a string.
func (s *State) freeViewName() string {
	for n := 1; ; n++ {
		if s.View(strconv.Itoa(n)) == nil {
			return strconv.Itoa(n)
		}
	}
}

// contains reports whether the window is arranged on the view.
func (v *View) contains(id WindowID) bool {
	for _, c := range v.Columns {
		for _, w := range c.Windows {
			if w == id {
				return true
			}
		}
	}
	for _, w := range v.Float {
		if w == id {
			return true
		}
	}
	return false
}

// addToView inserts the window into the view: appended to the focused
// column, or to the floating layer if floating is true.
func (s *State) addToView(v *View, id WindowID, floating bool) {
	if v.contains(id) {
		return
	}
	if floating {
		v.Float = append(v.Float, id)
		return
	}
	if len(v.Columns) == 0 {
		v.Columns = []*Column{{Mode: ModeDefault, Factor: 1}}
		v.FocusCol = 0
	}
	if v.FocusCol < 0 || v.FocusCol >= len(v.Columns) {
		v.FocusCol = len(v.Columns) - 1
	}
	c := v.Columns[v.FocusCol]
	c.Windows = append(c.Windows, id)
}

// removeFromView removes the window from the view, dropping empty
// columns and clamping focus markers. It reports whether the window
// was the view's focused window.
func (v *View) removeFromView(id WindowID) (wasFocused bool) {
	wasFocused = v.focusedWindow() == id
	for ci, c := range v.Columns {
		for ri, w := range c.Windows {
			if w != id {
				continue
			}
			c.Windows = append(c.Windows[:ri], c.Windows[ri+1:]...)
			if len(c.Windows) == 0 {
				v.Columns = append(v.Columns[:ci], v.Columns[ci+1:]...)
			}
			v.clampFocus()
			return wasFocused
		}
	}
	for fi, w := range v.Float {
		if w == id {
			v.Float = append(v.Float[:fi], v.Float[fi+1:]...)
			v.clampFocus()
			return wasFocused
		}
	}
	return false
}

// clampFocus keeps all focus markers within bounds and makes sure
// there is at least one column while the view has no floating focus.
func (v *View) clampFocus() {
	if len(v.Columns) == 0 {
		v.Columns = []*Column{{Mode: ModeDefault, Factor: 1}}
		v.FocusCol = 0
		v.FocusRow = 0
		if v.FocusLayer == LayerTiled {
			// column is empty; focus moves to float if any
			if len(v.Float) > 0 {
				v.FocusLayer = LayerFloating
			}
		}
	}
	if v.FocusCol >= len(v.Columns) {
		v.FocusCol = len(v.Columns) - 1
	}
	if v.FocusCol < 0 {
		v.FocusCol = 0
	}
	if c := v.Columns[v.FocusCol]; v.FocusRow >= len(c.Windows) {
		v.FocusRow = len(c.Windows) - 1
	}
	if v.FocusRow < 0 {
		v.FocusRow = 0
	}
	if v.FocusFloat >= len(v.Float) {
		v.FocusFloat = len(v.Float) - 1
	}
	if v.FocusFloat < 0 {
		v.FocusFloat = 0
	}
	if v.FocusLayer == LayerFloating && len(v.Float) == 0 {
		v.FocusLayer = LayerTiled
	}
}

// gcViews destroys views that are empty and not selected on any output.
func (s *State) gcViews() {
	selected := make(map[string]bool)
	for _, o := range s.Outputs {
		selected[o.View] = true
	}
	kept := s.Views[:0]
	for _, v := range s.Views {
		if v.empty() && !selected[v.Name] {
			continue
		}
		kept = append(kept, v)
	}
	s.Views = kept
}

// refocus sets the global focus from the active view's focus markers.
func (s *State) refocus() {
	if v := s.activeView(); v != nil {
		v.clampFocus()
		s.Focused = v.focusedWindow()
	} else {
		s.Focused = 0
	}
}

// focusWindowInView moves the view's focus markers to the given window.
func (v *View) focusWindow(id WindowID) {
	for ci, c := range v.Columns {
		for ri, w := range c.Windows {
			if w == id {
				v.FocusCol, v.FocusRow = ci, ri
				v.FocusLayer = LayerTiled
				return
			}
		}
	}
	for fi, w := range v.Float {
		if w == id {
			v.FocusFloat = fi
			v.FocusLayer = LayerFloating
			return
		}
	}
}

// defaultFloatRect returns a centered rectangle covering half the
// output's usable area in each dimension.
func defaultFloatRect(out *Output) Rect {
	if out == nil || out.Rect.W == 0 {
		return Rect{W: 640, H: 480}
	}
	a := out.tilingArea()
	w, h := a.W/2, a.H/2
	return Rect{X: a.X + (a.W-w)/2, Y: a.Y + (a.H-h)/2, W: w, H: h}
}

// fmtString is a small helper for debugging.
func (s *State) fmtString() string {
	return fmt.Sprintf("%d views, %d outputs, %d windows, focused=%d",
		len(s.Views), len(s.Outputs), len(s.Windows), s.Focused)
}

package backend

import "wimy/internal/wm"

// Edges is a set of window edges for resize operations.
type Edges uint32

// Window edges. Backends convert to and from their protocol's values
// explicitly.
const (
	EdgeTop Edges = 1 << iota
	EdgeBottom
	EdgeLeft
	EdgeRight
)

// Button identifies the Mod+button pointer bindings.
type Button int

const (
	// ButtonMove (left) moves a floating window.
	ButtonMove Button = iota
	// ButtonResize (right) resizes a floating window, or drags the
	// nearest column boundary of a tiled one.
	ButtonResize
)

// minFloatSize is the smallest floating width/height a resize drag
// produces.
const minFloatSize = 50

// PointerOp is an interactive pointer operation. Apply receives the
// pointer delta accumulated since the op started.
type PointerOp interface {
	Apply(dx, dy int32)
}

// MoveOp moves a floating window.
type MoveOp struct {
	Win   wm.WindowID
	state *wm.State
	start wm.Rect
}

// Apply implements PointerOp.
func (o *MoveOp) Apply(dx, dy int32) {
	o.state.SetFloatRect(o.Win, wm.Rect{
		X: o.start.X + dx, Y: o.start.Y + dy, W: o.start.W, H: o.start.H,
	})
}

// ResizeOp resizes a floating window by the given edges.
type ResizeOp struct {
	Win   wm.WindowID
	Edges Edges
	state *wm.State
	start wm.Rect
}

// Apply implements PointerOp.
func (o *ResizeOp) Apply(dx, dy int32) {
	r := o.start
	if o.Edges&EdgeLeft != 0 {
		r.X = o.start.X + dx
		r.W = o.start.W - dx
	}
	if o.Edges&EdgeRight != 0 {
		r.W = o.start.W + dx
	}
	if o.Edges&EdgeTop != 0 {
		r.Y = o.start.Y + dy
		r.H = o.start.H - dy
	}
	if o.Edges&EdgeBottom != 0 {
		r.H = o.start.H + dy
	}
	// keep the window from inverting past its minimum size
	if r.W < minFloatSize {
		if o.Edges&EdgeLeft != 0 {
			r.X -= minFloatSize - r.W
		}
		r.W = minFloatSize
	}
	if r.H < minFloatSize {
		if o.Edges&EdgeTop != 0 {
			r.Y -= minFloatSize - r.H
		}
		r.H = minFloatSize
	}
	o.state.SetFloatRect(o.Win, r)
}

// ColumnResizeOp drags a tiled column boundary.
type ColumnResizeOp struct {
	state    *wm.State
	view     *wm.View
	boundary int
	area     wm.Rect
	factors  []float64 // factors at op start (deltas are cumulative)
}

// Apply implements PointerOp.
func (o *ColumnResizeOp) Apply(dx, dy int32) {
	for i, f := range o.factors {
		o.view.Columns[i].Factor = f
	}
	o.state.ResizeColumnBoundary(o.view, o.boundary, dx, o.area.W)
}

// ResizeEdges computes the resize edges for a press at (px,py) inside
// rect r: the window is divided into a 3x3 grid; corners resize two
// edges, sides one, and the centre resizes the bottom-right corner.
func ResizeEdges(r wm.Rect, px, py int32) Edges {
	var edges Edges
	thirdW := max(r.W/3, 1)
	thirdH := max(r.H/3, 1)
	switch x := px - r.X; {
	case x < thirdW:
		edges |= EdgeLeft
	case x >= 2*thirdW:
		edges |= EdgeRight
	}
	switch y := py - r.Y; {
	case y < thirdH:
		edges |= EdgeTop
	case y >= 2*thirdH:
		edges |= EdgeBottom
	}
	if edges == 0 {
		edges = EdgeRight | EdgeBottom
	}
	return edges
}

func newMoveOp(st *wm.State, win wm.WindowID) *MoveOp {
	return &MoveOp{Win: win, state: st, start: st.FloatRectOf(win)}
}

func newResizeOp(st *wm.State, win wm.WindowID, edges Edges) *ResizeOp {
	return &ResizeOp{Win: win, Edges: edges, state: st, start: st.FloatRectOf(win)}
}

// newColumnResizeOp drags the column boundary of v nearest to px. It
// returns nil when v has no tiling area or no columns.
func newColumnResizeOp(st *wm.State, v *wm.View, px int32) *ColumnResizeOp {
	area := st.OutputArea(v.Name)
	if area.W == 0 || len(v.Columns) < 1 {
		return nil
	}
	bounds := st.ColumnBoundaries(v, area)
	factors := make([]float64, len(v.Columns))
	for i, c := range v.Columns {
		factors[i] = c.Factor
	}
	return &ColumnResizeOp{
		state:    st,
		view:     v,
		boundary: wm.NearestColumnBoundary(bounds, px),
		area:     area,
		factors:  factors,
	}
}

// StartPointerOp picks the op for a Mod+button press at (px,py) over
// window win: move or resize a floating window, or drag a column
// boundary of a tiled one. It returns nil when the press starts
// nothing.
func StartPointerOp(st *wm.State, win wm.WindowID, b Button, px, py int32) PointerOp {
	if st.Windows[win] == nil {
		return nil
	}
	v := st.ActiveViewOf(win)
	floating := v != nil && v.FloatContains(win)
	switch {
	case b == ButtonMove && floating:
		return newMoveOp(st, win)
	case b == ButtonResize && floating:
		return newResizeOp(st, win, ResizeEdges(st.FloatRectOf(win), px, py))
	case b == ButtonResize && v != nil:
		if op := newColumnResizeOp(st, v, px); op != nil {
			return op
		}
	}
	return nil
}

// ClientMoveOp starts a client-requested move (e.g. a CSD titlebar
// drag). Only floating windows move freely; the window is focused.
// It returns nil when no move starts.
func ClientMoveOp(st *wm.State, win wm.WindowID) PointerOp {
	if st.Windows[win] == nil {
		return nil
	}
	v := st.ActiveViewOf(win)
	if v == nil || !v.FloatContains(win) {
		return nil
	}
	st.FocusWindow(win)
	return newMoveOp(st, win)
}

// ClientResizeOp starts a client-requested resize by edges. The
// window is focused; a floating window resizes, a tiled one drags the
// column boundary nearest to px. It returns nil when no resize
// starts.
func ClientResizeOp(st *wm.State, win wm.WindowID, edges Edges, px int32) PointerOp {
	if st.Windows[win] == nil {
		return nil
	}
	st.FocusWindow(win)
	v := st.ActiveViewOf(win)
	switch {
	case v == nil:
		return nil
	case v.FloatContains(win):
		return newResizeOp(st, win, edges)
	}
	if op := newColumnResizeOp(st, v, px); op != nil {
		return op
	}
	return nil
}

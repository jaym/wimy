package river

import (
	"context"

	"wimy/internal/proto"
	"wimy/internal/wm"
)

// Pointer buttons (linux/input-event-codes.h).
const (
	btnLeft  = 0x110
	btnRight = 0x111
)

// PointerBinding wraps a river_pointer_binding_v1 object.
type PointerBinding struct {
	proto.RiverPointerBindingV1Stub

	Object proto.RiverPointerBindingV1
	Seat   *Seat
	Button uint32

	OnPressed func(button uint32)
}

func (b *PointerBinding) HandleRiverPointerBindingV1Pressed(ctx context.Context) {
	if b.OnPressed != nil {
		b.OnPressed(b.Button)
	}
}

// SeatOp is an interactive pointer operation (move/resize). Deltas are
// cumulative from the op start and applied during manage sequences.
type SeatOp interface {
	Apply(s *Seat, dx, dy int32)
	End(s *Seat)
}

// seatOpMove moves a floating window.
type seatOpMove struct {
	backend *Backend
	win     wm.WindowID
	start   wm.Rect
}

func (o *seatOpMove) Apply(s *Seat, dx, dy int32) {
	o.backend.state.SetFloatRect(o.win, wm.Rect{
		X: o.start.X + dx, Y: o.start.Y + dy, W: o.start.W, H: o.start.H,
	})
}

func (o *seatOpMove) End(s *Seat) {}

// seatOpResize resizes a floating window by the given edges.
type seatOpResize struct {
	backend *Backend
	win     wm.WindowID
	edges   uint32
	start   wm.Rect
}

func (o *seatOpResize) Apply(s *Seat, dx, dy int32) {
	r := o.start
	if o.edges&proto.RiverWindowV1EdgesLeft != 0 {
		r.X = o.start.X + dx
		r.W = o.start.W - dx
	}
	if o.edges&proto.RiverWindowV1EdgesRight != 0 {
		r.W = o.start.W + dx
	}
	if o.edges&proto.RiverWindowV1EdgesTop != 0 {
		r.Y = o.start.Y + dy
		r.H = o.start.H - dy
	}
	if o.edges&proto.RiverWindowV1EdgesBottom != 0 {
		r.H = o.start.H + dy
	}
	// keep the window from inverting past its minimum size
	if r.W < 50 {
		if o.edges&proto.RiverWindowV1EdgesLeft != 0 {
			r.X -= 50 - r.W
		}
		r.W = 50
	}
	if r.H < 50 {
		if o.edges&proto.RiverWindowV1EdgesTop != 0 {
			r.Y -= 50 - r.H
		}
		r.H = 50
	}
	o.backend.state.SetFloatRect(o.win, r)
}

func (o *seatOpResize) End(s *Seat) {
	if w := o.backend.windowByID(o.win); w != nil {
		w.Object.InformResizeEnd()
	}
}

// seatOpColumnResize drags a tiled column boundary.
type seatOpColumnResize struct {
	backend  *Backend
	view     *wm.View
	boundary int
	area     wm.Rect
	factors  []float64 // factors at op start (deltas are cumulative)
}

func (o *seatOpColumnResize) Apply(s *Seat, dx, dy int32) {
	for i, f := range o.factors {
		o.view.Columns[i].Factor = f
	}
	o.backend.state.ResizeColumnBoundary(o.view, o.boundary, dx, o.area.W)
}

func (o *seatOpColumnResize) End(s *Seat) {}

// resizeEdges computes the resize edges for a press at (px,py) inside
// rect r: the window is divided into a 3x3 grid; corners resize two
// edges, sides one.
func resizeEdges(r wm.Rect, px, py int32) uint32 {
	var edges uint32
	thirdW := max32(r.W/3, 1)
	thirdH := max32(r.H/3, 1)
	switch x := px - r.X; {
	case x < thirdW:
		edges |= proto.RiverWindowV1EdgesLeft
	case x >= 2*thirdW:
		edges |= proto.RiverWindowV1EdgesRight
	}
	switch y := py - r.Y; {
	case y < thirdH:
		edges |= proto.RiverWindowV1EdgesTop
	case y >= 2*thirdH:
		edges |= proto.RiverWindowV1EdgesBottom
	}
	if edges == 0 {
		edges = proto.RiverWindowV1EdgesRight | proto.RiverWindowV1EdgesBottom
	}
	return edges
}

// pointerPress handles Mod+button presses over a window.
func (b *Backend) pointerPress(s *Seat, button uint32) {
	w := s.Hovered
	if w == nil {
		return
	}
	if s.Op != nil {
		return
	}
	win := b.state.Windows[w.ID]
	if win == nil {
		return
	}
	v := b.state.ActiveViewOf(w.ID)
	floating := v != nil && v.FloatContains(w.ID)

	switch {
	case button == btnLeft && floating:
		s.Op = &seatOpMove{backend: b, win: w.ID, start: b.state.FloatRectOf(w.ID)}
	case button == btnRight && floating:
		edges := resizeEdges(b.state.FloatRectOf(w.ID), s.PointerX, s.PointerY)
		s.Op = &seatOpResize{backend: b, win: w.ID, edges: edges, start: b.state.FloatRectOf(w.ID)}
		w.Object.InformResizeStart()
	case button == btnRight && !floating && v != nil:
		area := b.state.OutputArea(v.Name)
		if area.W == 0 || len(v.Columns) < 1 {
			return
		}
		bounds := b.state.ColumnBoundaries(v, area)
		idx := wm.NearestColumnBoundary(bounds, s.PointerX)
		factors := make([]float64, len(v.Columns))
		for i, c := range v.Columns {
			factors[i] = c.Factor
		}
		s.Op = &seatOpColumnResize{backend: b, view: v, boundary: idx, area: area, factors: factors}
	default:
		return
	}
	s.Object.OpStartPointer()
}

// clientMoveRequest starts a client-initiated interactive move
// (e.g. a CSD titlebar drag).
func (b *Backend) clientMoveRequest(w *Window, seat proto.RiverSeatV1) {
	s := seatFromObject(b, seat)
	if s == nil || s.Op != nil {
		return
	}
	win := b.state.Windows[w.ID]
	if win == nil {
		return
	}
	v := b.state.ActiveViewOf(w.ID)
	if v == nil || !v.FloatContains(w.ID) {
		return // only floating windows move freely
	}
	b.state.FocusWindow(w.ID)
	s.Op = &seatOpMove{backend: b, win: w.ID, start: b.state.FloatRectOf(w.ID)}
	s.Object.OpStartPointer()
}

// clientResizeRequest starts a client-initiated interactive resize.
func (b *Backend) clientResizeRequest(w *Window, seat proto.RiverSeatV1, edges uint32) {
	s := seatFromObject(b, seat)
	if s == nil || s.Op != nil {
		return
	}
	win := b.state.Windows[w.ID]
	if win == nil {
		return
	}
	b.state.FocusWindow(w.ID)
	v := b.state.ActiveViewOf(w.ID)
	if v != nil && v.FloatContains(w.ID) {
		s.Op = &seatOpResize{backend: b, win: w.ID, edges: edges, start: b.state.FloatRectOf(w.ID)}
	} else if v != nil {
		// tiled: a resize drag adjusts the nearest column boundary
		area := b.state.OutputArea(v.Name)
		bounds := b.state.ColumnBoundaries(v, area)
		idx := wm.NearestColumnBoundary(bounds, s.PointerX)
		factors := make([]float64, len(v.Columns))
		for i, c := range v.Columns {
			factors[i] = c.Factor
		}
		s.Op = &seatOpColumnResize{backend: b, view: v, boundary: idx, area: area, factors: factors}
	} else {
		return
	}
	w.Object.InformResizeStart()
	s.Object.OpStartPointer()
}

func seatFromObject(b *Backend, seat proto.RiverSeatV1) *Seat {
	for _, s := range b.seats {
		if s.Object == seat {
			return s
		}
	}
	return nil
}

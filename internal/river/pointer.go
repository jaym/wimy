//go:build linux

package river

import (
	"context"

	"wimy/internal/backend"
	"wimy/internal/proto"
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

// edgesFromProto converts river_window_v1.edges to backend.Edges.
func edgesFromProto(e uint32) backend.Edges {
	var out backend.Edges
	if e&proto.RiverWindowV1EdgesTop != 0 {
		out |= backend.EdgeTop
	}
	if e&proto.RiverWindowV1EdgesBottom != 0 {
		out |= backend.EdgeBottom
	}
	if e&proto.RiverWindowV1EdgesLeft != 0 {
		out |= backend.EdgeLeft
	}
	if e&proto.RiverWindowV1EdgesRight != 0 {
		out |= backend.EdgeRight
	}
	return out
}

// pointerPress handles Mod+button presses over a window.
func (b *Backend) pointerPress(s *Seat, button uint32) {
	w := s.Hovered
	if w == nil || s.Op != nil {
		return
	}
	btn := backend.ButtonMove
	if button == btnRight {
		btn = backend.ButtonResize
	}
	op := backend.StartPointerOp(b.state, w.ID, btn, s.PointerX, s.PointerY)
	if op == nil {
		return
	}
	if _, ok := op.(*backend.ResizeOp); ok {
		w.Object.InformResizeStart()
	}
	s.Op = op
	s.Object.OpStartPointer()
}

// clientMoveRequest starts a client-initiated interactive move
// (e.g. a CSD titlebar drag).
func (b *Backend) clientMoveRequest(w *Window, seat proto.RiverSeatV1) {
	s := seatFromObject(b, seat)
	if s == nil || s.Op != nil {
		return
	}
	op := backend.ClientMoveOp(b.state, w.ID)
	if op == nil {
		return
	}
	s.Op = op
	s.Object.OpStartPointer()
}

// clientResizeRequest starts a client-initiated interactive resize.
func (b *Backend) clientResizeRequest(w *Window, seat proto.RiverSeatV1, edges uint32) {
	s := seatFromObject(b, seat)
	if s == nil || s.Op != nil {
		return
	}
	op := backend.ClientResizeOp(b.state, w.ID, edgesFromProto(edges), s.PointerX)
	if op == nil {
		return
	}
	w.Object.InformResizeStart()
	s.Op = op
	s.Object.OpStartPointer()
}

// endOp finishes an interactive op: a floating resize tells the
// client the resize is over (column drags never informed it).
func (b *Backend) endOp(op backend.PointerOp) {
	if r, ok := op.(*backend.ResizeOp); ok {
		if w := b.windowByID(r.Win); w != nil {
			w.Object.InformResizeEnd()
		}
	}
}

func seatFromObject(b *Backend, seat proto.RiverSeatV1) *Seat {
	for _, s := range b.seats {
		if s.Object == seat {
			return s
		}
	}
	return nil
}

package river

import (
	"context"

	"hazelnut.eclair.cafe/wlcl"

	"wimy/internal/proto"
	"wimy/internal/wm"
)

// Output wraps a river_output_v1 object.
type Output struct {
	proto.RiverOutputV1Stub

	Object  proto.RiverOutputV1
	Backend *Backend

	WlName uint32 // global name of the corresponding wl_output
	Name   string // from the corresponding wl_output
	X, Y   int32
	W, H   int32

	NameInModel string // name currently used in the model
	Added       bool   // registered in the model
	Removed     bool
}

func (o *Output) HandleRiverOutputV1Removed(ctx context.Context) { o.Removed = true }

func (o *Output) HandleRiverOutputV1WlOutput(ctx context.Context, name uint32) {
	o.WlName = name
	o.Backend.resolveOutputNames()
}

func (o *Output) HandleRiverOutputV1Position(ctx context.Context, x int32, y int32) {
	o.X, o.Y = x, y
}

func (o *Output) HandleRiverOutputV1Dimensions(ctx context.Context, width int32, height int32) {
	o.W, o.H = width, height
}

func (o *Output) MaybeDestroy() bool {
	if !o.Removed {
		return false
	}
	o.Object.Destroy()
	return true
}

// Window wraps a river_window_v1 object and its node.
type Window struct {
	proto.RiverWindowV1Stub

	Object proto.RiverWindowV1
	Node   proto.RiverNodeV1

	ID     wm.WindowID
	AppID  string
	Title  string
	Parent bool // has a parent window (dialogs etc.)

	Width, Height int32 // actual dimensions from the compositor

	New       bool
	Closed    bool
	Shown     bool // last show/hide state applied
	Clipped   bool // a clip box was applied
	FocusSent bool // last focus border state applied
	BorderSet bool // a border was applied at least once

	Fullscreen      bool
	FullscreenReq   bool // client requested fullscreen
	UnfullscreenReq bool // client requested to exit fullscreen
}

func NewWindow(object proto.RiverWindowV1, id wm.WindowID) *Window {
	w := &Window{
		Object: object,
		Node:   object.GetNode(),
		ID:     id,
		New:    true,
	}
	object.SetUserData(w)
	return w
}

func (w *Window) HandleRiverWindowV1Closed(ctx context.Context) { w.Closed = true }

func (w *Window) HandleRiverWindowV1Dimensions(ctx context.Context, width int32, height int32) {
	w.Width, w.Height = width, height
}

func (w *Window) HandleRiverWindowV1AppId(ctx context.Context, appId wlcl.NullString) {
	if appId.Valid {
		w.AppID = appId.String
	}
}

func (w *Window) HandleRiverWindowV1Title(ctx context.Context, title wlcl.NullString) {
	if title.Valid {
		w.Title = title.String
	}
}

func (w *Window) HandleRiverWindowV1Parent(ctx context.Context, parent proto.RiverWindowV1) {
	w.Parent = parent.IsSet()
}

func (w *Window) HandleRiverWindowV1FullscreenRequested(ctx context.Context, output proto.RiverOutputV1) {
	w.FullscreenReq = true
}

func (w *Window) HandleRiverWindowV1ExitFullscreenRequested(ctx context.Context) {
	w.UnfullscreenReq = true
}

// Seat wraps a river_seat_v1 object and its xkb bindings.
type Seat struct {
	proto.RiverSeatV1Stub

	Object proto.RiverSeatV1

	New     bool
	Removed bool

	Interacted *Window
}

func (s *Seat) HandleRiverSeatV1Removed(ctx context.Context) { s.Removed = true }

func (s *Seat) HandleRiverSeatV1WindowInteraction(ctx context.Context, window proto.RiverWindowV1) {
	if w, ok := window.UserData().(*Window); ok {
		s.Interacted = w
	}
}

func (s *Seat) MaybeDestroy(bindings []*XkbBinding) bool {
	if !s.Removed {
		return false
	}
	for _, b := range bindings {
		b.Object.Destroy()
	}
	s.Object.Destroy()
	return true
}

// XkbBinding wraps a river_xkb_binding_v1 object.
type XkbBinding struct {
	proto.RiverXkbBindingV1Stub

	Object  proto.RiverXkbBindingV1
	Seat    *Seat
	Command string

	// OnPressed is called when the binding is triggered.
	OnPressed func(command string)
}

func (b *XkbBinding) HandleRiverXkbBindingV1Pressed(ctx context.Context) {
	if b.OnPressed != nil {
		b.OnPressed(b.Command)
	}
}

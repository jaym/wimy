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

	Object      proto.RiverOutputV1
	LayerOutput proto.RiverLayerShellOutputV1
	Backend     *Backend

	WlName uint32 // global name of the corresponding wl_output
	Name   string // from the corresponding wl_output
	X, Y   int32
	W, H   int32
	// UsableX/Y/W/H is the non-exclusive area (after layer shell
	// exclusive zones). Zero until the first event arrives.
	UsableX, UsableY, UsableW, UsableH int32
	// Scale is the output's integer scale factor (from wl_output).
	Scale int32

	NameInModel string // name currently used in the model
	Added       bool   // registered in the model
	Removed     bool
}

func (o *Output) HandleRiverOutputV1Removed(ctx context.Context) { o.Removed = true }

func (o *Output) HandleRiverOutputV1WlOutput(ctx context.Context, name uint32) {
	o.WlName = name
	o.Backend.resolveOutputInfo()
}

// HandleRiverLayerShellOutputV1NonExclusiveArea records the area left
// for tiling after layer shell exclusive zones.
func (o *Output) HandleRiverLayerShellOutputV1NonExclusiveArea(ctx context.Context, x int32, y int32, width int32, height int32) {
	o.UsableX, o.UsableY, o.UsableW, o.UsableH = x, y, width, height
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
	if o.LayerOutput.IsSet() {
		o.LayerOutput.Destroy()
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

	New            bool
	Closed         bool
	Shown          bool // last show/hide state applied
	Clipped        bool // a clip box was applied
	ContentClipped bool // a content clip box was applied
	FocusSent      bool // last focus border state applied
	BarSent        bool // last titlebar border state applied
	BorderSet      bool // a border was applied at least once

	// CSDOnly is true for clients that only support client-side
	// decorations (decoration_hint); they keep their own titlebar
	// and get no wimy titlebar.
	CSDOnly  bool
	DecoSent bool // use_ssd/use_csd was sent

	// Titlebar decoration
	Deco        proto.RiverDecorationV1
	DecoSurface proto.WlSurface
	DecoTitle   string
	DecoFocused bool
	DecoWidth   int32
	DecoScale   int32

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

// HandleRiverWindowV1DecorationHint notes clients that can only do CSD.
func (w *Window) HandleRiverWindowV1DecorationHint(ctx context.Context, hint uint32) {
	only := hint == proto.RiverWindowV1DecorationHintOnlySupportsCsd
	if only != w.CSDOnly {
		w.CSDOnly = only
		w.DecoSent = false // renegotiate at the next manage sequence
	}
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

	Object    proto.RiverSeatV1
	LayerSeat proto.RiverLayerShellSeatV1
	Backend   *Backend

	New     bool
	Removed bool

	Interacted *Window
}

func (s *Seat) HandleRiverSeatV1Removed(ctx context.Context) { s.Removed = true }

// HandleRiverLayerShellSeatV1FocusExclusive marks that a layer shell
// surface (e.g. a launcher) takes exclusive keyboard focus.
func (s *Seat) HandleRiverLayerShellSeatV1FocusExclusive(ctx context.Context) {
	s.Backend.layerFocus = true
}

// HandleRiverLayerShellSeatV1FocusNonExclusive marks that a layer
// shell surface takes non-exclusive keyboard focus.
func (s *Seat) HandleRiverLayerShellSeatV1FocusNonExclusive(ctx context.Context) {
	s.Backend.layerFocus = true
}

// HandleRiverLayerShellSeatV1FocusNone marks that no layer shell
// surface has keyboard focus anymore; focus returns to the focused
// window in the following manage sequence.
func (s *Seat) HandleRiverLayerShellSeatV1FocusNone(ctx context.Context) {
	s.Backend.layerFocus = false
}

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
	if s.LayerSeat.IsSet() {
		s.LayerSeat.Destroy()
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

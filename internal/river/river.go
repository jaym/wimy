// Package river implements the Wayland backend of wimy: it speaks the
// river-window-management-v1 protocol to the compositor, drives the
// pure wm model, and applies the model's layout back to the
// compositor in manage/render sequences.
package river

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"log"
	"os"
	"sync"

	"hazelnut.eclair.cafe/wlcl"

	"wimy/internal/command"
	"wimy/internal/config"
	"wimy/internal/proto"
	"wimy/internal/titlebar"
	"wimy/internal/wm"
)

// Backend is the river protocol backend.
type Backend struct {
	proto.WlRegistryStub
	proto.RiverWindowManagerV1Stub

	cfg   *config.Config
	state *wm.State
	reg   *command.Registry

	conn     *wlcl.Connection
	registry proto.WlRegistry
	wmg      proto.RiverWindowManagerV1
	xkb      proto.RiverXkbBindingsV1
	layer    proto.RiverLayerShellV1
	comp     proto.WlCompositor
	shm      proto.WlShm
	tbr      *titlebar.Renderer

	outputs         []*Output
	windows         []*Window
	seats           []*Seat
	bindings        []*XkbBinding
	pointerBindings []*PointerBinding

	wlOutputNames  map[uint32]string
	wlOutputScales map[uint32]int32

	mu     sync.Mutex
	queue  []string
	nextID wm.WindowID

	done   bool
	err    error
	cancel context.CancelFunc

	// exitSession is set once ExitSession was requested: subsequent
	// connection errors are the expected session teardown.
	exitSession bool

	notify func()

	lastFocus wm.WindowID

	// layerFocus is true while a layer shell surface (launcher etc.)
	// holds keyboard focus.
	layerFocus        bool
	lastDefaultOutput string
}

// New creates a backend. notify is called (from the dispatch
// goroutine) after every manage sequence in which state may have
// changed; it must not block.
func New(cfg *config.Config, notify func()) *Backend {
	b := &Backend{
		cfg:            cfg,
		state:          wm.NewState(),
		wlOutputNames:  make(map[uint32]string),
		wlOutputScales: make(map[uint32]int32),
		nextID:         1,
		notify:         notify,
	}
	b.state.StackStrip = cfg.StackStrip
	b.state.TitlebarHeight = cfg.Titlebar.Height
	b.tbr = titlebar.New(cfg.Titlebar.Height, titlebar.Colors{
		FocusedBg:     toRGBA(cfg.Titlebar.FocusedBg),
		FocusedFg:     toRGBA(cfg.Titlebar.FocusedFg),
		NormalBg:      toRGBA(cfg.Titlebar.NormalBg),
		NormalFg:      toRGBA(cfg.Titlebar.NormalFg),
		BorderFocused: toRGBA(cfg.Border.Focused),
		BorderNormal:  toRGBA(cfg.Border.Normal),
	}, cfg.Border.Width)
	b.reg = command.New(&command.Env{State: b.state, Fx: b})
	return b
}

// toRGBA converts a 32-bit-per-channel protocol color to 8-bit.
func toRGBA(c config.Color) color.RGBA {
	return color.RGBA{R: uint8(c.R >> 24), G: uint8(c.G >> 24), B: uint8(c.B >> 24), A: uint8(c.A >> 24)}
}

// State returns the model (only safe to use inside Snapshot or from
// the dispatch goroutine).
func (b *Backend) State() *wm.State { return b.state }

// CommandNames returns the registered command names.
func (b *Backend) CommandNames() []string { return b.reg.Names() }

// Snapshot runs fn with exclusive access to the model: no protocol
// event is dispatched while fn runs.
func (b *Backend) Snapshot(fn func(*wm.State)) {
	b.conn.DoSync(func() { fn(b.state) })
}

// QueueCommand appends a command string to the pending queue and asks
// the compositor for a manage sequence, in which the queue is drained.
// It is safe to call from any goroutine.
func (b *Backend) QueueCommand(cmd string) {
	b.mu.Lock()
	b.queue = append(b.queue, cmd)
	b.mu.Unlock()
	if b.conn != nil {
		b.conn.DoSync(func() {
			if b.wmg.IsSet() {
				b.wmg.ManageDirty()
				_ = b.conn.Flush()
			}
		})
	}
}

// Quit ends the Wayland session (exit_session): the compositor exits
// and every client, including wimy, is disconnected. wimy itself only
// exits without ending the session on signals or crashes — per the
// protocol, normal WM termination must not end the session.
func (b *Backend) Quit() {
	b.exitSession = true
	b.wmg.ExitSession()
}

// Shutdown stops the backend's event loop without ending the
// session (river keeps running without a window manager).
func (b *Backend) Shutdown() {
	b.mu.Lock()
	b.done = true
	b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
	}
}

func (b *Backend) isDone() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.done
}

// Run connects to the compositor and runs the event loop until Quit
// is called or the compositor drops us.
func (b *Backend) Run(ctx context.Context) (err error) {
	conn, err := wlcl.Connect(ctx, "")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	b.conn = conn
	defer func() {
		if cerr := conn.Close(); cerr != nil && err == nil && !errors.Is(cerr, wlcl.ErrClosed) {
			err = cerr
		}
	}()

	// Don't pass these on to children: WAYLAND_SOCKET is ours alone and
	// WAYLAND_DEBUG noise makes debugging the WM itself impractical.
	_ = os.Unsetenv("WAYLAND_SOCKET")
	_ = os.Unsetenv("WAYLAND_DEBUG")

	display := proto.CreateDisplay(conn)

	b.registry = display.GetRegistry()
	b.registry.SetUserData(b)

	if err := wlcl.Roundtrip(ctx, display); err != nil {
		return fmt.Errorf("roundtrip: %w", err)
	}
	if !b.wmg.IsSet() {
		return fmt.Errorf("river_window_manager_v1 global not available (is this river 0.4+?)")
	}
	if !b.xkb.IsSet() {
		return fmt.Errorf("river_xkb_bindings_v1 global not available")
	}
	if !b.layer.IsSet() {
		return fmt.Errorf("river_layer_shell_v1 global not available (river too old?)")
	}
	b.wmg.SetUserData(b)

	b.runAutostart()

	dctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel

	for !b.isDone() {
		err := conn.Dispatch(dctx)
		if err == nil {
			continue
		}
		if errors.Is(err, context.Canceled) {
			// Shutdown was called, or the parent context (signal) fired
			break
		}
		if b.exitSession {
			// the compositor is tearing the session down
			break
		}
		if cerr := conn.Err(); cerr != nil {
			return fmt.Errorf("dispatch: %w", cerr)
		}
		return fmt.Errorf("dispatch: %w", err)
	}
	return b.err
}

// --- registry ---

// HandleWlRegistryGlobal binds the globals wimy needs.
func (b *Backend) HandleWlRegistryGlobal(ctx context.Context, name uint32, iface string, version uint32) {
	switch iface {
	case proto.RiverWindowManagerV1Name:
		if version >= 4 {
			b.wmg = proto.As[proto.RiverWindowManagerV1](b.registry.Bind(name, iface, 4))
		}
	case proto.RiverXkbBindingsV1Name:
		b.xkb = proto.As[proto.RiverXkbBindingsV1](b.registry.Bind(name, iface, 1))
	case proto.RiverLayerShellV1Name:
		b.layer = proto.As[proto.RiverLayerShellV1](b.registry.Bind(name, iface, 1))
	case proto.WlCompositorName:
		if version > 4 {
			version = 4
		}
		b.comp = proto.As[proto.WlCompositor](b.registry.Bind(name, iface, version))
	case proto.WlShmName:
		b.shm = proto.As[proto.WlShm](b.registry.Bind(name, iface, 1))
	case proto.WlOutputName:
		bindWlOutput(b, name, version)
	}
}

// wlOutput tracks a bound wl_output to learn the output's name.
type wlOutput struct {
	proto.WlOutputStub

	b      *Backend
	global uint32
}

func (o *wlOutput) HandleWlOutputName(ctx context.Context, name string) {
	o.b.wlOutputNames[o.global] = name
	o.b.resolveOutputInfo()
}

func (o *wlOutput) HandleWlOutputScale(ctx context.Context, factor int32) {
	o.b.wlOutputScales[o.global] = factor
	o.b.resolveOutputInfo()
}

func bindWlOutput(b *Backend, name uint32, version uint32) {
	if version > 4 {
		version = 4 // we only need up to wl_output.name
	}
	obj := proto.As[proto.WlOutput](b.registry.Bind(name, proto.WlOutputName, version))
	obj.SetUserData(&wlOutput{b: b, global: name})
}

// resolveOutputInfo applies learned wl_output names and scales to
// river outputs.
func (b *Backend) resolveOutputInfo() {
	for _, o := range b.outputs {
		if o.WlName == 0 {
			continue
		}
		if o.Name == "" {
			o.Name = b.wlOutputNames[o.WlName]
		}
		if s := b.wlOutputScales[o.WlName]; s > 0 {
			o.Scale = s
		}
	}
}

// --- window manager global events ---

func (b *Backend) HandleRiverWindowManagerV1Unavailable(ctx context.Context) {
	b.err = errors.New("another window manager is already running")
	b.Shutdown()
}

func (b *Backend) HandleRiverWindowManagerV1Finished(ctx context.Context) {
	b.Shutdown()
}

func (b *Backend) HandleRiverWindowManagerV1Output(ctx context.Context, id proto.RiverOutputV1) {
	o := &Output{Object: id, Backend: b}
	id.SetUserData(o)
	if b.layer.IsSet() {
		o.LayerOutput = b.layer.GetOutput(id)
		o.LayerOutput.SetUserData(o)
	}
	b.outputs = append(b.outputs, o)
}

func (b *Backend) HandleRiverWindowManagerV1Window(ctx context.Context, id proto.RiverWindowV1) {
	w := NewWindow(id, b.nextID, b)
	b.nextID++
	b.windows = append(b.windows, w)
}

func (b *Backend) HandleRiverWindowManagerV1Seat(ctx context.Context, id proto.RiverSeatV1) {
	s := &Seat{Object: id, New: true, Backend: b}
	id.SetUserData(s)
	if b.layer.IsSet() {
		s.LayerSeat = b.layer.GetSeat(id)
		s.LayerSeat.SetUserData(s)
	}
	b.seats = append(b.seats, s)
	for _, bind := range b.cfg.Binds {
		obj := b.xkb.GetXkbBinding(id, bind.Keysym, bind.Mods)
		xb := &XkbBinding{
			Object:  obj,
			Seat:    s,
			Command: bind.Command,
			OnPressed: func(cmd string) {
				b.mu.Lock()
				b.queue = append(b.queue, cmd)
				b.mu.Unlock()
			},
		}
		obj.SetUserData(xb)
		b.bindings = append(b.bindings, xb)
	}
	// pointer bindings: Mod+drag move/resize
	for _, button := range []uint32{btnLeft, btnRight} {
		obj := id.GetPointerBinding(button, b.cfg.ModMask)
		pb := &PointerBinding{
			Object: obj,
			Seat:   s,
			Button: button,
			OnPressed: func(btn uint32) {
				b.pointerPress(s, btn)
			},
		}
		obj.SetUserData(pb)
		b.pointerBindings = append(b.pointerBindings, pb)
	}
}

// HandleRiverWindowManagerV1ManageStart runs a manage sequence: sync
// the model with compositor state, drain the command queue, then
// propose new window-management state.
func (b *Backend) HandleRiverWindowManagerV1ManageStart(ctx context.Context) {
	b.syncModel()
	b.drainQueue()
	b.applyManage()
	b.wmg.ManageFinish()
	if b.notify != nil {
		b.notify()
	}
}

// HandleRiverWindowManagerV1RenderStart runs a render sequence: apply
// positions, stacking, clipping, visibility and borders.
func (b *Backend) HandleRiverWindowManagerV1RenderStart(ctx context.Context) {
	b.applyRender()
	b.wmg.RenderFinish()
}

// syncModel applies compositor-side changes to the model: new and
// removed outputs/windows, geometry changes, click focus.
func (b *Backend) syncModel() {
	// outputs
	for _, o := range b.outputs {
		if o.Removed {
			b.state.RemoveOutput(o.NameInModel)
		}
	}
	b.outputs = deleteFunc(b.outputs, (*Output).MaybeDestroy)
	for _, o := range b.outputs {
		name := outputName(o)
		if !o.Added {
			o.NameInModel = name
			b.state.AddOutput(name)
			o.Added = true
		} else if o.NameInModel != name {
			b.state.RenameOutput(o.NameInModel, name)
			o.NameInModel = name
		}
		b.state.SetOutputGeometry(o.NameInModel, o.X, o.Y, o.W, o.H)
		if o.UsableW > 0 {
			b.state.SetOutputUsable(o.NameInModel, o.UsableX, o.UsableY, o.UsableW, o.UsableH)
		}
	}

	// windows
	for _, w := range b.windows {
		if w.New {
			w.New = false
			b.state.AddWindow(w.ID, w.Parent)
		}
		if w.Closed {
			continue
		}
		if w.AppID != "" {
			b.state.SetAppID(w.ID, w.AppID)
		}
		if w.Title != "" {
			b.state.SetTitle(w.ID, w.Title)
		}
	}
	kept := b.windows[:0]
	for _, w := range b.windows {
		if w.Closed {
			b.state.RemoveWindow(w.ID)
			w.destroyDeco()
			w.Node.Destroy()
			w.Object.Destroy()
		} else {
			kept = append(kept, w)
		}
	}
	b.windows = kept

	// seats
	for _, s := range b.seats {
		if s.Interacted != nil {
			b.state.FocusWindow(s.Interacted.ID)
			s.Interacted = nil
		}
	}
	b.seats = deleteFunc(b.seats, func(s *Seat) bool {
		return s.MaybeDestroy(b.bindings)
	})
}

// drainQueue executes pending commands (key bindings, RPC, prompts).
func (b *Backend) drainQueue() {
	b.mu.Lock()
	queue := b.queue
	b.queue = nil
	b.mu.Unlock()
	for _, cmd := range queue {
		if err := b.reg.Run(cmd); err != nil {
			log.Printf("command %q: %v", cmd, err)
		}
	}
}

// applyManage proposes window-management state: focus, dimensions,
// tiled edges, fullscreen transitions.
func (b *Backend) applyManage() {
	// enable bindings of new seats
	for _, s := range b.seats {
		if s.New {
			s.New = false
			for _, xb := range b.bindings {
				if xb.Seat == s {
					xb.Object.Enable()
				}
			}
			for _, pb := range b.pointerBindings {
				if pb.Seat == s {
					pb.Object.Enable()
				}
			}
		}
		// interactive pointer ops
		if s.Op != nil {
			switch {
			case s.OpReleased:
				s.Op.End(s)
				s.Object.OpEnd()
				s.Op = nil
				s.OpReleased = false
			default:
				s.Op.Apply(s, s.OpDx, s.OpDy)
			}
		}
	}

	// fullscreen requests
	for _, w := range b.windows {
		if !w.DecoSent {
			w.DecoSent = true
			if w.CSDOnly {
				w.Object.UseCsd()
			} else {
				w.Object.UseSsd()
			}
		}
		if w.FullscreenReq {
			w.FullscreenReq = false
			if out := b.outputForWindow(w); out != nil {
				w.Object.Fullscreen(out.Object)
				w.Object.InformFullscreen()
				w.Fullscreen = true
			}
		}
		if w.UnfullscreenReq {
			w.UnfullscreenReq = false
			w.Object.ExitFullscreen()
			w.Object.InformNotFullscreen()
			w.Fullscreen = false
		}
	}

	// keyboard focus: while a layer shell surface holds focus, leave
	// it alone and dim all window borders
	focused := b.windowByID(b.state.Focused)
	for _, s := range b.seats {
		if b.layerFocus {
			continue
		}
		if focused != nil {
			s.Object.FocusWindow(focused.Object)
		} else {
			s.Object.ClearFocus()
		}
	}

	// keep the default layer shell output on the active output
	if o := b.activeOutput(); o != nil && o.LayerOutput.IsSet() && b.lastDefaultOutput != o.NameInModel {
		o.LayerOutput.SetDefault()
		b.lastDefaultOutput = o.NameInModel
	}

	// dimensions
	for _, p := range b.state.Layout() {
		if p.Hidden {
			continue
		}
		w := b.windowByID(p.ID)
		if w == nil || w.Fullscreen {
			continue
		}
		w.Object.ProposeDimensions(max32(p.Rect.W, 1), max32(p.Rect.H, 1))
		if p.Layer == wm.LayerTiled {
			w.Object.SetTiled(proto.RiverWindowV1EdgesTop |
				proto.RiverWindowV1EdgesBottom |
				proto.RiverWindowV1EdgesLeft |
				proto.RiverWindowV1EdgesRight)
		} else {
			w.Object.SetTiled(0)
		}
	}
}

// applyRender applies rendering state from the layout solver.
func (b *Backend) applyRender() {
	for _, p := range b.state.Layout() {
		w := b.windowByID(p.ID)
		if w == nil {
			continue
		}
		switch {
		case p.Hidden:
			if w.Shown {
				w.Object.Hide()
				w.Shown = false
			}
		case w.Fullscreen:
			if !w.Shown {
				w.Object.Show()
				w.Shown = true
			}
			w.Node.PlaceTop()
		default:
			if !w.Shown {
				w.Object.Show()
				w.Shown = true
			}
			w.Node.SetPosition(p.Rect.X, p.Rect.Y)
			if p.Collapsed {
				if b.state.TitlebarHeight > 0 && !w.CSDOnly {
					// only the titlebar is visible
					w.Object.SetContentClipBox(0, 0, max32(p.Rect.W, 1), 1)
					w.ContentClipped = true
				} else {
					// no titlebars: clip content to a strip
					w.Object.SetClipBox(0, 0, max32(p.Rect.W, 1), max32(b.cfg.StackStrip, 1))
					w.Clipped = true
				}
			} else {
				if w.Clipped {
					w.Object.SetClipBox(0, 0, 0, 0)
					w.Clipped = false
				}
				if w.ContentClipped {
					w.Object.SetContentClipBox(0, 0, 0, 0)
					w.ContentClipped = false
				}
			}
			w.Node.PlaceTop()
			hasBar := b.state.TitlebarHeight > 0 && p.Bar && !w.CSDOnly
			b.setBorder(w, p.Focused && !b.layerFocus, hasBar)
			b.renderTitlebar(w, p)
		}
	}
}

// setBorder applies the configured border to a window if its focus
// or titlebar state changed since the last application. With a
// titlebar there is no top border: the titlebar frame covers it.
func (b *Backend) setBorder(w *Window, focused bool, hasBar bool) {
	if w.FocusSent == focused && w.BarSent == hasBar && w.BorderSet {
		return
	}
	w.FocusSent = focused
	w.BarSent = hasBar
	w.BorderSet = true
	c := b.cfg.Border.Normal
	if focused {
		c = b.cfg.Border.Focused
	}
	var edges uint32 = proto.RiverWindowV1EdgesTop | proto.RiverWindowV1EdgesBottom |
		proto.RiverWindowV1EdgesLeft | proto.RiverWindowV1EdgesRight
	if hasBar {
		edges = proto.RiverWindowV1EdgesBottom |
			proto.RiverWindowV1EdgesLeft | proto.RiverWindowV1EdgesRight
	}
	w.Object.SetBorders(edges, b.cfg.Border.Width, c.R, c.G, c.B, c.A)
}

// outputForWindow returns the river output the window is rendered on.
func (b *Backend) outputForWindow(w *Window) *Output {
	win := b.state.Windows[w.ID]
	if win == nil {
		return nil
	}
	for _, name := range win.TagList() {
		for i, o := range b.state.Outputs {
			if o.View == name && i < len(b.outputs) {
				return b.outputs[i]
			}
		}
	}
	if len(b.outputs) > 0 {
		return b.outputs[0]
	}
	return nil
}

func (b *Backend) windowByID(id wm.WindowID) *Window {
	for _, w := range b.windows {
		if w.ID == id {
			return w
		}
	}
	return nil
}

// activeOutput returns the river output that is active in the model.
func (b *Backend) activeOutput() *Output {
	if len(b.state.Outputs) == 0 || b.state.FocusOutput >= len(b.state.Outputs) {
		return nil
	}
	name := b.state.Outputs[b.state.FocusOutput].Name
	for _, o := range b.outputs {
		if o.NameInModel == name {
			return o
		}
	}
	return nil
}

func outputName(o *Output) string {
	if o.Name != "" {
		return o.Name
	}
	if o.WlName != 0 {
		return fmt.Sprintf("output-%d", o.WlName)
	}
	return fmt.Sprintf("output-%p", o)
}

func max32(v, m int32) int32 {
	if v < m {
		return m
	}
	return v
}

func deleteFunc[S ~[]E, E any](s S, f func(E) bool) S {
	var out S
	for _, e := range s {
		if !f(e) {
			out = append(out, e)
		}
	}
	return out
}

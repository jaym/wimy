// Package river implements the Wayland backend of wimy: it speaks the
// river-window-management-v1 protocol to the compositor, drives the
// pure wm model, and applies the model's layout back to the
// compositor in manage/render sequences.
package river

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"

	"hazelnut.eclair.cafe/wlcl"

	"wimy/internal/command"
	"wimy/internal/config"
	"wimy/internal/proto"
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

	outputs  []*Output
	windows  []*Window
	seats    []*Seat
	bindings []*XkbBinding

	wlOutputNames map[uint32]string

	mu     sync.Mutex
	queue  []string
	nextID wm.WindowID

	done   bool
	err    error
	cancel context.CancelFunc

	notify func()

	lastFocus wm.WindowID
}

// New creates a backend. notify is called (from the dispatch
// goroutine) after every manage sequence in which state may have
// changed; it must not block.
func New(cfg *config.Config, notify func()) *Backend {
	b := &Backend{
		cfg:           cfg,
		state:         wm.NewState(),
		wlOutputNames: make(map[uint32]string),
		nextID:        1,
		notify:        notify,
	}
	b.state.StackStrip = cfg.StackStrip
	b.reg = command.New(&command.Env{State: b.state, Fx: b})
	return b
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

// Quit stops the backend's event loop.
func (b *Backend) Quit() {
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
			// Quit was called, or the parent context (signal) fired
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
	o.b.resolveOutputNames()
}

func bindWlOutput(b *Backend, name uint32, version uint32) {
	if version > 4 {
		version = 4 // we only need up to wl_output.name
	}
	obj := proto.As[proto.WlOutput](b.registry.Bind(name, proto.WlOutputName, version))
	obj.SetUserData(&wlOutput{b: b, global: name})
}

// resolveOutputNames applies learned wl_output names to river outputs.
func (b *Backend) resolveOutputNames() {
	for _, o := range b.outputs {
		if o.Name == "" && o.WlName != 0 {
			o.Name = b.wlOutputNames[o.WlName]
		}
	}
}

// --- window manager global events ---

func (b *Backend) HandleRiverWindowManagerV1Unavailable(ctx context.Context) {
	b.err = errors.New("another window manager is already running")
	b.Quit()
}

func (b *Backend) HandleRiverWindowManagerV1Finished(ctx context.Context) {
	b.Quit()
}

func (b *Backend) HandleRiverWindowManagerV1Output(ctx context.Context, id proto.RiverOutputV1) {
	o := &Output{Object: id, Backend: b}
	id.SetUserData(o)
	b.outputs = append(b.outputs, o)
}

func (b *Backend) HandleRiverWindowManagerV1Window(ctx context.Context, id proto.RiverWindowV1) {
	w := NewWindow(id, b.nextID)
	b.nextID++
	b.windows = append(b.windows, w)
}

func (b *Backend) HandleRiverWindowManagerV1Seat(ctx context.Context, id proto.RiverSeatV1) {
	s := &Seat{Object: id, New: true}
	id.SetUserData(s)
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
		}
	}

	// fullscreen requests
	for _, w := range b.windows {
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

	// keyboard focus
	focused := b.windowByID(b.state.Focused)
	for _, s := range b.seats {
		if focused != nil {
			s.Object.FocusWindow(focused.Object)
		} else {
			s.Object.ClearFocus()
		}
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
			if p.Clip != nil {
				w.Object.SetClipBox(p.Clip.X, p.Clip.Y, p.Clip.W, p.Clip.H)
				w.Clipped = true
			} else if w.Clipped {
				w.Object.SetClipBox(0, 0, max32(p.Rect.W, 1), max32(p.Rect.H, 1))
				w.Clipped = false
			}
			w.Node.PlaceTop()
			b.setBorder(w, p.Focused)
		}
	}
}

// setBorder applies the configured border to a window if its focus
// state changed since the last application.
func (b *Backend) setBorder(w *Window, focused bool) {
	if w.FocusSent == focused && w.BorderSet {
		return
	}
	w.FocusSent = focused
	w.BorderSet = true
	c := b.cfg.Border.Normal
	if focused {
		c = b.cfg.Border.Focused
	}
	w.Object.SetBorders(
		proto.RiverWindowV1EdgesTop|proto.RiverWindowV1EdgesBottom|
			proto.RiverWindowV1EdgesLeft|proto.RiverWindowV1EdgesRight,
		b.cfg.Border.Width, c.R, c.G, c.B, c.A)
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

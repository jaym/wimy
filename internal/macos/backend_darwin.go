package macos

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework AppKit -framework ApplicationServices -framework Carbon
#include "bridge.h"
*/
import "C"

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"runtime/cgo"
	"time"

	"wimy/internal/backend"
	"wimy/internal/config"
	"wimy/internal/wm"
)

// AppKit must run on the main thread. Package initialization runs on
// the main goroutine, which is on the main thread; keep it there so
// Run (called from main) owns the thread.
func init() { runtime.LockOSThread() }

// current receives the callbacks exported to Objective-C. There is one
// backend per process.
var current *Backend

// Backend is the macOS backend. All fields except Core's queue are
// owned by the main thread.
type Backend struct {
	*backend.Core

	hotkeys   []hotkey  // all bindings; index = Carbon hotkey id
	tap       tapRouter // bindings the event tap delivers; read on the tap thread
	tapOn     bool      // the event tap is installed
	securePID int       // process holding secure input, 0 if none
	applied   *frames
	known     map[wm.WindowID]bool // windows in the model
	outputs   []outputSpec         // screens, in NSScreen order
	scheduled bool                 // an apply pass is queued on the main queue
	lastFocus wm.WindowID
	echo      focusEcho
	startup   bool // windows reported now were already open
	notify    func()

	// Windows of views that aren't shown are parked in a hide corner.
	// hidden maps each parked window to its last on-screen frame; the
	// store persists it (together with restore, the parked windows a
	// previous wimy left behind and that haven't been seen yet) so no
	// window is ever lost.
	hidden    map[wm.WindowID]wm.Rect
	restore   map[wm.WindowID]wm.Rect
	lastShown map[wm.WindowID]wm.Rect
	store     hiddenStore
}

var (
	_ backend.Platform = (*Backend)(nil)
)

// New creates the macOS backend. notify is called on the main thread
// after every apply pass; it must not block.
func New(cfg *config.Config, configArg string, notify func()) *Backend {
	b := &Backend{
		applied:   newFrames(),
		known:     make(map[wm.WindowID]bool),
		hidden:    make(map[wm.WindowID]wm.Rect),
		restore:   make(map[wm.WindowID]wm.Rect),
		lastShown: make(map[wm.WindowID]wm.Rect),
		notify:    notify,
	}
	b.Core = backend.NewCore(cfg, configArg, b)
	current = b
	return b
}

// rebind (re)registers the config's key bindings: combos with Control
// or Command as Carbon hotkeys, the rest through the event tap (see
// hotkey.viaTap). Main thread only, after wimy_app_init.
func (b *Backend) rebind() {
	C.wimy_hotkeys_clear()
	keys, unsupported := hotkeysFor(b.Cfg.Binds)
	for _, combo := range unsupported {
		log.Printf("bind %q: no macOS key or modifier for this combo; ignored", combo)
	}
	b.hotkeys = keys
	b.tap.set(keys)
	for id, k := range keys {
		if k.viaTap() {
			continue
		}
		if st := C.wimy_hotkey_register(C.uint32_t(id), C.uint16_t(k.code), C.uint32_t(k.mods)); st != 0 {
			log.Printf("bind %q: macOS refused the hotkey (OSStatus %d; another app may own it)", k.combo, int(st))
		}
	}
	if b.tap.active() && !b.tapOn {
		if C.wimy_start_keytap() != 0 {
			log.Printf("could not install the keyboard event tap: bindings without Ctrl or Cmd won't work")
		} else {
			b.tapOn = true
		}
	}
}

// checkSecureInput logs when an app starts or stops holding secure
// input while tap-delivered bindings exist: macOS then withholds all
// key events from the tap, so those bindings go dead until it ends.
func (b *Backend) checkSecureInput() {
	pid := int(C.wimy_secure_input_pid())
	if pid == b.securePID {
		return
	}
	b.securePID = pid
	if !b.tap.active() {
		return
	}
	if pid != 0 {
		log.Printf("secure input is on (pid %d, e.g. Terminal's Secure Keyboard Entry or a password field): "+
			"bindings without Ctrl or Cmd are blocked until it ends", pid)
	} else {
		log.Printf("secure input is off: all bindings work again")
	}
}

// Run checks the Accessibility permission, starts tracking windows and
// keys, and runs the AppKit event loop until Quit or Shutdown. It must
// be called from the main goroutine.
func (b *Backend) Run(ctx context.Context) error {
	if C.wimy_ax_trusted(1) == 0 {
		exe, _ := os.Executable()
		return fmt.Errorf("wimy needs the Accessibility permission: allow %s (or the terminal that starts it) "+
			"in System Settings → Privacy & Security → Accessibility, then start wimy again", exe)
	}
	C.wimy_app_init()
	b.store = hiddenStore{path: filepath.Join(stateDir(os.Getenv, homeDir()), "hidden.json")}
	if m, err := b.store.load(); err != nil {
		log.Printf("hidden-window store %s: %v (ignored)", b.store.path, err)
	} else {
		b.restore = m
	}
	b.syncScreens()
	b.rebind()
	b.startup = true
	C.wimy_start_tracking()
	b.startup = false
	// the windows already open would otherwise all stack in one
	// column per view (new windows join the focused column, as in wmii)
	for _, o := range b.outputs {
		if v := b.viewOn(o.Name); v != "" {
			b.State.SpreadColumns(v, spreadColumnCount(o.Usable.W))
		}
	}
	// start with the window the user had in front, not whichever
	// window was reported last
	if id := wm.WindowID(C.wimy_focused_window()); b.known[id] {
		b.State.FocusWindow(id)
	}
	b.lastFocus = b.State.Focused
	C.wimy_start_secure_input_poll()
	b.StartAutostart()
	b.markDirty()
	C.wimy_app_run()
	// Quit, Shutdown (SIGINT/SIGTERM): never leave windows parked
	b.unhideAll()
	return nil
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return ""
}

// guard is deferred by every callback from Objective-C: on a panic it
// puts parked windows back before the process dies.
func (b *Backend) guard() {
	if r := recover(); r != nil {
		log.Printf("wimy: panic: %v; putting hidden windows back", r)
		b.unhideAll()
		panic(r)
	}
}

// dispatch runs fn on the main thread's queue. Safe from any
// goroutine; it does not wait.
func dispatch(fn func()) {
	C.wimy_dispatch(C.uintptr_t(cgo.NewHandle(fn)))
}

//export goRunDispatched
func goRunDispatched(h C.uintptr_t) {
	defer current.guard()
	handle := cgo.Handle(h)
	fn := handle.Value().(func())
	handle.Delete()
	fn()
}

// Shutdown stops the event loop. Safe from any goroutine.
func (b *Backend) Shutdown() { dispatch(func() { C.wimy_app_stop() }) }

// Snapshot runs fn on the main thread with exclusive access to the
// model. It must not be called from the main thread.
func (b *Backend) Snapshot(fn func(*wm.State)) {
	done := make(chan struct{})
	dispatch(func() {
		fn(b.State)
		close(done)
	})
	<-done
}

// Wake implements backend.Platform: schedule an apply pass, in which
// the command queue is drained.
func (b *Backend) Wake() { dispatch(b.markDirty) }

// markDirty schedules one apply pass for the next main-queue turn;
// further events before it runs coalesce into it. Main thread only.
func (b *Backend) markDirty() {
	if b.scheduled {
		return
	}
	b.scheduled = true
	C.wimy_schedule_apply()
}

// ApplyConfigChange implements backend.Platform. Borders and
// titlebars are drawn from Phase 3 on.
func (b *Backend) ApplyConfigChange(ch backend.ConfigChange) {
	if ch.Binds || ch.Mod {
		b.rebind()
	}
	if ch.BarGap {
		b.syncScreens()
	}
}

// Kill implements backend.Platform by pressing the window's close
// button.
func (b *Backend) Kill(id wm.WindowID) { C.wimy_window_close(C.uint32_t(id)) }

// Quit implements backend.Platform: wimy exits; windows stay where
// they are.
func (b *Backend) Quit() { C.wimy_app_stop() }

//export goApply
func goApply() {
	defer current.guard()
	current.apply()
}

// apply is one manage pass: run queued commands, lay out, and push
// changed frames and focus to AX.
func (b *Backend) apply() {
	b.scheduled = false
	b.DrainQueue()
	// wimy draws no titlebars on macOS until Phase 3: reserve no space
	b.State.TitlebarHeight = 0

	var moved []wm.Placement
	shown := false
	start := time.Now()
	for _, p := range b.State.Layout() {
		if p.Hidden {
			b.hide(p.ID)
			continue
		}
		if _, parked := b.hidden[p.ID]; parked {
			delete(b.hidden, p.ID)
			shown = true
		}
		b.lastShown[p.ID] = p.Rect
		if !b.applied.changed(p.ID, p.Rect) {
			continue
		}
		r := p.Rect
		var perr, serr C.int
		if C.wimy_window_set_frame(C.uint32_t(p.ID), C.double(r.X), C.double(r.Y), C.double(r.W), C.double(r.H), &perr, &serr) != 0 {
			// often harmless: checkFrames reports whether it took
			log.Printf("window %d: AX errors setting frame (position %d, size %d)", p.ID, perr, serr)
		}
		moved = append(moved, p)
	}
	if shown {
		b.saveHidden()
	}
	if len(moved) > 0 {
		elapsed := time.Since(start)
		log.Printf("retile: %d windows in %s (%s per window)", len(moved),
			elapsed.Round(time.Millisecond), (elapsed / time.Duration(len(moved))).Round(time.Millisecond))
		b.checkFrames(moved)
	}

	b.checkSecureInput()
	if f := b.State.Focused; f != 0 && f != b.lastFocus {
		b.echo.sent(f, time.Now())
		C.wimy_window_focus(C.uint32_t(f))
	}
	b.lastFocus = b.State.Focused
	if b.notify != nil {
		b.notify()
	}
}

// frameRetryDelay is how long apply waits before re-sending a frame
// an app didn't take (it may still be restoring its own saved frame).
const frameRetryDelay = 200 // ms

// checkFrames reads back the windows just moved. A window that didn't
// end up where it was put (the app moved it itself right after
// creation, AX was busy, or it has a minimum size) gets its frame
// re-sent a bounded number of times; the rest is logged.
func (b *Backend) checkFrames(ps []wm.Placement) {
	retry := false
	for _, p := range ps {
		var f C.wimy_rect
		ok := C.wimy_window_frame(C.uint32_t(p.ID), &f) == 0
		got := wm.Rect{X: int32(f.x), Y: int32(f.y), W: int32(f.w), H: int32(f.h)}
		if ok && sameFrame(got, p.Rect) {
			b.applied.matched(p.ID)
			continue
		}
		if b.applied.mismatch(p.ID) {
			retry = true
			continue
		}
		if ok {
			app := ""
			if w := b.State.Windows[p.ID]; w != nil {
				app = w.AppID
			}
			log.Printf("window %d (%s) is %+v, wanted %+v", p.ID, app, got, p.Rect)
		}
	}
	if retry {
		C.wimy_schedule_apply_after(frameRetryDelay)
	}
}

// syncScreens makes every screen an output, matching screens across
// changes by display ID: new screens are added, renamed ones renamed,
// unplugged ones removed (their views stay, wm collects them when
// empty).
func (b *Backend) syncScreens() {
	var buf [16]C.wimy_screen
	n := int(C.wimy_screens(&buf[0], C.int(len(buf))))
	if n == 0 {
		return // mid-reconfiguration (sleep, display change): keep what we have
	}
	screens := make([]screenInfo, n)
	for i := range screens {
		s := buf[i]
		screens[i] = screenInfo{Frame: frameOf(s.frame), Visible: frameOf(s.visible),
			Display: uint32(s.display), Name: C.GoString(&s.name[0])}
	}
	outs := outputsFor(screens, b.Cfg.BarGap)
	prev := make(map[uint32]string, len(b.outputs))
	for _, o := range b.outputs {
		prev[o.Display] = o.Name
	}
	still := make(map[uint32]bool, len(outs))
	for _, o := range outs {
		still[o.Display] = true
	}
	for _, o := range b.outputs {
		if !still[o.Display] {
			b.State.RemoveOutput(o.Name)
		}
	}
	for _, o := range outs {
		if old, ok := prev[o.Display]; !ok {
			b.State.AddOutput(o.Name)
		} else if old != o.Name {
			b.State.RenameOutput(old, o.Name)
		}
		b.State.SetOutputGeometry(o.Name, o.Full.X, o.Full.Y, o.Full.W, o.Full.H)
		b.State.SetOutputUsable(o.Name, o.Usable.X, o.Usable.Y, o.Usable.W, o.Usable.H)
	}
	b.outputs = outs
}

// screenRects returns the screens' full frames, in NSScreen order.
func (b *Backend) screenRects() []wm.Rect {
	rs := make([]wm.Rect, len(b.outputs))
	for i, o := range b.outputs {
		rs[i] = o.Full
	}
	return rs
}

// viewOn returns the view shown on the named output.
func (b *Backend) viewOn(output string) string {
	for _, o := range b.State.Outputs {
		if o.Name == output {
			return o.View
		}
	}
	return ""
}

// frame reads a window's current frame.
func (b *Backend) frame(id wm.WindowID) (wm.Rect, bool) {
	var f C.wimy_rect
	if C.wimy_window_frame(C.uint32_t(id), &f) != 0 {
		return wm.Rect{}, false
	}
	return wm.Rect{X: int32(f.x), Y: int32(f.y), W: int32(f.w), H: int32(f.h)}, true
}

func (b *Backend) setFrame(id wm.WindowID, r wm.Rect) {
	var perr, serr C.int
	C.wimy_window_set_frame(C.uint32_t(id), C.double(r.X), C.double(r.Y), C.double(r.W), C.double(r.H), &perr, &serr)
}

// hide parks a window in a hide corner. Its last on-screen frame is
// recorded and saved before it moves, so a crash right after can
// still put it back.
func (b *Backend) hide(id wm.WindowID) {
	if _, parked := b.hidden[id]; parked {
		return
	}
	cur, ok := b.frame(id)
	last, shown := b.lastShown[id]
	if !shown {
		if !ok {
			return
		}
		last = cur
	}
	if !ok {
		cur = last
	}
	b.hidden[id] = last
	b.saveHidden()
	b.park(id, last, cur.W, cur.H)
	b.applied.forget(id)
}

// park moves a window to the hide corner of the screen its last
// on-screen frame was on.
func (b *Backend) park(id wm.WindowID, last wm.Rect, w, h int32) {
	if len(b.outputs) == 0 {
		return
	}
	x, y := hidePosition(b.screenRects(), outputAt(b.outputs, last), w, h)
	if st := C.wimy_window_set_position(C.uint32_t(id), C.double(x), C.double(y)); st != 0 {
		log.Printf("window %d: hiding it failed (AXError %d)", id, int(st))
	}
}

// unhideAll puts every parked window back where it was.
func (b *Backend) unhideAll() {
	for id, r := range b.hidden {
		b.setFrame(id, r)
	}
	clear(b.hidden)
	b.saveHidden()
}

// saveHidden persists parked windows plus not-yet-seen ones a previous
// wimy parked.
func (b *Backend) saveHidden() {
	if b.store.path == "" {
		return
	}
	m := make(map[wm.WindowID]wm.Rect, len(b.hidden)+len(b.restore))
	for id, r := range b.restore {
		m[id] = r
	}
	for id, r := range b.hidden {
		m[id] = r
	}
	if err := b.store.save(m); err != nil {
		log.Printf("hidden-window store %s: %v", b.store.path, err)
	}
}

func frameOf(r C.wimy_rect) Frame {
	return Frame{X: float64(r.x), Y: float64(r.y), W: float64(r.w), H: float64(r.h)}
}

//export goScreensChanged
func goScreensChanged() {
	b := current
	defer b.guard()
	b.syncScreens()
	// a changed arrangement can put a parked window on screen
	for id, last := range b.hidden {
		if cur, ok := b.frame(id); ok {
			b.park(id, last, cur.W, cur.H)
		}
	}
	b.markDirty()
}

//export goWindowAdded
func goWindowAdded(wid C.uint32_t, pid C.int, bundle, title, subrole *C.char, hasZoom, minimized C.int) {
	defer current.guard()
	current.windowAdded(wm.WindowID(wid), C.GoString(bundle), C.GoString(title), C.GoString(subrole),
		hasZoom != 0, minimized != 0)
}

// windowAdded brings a window into the model: a new one, one that was
// open at startup, or one back from being minimized or its app hidden.
// Minimized windows stay out until they are restored.
func (b *Backend) windowAdded(id wm.WindowID, bundle, title, subrole string, hasZoom, minimized bool) {
	if minimized || b.known[id] {
		return
	}
	b.known[id] = true
	cur, ok := b.frame(id)
	if r, pending := b.restore[id]; pending {
		b.setFrame(id, r)
		cur, ok = r, true
		delete(b.restore, id)
		b.saveHidden()
		log.Printf("window %d: put back from a hide corner a previous wimy left it in", id)
	} else if ok && len(b.outputs) > 0 && inHideCorner(b.screenRects(), cur) {
		r := centeredIn(b.outputs[0].Usable, cur.W, cur.H)
		b.setFrame(id, r)
		cur = r
		log.Printf("window %d: found in a hide corner, moved on screen", id)
	}
	var tags []string
	if b.startup && ok && len(b.outputs) > 0 {
		// already open: join the view of the screen it is on
		if v := b.viewOn(b.outputs[outputAt(b.outputs, cur)].Name); v != "" {
			tags = []string{v}
		}
	}
	floating := ShouldFloat(subrole, hasZoom)
	b.State.AddWindow(id, floating, tags...)
	b.State.SetAppID(id, bundle)
	b.State.SetTitle(id, title)
	if floating && ok {
		// keep floating windows where the app put them
		b.State.SetFloatRect(id, cur)
	}
	b.markDirty()
}

// dropWindow takes a window out of the model. A parked window that
// still exists (minimized, app hidden) is put back first, so it isn't
// stranded in the corner.
func (b *Backend) dropWindow(id wm.WindowID, exists bool) {
	if !b.known[id] {
		return
	}
	delete(b.known, id)
	if r, parked := b.hidden[id]; parked {
		if exists {
			b.setFrame(id, r)
		}
		delete(b.hidden, id)
		b.saveHidden()
	}
	delete(b.lastShown, id)
	b.applied.forget(id)
	b.State.RemoveWindow(id)
	b.markDirty()
}

//export goWindowRemoved
func goWindowRemoved(wid C.uint32_t) {
	defer current.guard()
	current.dropWindow(wm.WindowID(wid), false)
}

// goWindowGone: the window still exists but leaves the tiling
// (minimized, or its app hidden with Cmd-H).
//
//export goWindowGone
func goWindowGone(wid C.uint32_t) {
	defer current.guard()
	current.dropWindow(wm.WindowID(wid), true)
}

// goFocusChanged: AX or app activation reports a newly focused window
// (a click, Cmd-Tab, the Dock). Echoes of wimy's own focus requests
// are ignored; a window on a view that isn't shown gets its view
// selected (wm.FocusWindow does that).
//
//export goFocusChanged
func goFocusChanged(wid C.uint32_t) {
	b, id := current, wm.WindowID(wid)
	defer b.guard()
	if !b.known[id] || id == b.State.Focused || b.echo.isEcho(id, time.Now()) {
		return
	}
	b.State.FocusWindow(id)
	b.lastFocus = b.State.Focused // already in front: don't activate it again
	b.markDirty()
}

//export goSecureInputTick
func goSecureInputTick() {
	defer current.guard()
	current.checkSecureInput()
}

//export goTitleChanged
func goTitleChanged(wid C.uint32_t, title *C.char) {
	b, id := current, wm.WindowID(wid)
	defer b.guard()
	if b.known[id] {
		b.State.SetTitle(id, C.GoString(title))
		b.markDirty()
	}
}

//export goHotKey
func goHotKey(id C.uint32_t) {
	b := current
	defer b.guard()
	if int(id) < len(b.hotkeys) {
		b.Enqueue(b.hotkeys[id].cmd)
		b.markDirty()
	}
}

// goKeyDown runs on the event tap's thread: it only looks the key up
// and hands the command to the main thread, so the tap never waits on
// AX calls.
//
//export goKeyDown
func goKeyDown(code C.uint16_t, flags C.uint64_t, repeat C.int) C.int {
	b := current
	cmd, run, swallow := b.tap.keyDown(uint16(code), uint64(flags), repeat != 0)
	if run {
		dispatch(func() {
			b.Enqueue(cmd)
			b.markDirty()
		})
	}
	if swallow {
		return 1
	}
	return 0
}

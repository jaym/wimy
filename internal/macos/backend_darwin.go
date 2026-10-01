package macos

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework AppKit -framework ApplicationServices -framework Carbon -framework QuartzCore -framework ServiceManagement
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"context"
	"log"
	"math"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"runtime/cgo"
	"slices"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"wimy/internal/backend"
	"wimy/internal/config"
	"wimy/internal/titlebar"
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
	secureApp string    // its name while it blinds tap-delivered bindings
	applied   *frames
	known     map[wm.WindowID]bool // windows in the model
	outputs   []outputSpec         // screens, in NSScreen order
	scheduled bool                 // an apply pass is queued on the main queue
	lastFocus wm.WindowID
	echo      focusEcho
	startup   bool // windows reported now were already open
	notify    func()

	// Windows of views that aren't shown are parked in a hide corner.
	// hidden maps each window wimy parked to its last on-screen frame,
	// until the window is confirmed back on screen (a show can fail);
	// parked holds the ones sitting in the corner right now. The store
	// persists hidden (together with restore, the parked windows a
	// previous wimy left behind and that haven't been seen yet) so no
	// window is ever lost.
	hidden    map[wm.WindowID]wm.Rect
	parked    map[wm.WindowID]bool
	restore   map[wm.WindowID]wm.Rect
	lastShown map[wm.WindowID]wm.Rect
	store     hiddenStore
	boot      int64 // boot time, tags the store (window IDs restart after a reboot)

	pids map[wm.WindowID]int // owning app of each window in the model

	// macOS tabs: the background tabs wimy set aside (see tabs.go), and a
	// guard against re-entering the tab check from its own callbacks
	tabbed      map[wm.WindowID]bool
	reconciling bool

	// decorations: titlebar renderers by height (titlebar, or stack
	// strip when titlebars are off) and each window's current image
	renderers map[int32]*titlebar.Renderer
	decoKeys  decoCache

	// menu bar item and startup state
	started   bool       // start() ran (Accessibility granted)
	storeOK   bool       // the hidden store was loaded: saving it is safe
	restarted bool       // took over a restart handoff
	login     int        // start-at-login status (loginOff, ...)
	menuTitle string     // what the status item shows, to skip no-op updates
	menu      []menuItem // current menu; goMenuItem indexes it
	menuShown bool

	// away remembers the views (tags) of minimized windows, so a
	// restored window returns to them instead of the focused view.
	away map[wm.WindowID][]string
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
		parked:    make(map[wm.WindowID]bool),
		restore:   make(map[wm.WindowID]wm.Rect),
		lastShown: make(map[wm.WindowID]wm.Rect),
		away:      make(map[wm.WindowID][]string),
		pids:      make(map[wm.WindowID]int),
		tabbed:    make(map[wm.WindowID]bool),
		renderers: make(map[int32]*titlebar.Renderer),
		decoKeys:  decoCache{},
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
	b.secureApp = ""
	if !b.tap.active() {
		return
	}
	if pid != 0 {
		var name [256]C.char
		C.wimy_app_name(C.int(pid), &name[0], C.int(len(name)))
		b.secureApp = C.GoString(&name[0])
		log.Printf("secure input is on (%s, pid %d: a password prompt or Secure Keyboard Entry): "+
			"bindings without Ctrl or Cmd are blocked until it ends", b.secureApp, pid)
	} else {
		log.Printf("secure input is off: all bindings work again")
	}
	b.updateMenu()
}

// Run checks the Accessibility permission, starts tracking windows and
// keys, and runs the AppKit event loop until Quit or Shutdown. It must
// be called from the main goroutine.
func (b *Backend) Run(ctx context.Context) error {
	C.wimy_app_init()
	b.store = hiddenStore{path: filepath.Join(stateDir(os.Getenv, homeDir()), "hidden.json")}
	if tv, err := unix.SysctlTimeval("kern.boottime"); err == nil {
		b.boot = tv.Sec
	}
	// Take a restart handoff over right away: it expires after two
	// minutes, and an update that lost the Accessibility permission may
	// wait longer than that for the user to approve it again.
	b.restarted = b.takeHandoff()
	if C.wimy_ax_trusted(1) != 0 {
		b.start()
	} else {
		// Wait instead of exiting: under launchd an exit would only
		// restart wimy in a loop. The menu bar item says what's missing.
		exe, _ := os.Executable()
		log.Printf("waiting for the Accessibility permission: allow %s (or the terminal that starts it) "+
			"in System Settings → Privacy & Security → Accessibility", exe)
		b.updateMenu()
		C.wimy_start_trust_poll()
	}
	C.wimy_app_run()
	// Quit, Shutdown (SIGINT/SIGTERM): never leave windows parked
	b.unhideAll()
	return nil
}

//export goTrustTick
func goTrustTick() {
	b := current
	defer b.guard()
	if !b.started && C.wimy_ax_trusted(0) != 0 {
		log.Printf("Accessibility permission granted")
		b.start()
	}
}

// start brings wimy up once it may use the Accessibility API.
func (b *Backend) start() {
	b.started = true
	if m, err := b.store.load(b.boot); err != nil {
		log.Printf("hidden-window store %s: %v (ignored)", b.store.path, err)
	} else {
		b.restore = m
	}
	b.storeOK = true
	restarted := b.restarted
	b.syncScreens()
	if restarted {
		// outputs of the old process that no screen has any more
		for _, name := range b.staleOutputs() {
			b.State.RemoveOutput(name)
		}
	}
	b.rebind()
	b.startup = true
	C.wimy_start_tracking()
	b.startup = false
	if restarted {
		// windows that closed while wimy restarted
		for _, id := range b.PruneWindows(func(id wm.WindowID) bool { return b.known[id] }) {
			delete(b.hidden, id)
			delete(b.parked, id)
			delete(b.lastShown, id)
		}
	}
	// windows a previous wimy parked that aren't open now: forget them
	// (apps that come up later and left a window in a corner are still
	// caught by inHideCorner)
	if len(b.restore) > 0 {
		clear(b.restore)
		b.saveHidden()
	}
	if !restarted {
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
	}
	b.lastFocus = b.State.Focused
	C.wimy_start_secure_input_poll()
	if !restarted {
		b.StartAutostart() // after a restart the old children were adopted
	}
	b.applyLogin()
	b.markDirty()
}

// applyLogin installs or removes the login agent per start-at-login
// (only when running from Wimy.app), and removes the SMAppService login
// item older wimys registered (see login.go).
func (b *Backend) applyLogin() {
	if C.wimy_in_app_bundle() == 0 {
		b.login = loginNoBundle
		return
	}
	if os.Getenv("XPC_SERVICE_NAME") != "io.github.jaym.wimy" {
		// unregistering unloads that job: never when it is this process
		switch C.wimy_login_legacy_unregister() {
		case 1:
			log.Printf("start at login: removed the old Login Items entry (now a launchd agent)")
		case -1:
			log.Printf("start at login: could not remove the old Login Items entry; remove Wimy in System Settings → General → Login Items")
		}
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		log.Printf("start at login: %v", err)
		return
	}
	home, _ := os.UserHomeDir()
	b.login, err = setLoginAgent(filepath.Join(home, "Library", "LaunchAgents"), exe, b.Cfg.StartAtLogin)
	if err != nil {
		log.Printf("start at login: %v", err)
	}
}

// updateMenu shows the menu bar item (or removes it, per status-item)
// when what it shows changed.
func (b *Backend) updateMenu() {
	if b.started && !b.Cfg.StatusItem {
		if b.menuShown {
			C.wimy_status_remove()
			b.menuShown, b.menuTitle, b.menu = false, "", nil
		}
		return
	}
	cfgPath := b.ConfigPath()
	title, items := menuFor(b.State, menuStatus{Trusted: b.started, Login: b.login, ConfigPath: cfgPath,
		SecureApp: b.secureApp})
	if b.menuShown && title == b.menuTitle && slices.Equal(items, b.menu) {
		return
	}
	b.menuShown, b.menuTitle, b.menu = true, title, items
	labels := make([]*C.char, len(items))
	flags := make([]C.int, len(items))
	for i, it := range items {
		labels[i] = C.CString(it.Label)
		var f C.int
		if it.Enabled {
			f |= 1
		}
		if it.Checked {
			f |= 2
		}
		if it.Sep {
			f |= 4
		}
		flags[i] = f
	}
	ctitle := C.CString(title)
	C.wimy_status_set(ctitle, C.int(len(items)), &labels[0], &flags[0])
	C.free(unsafe.Pointer(ctitle))
	for _, l := range labels {
		C.free(unsafe.Pointer(l))
	}
}

// goMenuItem: a click in the menu bar item's menu.
//
//export goMenuItem
func goMenuItem(i C.int) {
	b := current
	defer b.guard()
	if int(i) >= len(b.menu) {
		return
	}
	switch cmd := b.menu[i].Cmd; cmd {
	case "":
	case cmdOpenAccessibility:
		C.wimy_open_accessibility_settings()
	default:
		b.Enqueue(cmd)
		b.markDirty()
	}
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
	if ch.Status {
		b.applyLogin()
	}
	if ch.Border || ch.Titlebar {
		// colors, border width or height changed: re-render everything
		clear(b.renderers)
		clear(b.decoKeys)
	}
}

// Kill implements backend.Platform by pressing the window's close
// button.
func (b *Backend) Kill(id wm.WindowID) { C.wimy_window_close(C.uint32_t(id)) }

// Restart implements backend.Platform (see restart_darwin.go).
func (b *Backend) Restart() error { return b.restart() }

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
	if !b.started {
		// waiting for the Accessibility permission: nothing can be
		// moved yet; only quit and restart run, the rest waits for start()
		b.DrainQueueIf(runsBeforeStart)
		b.updateMenu()
		return
	}
	b.DrainQueue()
	var moved []wm.Placement
	start := time.Now()
	placements := b.State.Layout()
	for _, p := range placements {
		// macOS can't clip another app's window: a collapsed stack
		// window is parked like a hidden one and only its strip shows
		if p.Hidden || p.Collapsed {
			b.hide(p.ID)
			continue
		}
		// a parked window being shown keeps its saved frame until
		// checkFrames confirms it left the corner
		delete(b.parked, p.ID)
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
	if len(moved) > 0 {
		elapsed := time.Since(start)
		log.Printf("retile: %d windows in %s (%s per window)", len(moved),
			elapsed.Round(time.Millisecond), (elapsed / time.Duration(len(moved))).Round(time.Millisecond))
		b.checkFrames(moved)
	}

	// decorations go behind their windows, so after the windows moved
	for _, p := range placements {
		b.decorate(p, placements)
	}

	b.checkSecureInput()
	switch f := b.State.Focused; {
	case f != 0 && f != b.lastFocus:
		b.echo.sent(f, b.pids[f], time.Now())
		C.wimy_window_focus(C.uint32_t(f))
		// the app raises its window asynchronously; order the panels
		// again once it has, or they stay under the windows it overlaps
		C.wimy_schedule_apply_after(decoReorderDelay)
	case f == 0 && b.lastFocus != 0:
		// an empty view: the window that had focus is parked (or gone)
		// and must not keep taking keystrokes
		if pid := int(C.wimy_focus_none()); pid != 0 {
			b.echo.sentApp(pid, time.Now())
		}
	}
	b.lastFocus = b.State.Focused
	b.updateMenu()
	if b.notify != nil {
		b.notify()
	}
}

// decoReorderDelay is how long after focusing a window its panels are
// ordered again (ms).
const decoReorderDelay = 120

// cornerRadius is the window corner radius the border fill covers
// (Tahoe windows round their corners by up to about this much); inside
// it the fill leaves the window alone, so translucent windows aren't
// tinted.
const cornerRadius = 26

// decorate shows (or hides) a window's frame panel: titlebar and border
// behind it, or the titlebar strip of a collapsed stack window, ordered
// just above its column's expanded window.
func (b *Backend) decorate(p wm.Placement, all []wm.Placement) {
	id := C.uint32_t(p.ID)
	d, ok := decoFor(p, b.Cfg.Titlebar.Height, b.Cfg.Border.Width)
	if !ok || len(b.outputs) == 0 {
		C.wimy_deco_hide(id)
		return
	}
	col := b.Cfg.Border.Normal
	if p.Focused {
		col = b.Cfg.Border.Focused
	}
	f := fromModel(d.Panel, float64(b.outputs[0].Full.H))
	var above wm.WindowID
	if d.Front {
		above = stripAnchor(all, p)
	}
	c := d.Content
	C.wimy_deco_update(id, C.wimy_rect{x: C.double(f.X), y: C.double(f.Y), w: C.double(f.W), h: C.double(f.H)},
		C.double(d.BarH), C.uint32_t(argb(col)), cbool(d.Fill),
		C.wimy_rect{x: C.double(c.X), y: C.double(c.Y), w: C.double(c.W), h: C.double(c.H)},
		cornerRadius, cbool(d.Front), C.uint32_t(above))
	if d.BarH <= 0 {
		return
	}
	title := ""
	if w := b.State.Windows[p.ID]; w != nil {
		title = w.Title
	}
	scale := b.scaleOf(p.Output)
	if !b.decoKeys.stale(p.ID, decoKey{Title: title, Focused: p.Focused, W: d.Panel.W, H: d.BarH, Scale: scale}) {
		return
	}
	px := b.renderer(d.BarH).Render(d.Panel.W, scale, title, p.Focused)
	C.wimy_deco_image(id, unsafe.Pointer(&px[0]), C.int(max(d.Panel.W, 1)*scale), C.int(d.BarH*scale))
}

// renderer returns the titlebar renderer for bars of height h.
func (b *Backend) renderer(h int32) *titlebar.Renderer {
	r := b.renderers[h]
	if r == nil {
		r = backend.NewTitlebarRenderer(b.Cfg, h)
		b.renderers[h] = r
	}
	return r
}

// scaleOf returns the pixel scale of the named output.
func (b *Backend) scaleOf(output string) int32 {
	for _, o := range b.outputs {
		if o.Name == output {
			return o.Scale
		}
	}
	return 1
}

func argb(c config.Color) uint32 {
	x := c.RGBA()
	return uint32(x.A)<<24 | uint32(x.R)<<16 | uint32(x.G)<<8 | uint32(x.B)
}

func cbool(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

// frameRetryDelay is how long apply waits before re-sending a frame
// an app didn't take (it may still be restoring its own saved frame).
const frameRetryDelay = 200 // ms

// checkFrames reads back the windows just moved. A window that didn't
// end up where it was put (the app moved it itself right after
// creation, AX was busy, or it has a minimum size) gets its frame
// re-sent a bounded number of times; the rest is logged.
func (b *Backend) checkFrames(ps []wm.Placement) {
	retry, confirmed := false, false
	for _, p := range ps {
		var f C.wimy_rect
		ok := C.wimy_window_frame(C.uint32_t(p.ID), &f) == 0
		got := wm.Rect{X: int32(f.x), Y: int32(f.y), W: int32(f.w), H: int32(f.h)}
		if _, saved := b.hidden[p.ID]; saved && !b.parked[p.ID] && backOnScreen(b.screenRects(), got, ok) {
			delete(b.hidden, p.ID)
			confirmed = true
		}
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
	if confirmed {
		b.saveHidden()
	}
	if retry {
		C.wimy_schedule_apply_after(frameRetryDelay)
	}
}

// syncScreens makes every screen an output, matching screens across
// changes by display ID: a connected screen keeps its output name (see
// keepNames), new screens are added, unplugged ones removed (their
// views stay, wm collects them when empty).
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
			Display: uint32(s.display), Name: C.GoString(&s.name[0]), Scale: int32(math.Round(float64(s.scale)))}
	}
	outs := keepNames(b.outputs, outputsFor(screens, b.Cfg.BarGap))
	prev := make(map[uint32]bool, len(b.outputs))
	for _, o := range b.outputs {
		prev[o.Display] = true
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
		if !prev[o.Display] {
			b.State.AddOutput(o.Name)
		}
		b.State.SetOutputGeometry(o.Name, o.Full.X, o.Full.Y, o.Full.W, o.Full.H)
		b.State.SetOutputUsable(o.Name, o.Usable.X, o.Usable.Y, o.Usable.W, o.Usable.H)
	}
	b.outputs = outs
}

// staleOutputs returns model outputs no current screen has.
func (b *Backend) staleOutputs() []string {
	have := make(map[string]bool, len(b.outputs))
	for _, o := range b.outputs {
		have[o.Name] = true
	}
	var out []string
	for _, o := range b.State.Outputs {
		if !have[o.Name] {
			out = append(out, o.Name)
		}
	}
	return out
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
	if b.parked[id] {
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
	b.parked[id] = true
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

// unhideAll puts every parked window back where it was. Before start()
// (no permission yet) it does nothing: nothing was parked by this
// process, and saving would overwrite the store with an empty one.
func (b *Backend) unhideAll() {
	if !b.started {
		return
	}
	for id, r := range b.hidden {
		b.setFrame(id, onScreen(b.outputs, r))
	}
	clear(b.hidden)
	clear(b.parked)
	for id := range b.known {
		C.wimy_deco_hide(C.uint32_t(id))
	}
	b.saveHidden()
}

// saveHidden persists parked windows plus not-yet-seen ones a previous
// wimy parked.
func (b *Backend) saveHidden() {
	if b.store.path == "" || !b.storeOK {
		return
	}
	m := make(map[wm.WindowID]wm.Rect, len(b.hidden)+len(b.restore))
	for id, r := range b.restore {
		m[id] = r
	}
	for id, r := range b.hidden {
		m[id] = r
	}
	if err := b.store.save(m, b.boot); err != nil {
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
	for id := range b.parked {
		last := b.hidden[id]
		if cur, ok := b.frame(id); ok {
			b.park(id, last, cur.W, cur.H)
		}
	}
	b.markDirty()
}

//export goWindowAdded
func goWindowAdded(wid C.uint32_t, pid C.int, bundle, title, subrole *C.char, zoom, resizable, minimized C.int) {
	defer current.guard()
	current.pids[wm.WindowID(wid)] = int(pid)
	current.windowAdded(wm.WindowID(wid), C.GoString(bundle), C.GoString(title), C.GoString(subrole),
		zoomButton(zoom), resizable != 0, minimized != 0)
}

// windowAdded brings a window into the model: a new one, one that was
// open at startup, or one back from being minimized (to the views it
// was on). Minimized windows stay out until they are restored.
func (b *Backend) windowAdded(id wm.WindowID, bundle, title, subrole string, zoom zoomButton, resizable, minimized bool) {
	if minimized || b.known[id] {
		return
	}
	if !b.startup && b.reconcileTabs(b.pids[id], id, 0) {
		// a new or re-shown tab took its tab group's tile
		b.State.SetAppID(id, bundle)
		b.State.SetTitle(id, title)
		b.markDirty()
		return
	}
	delete(b.tabbed, id)
	b.known[id] = true
	if b.State.Windows[id] != nil {
		// taken over from the wimy this one replaced: keep its place
		// (and leave a parked window parked)
		b.State.SetAppID(id, bundle)
		b.State.SetTitle(id, title)
		b.markDirty()
		return
	}
	cur, ok := b.frame(id)
	if r, pending := b.restore[id]; pending {
		r = onScreen(b.outputs, r)
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
	tags := b.away[id]
	delete(b.away, id)
	if tags == nil && b.startup && ok && len(b.outputs) > 0 {
		// already open: join the view of the screen it is on
		if v := b.viewOn(b.outputs[outputAt(b.outputs, cur)].Name); v != "" {
			tags = []string{v}
		}
	}
	floating := ShouldFloat(subrole, zoom, resizable)
	b.State.AddWindow(id, floating, tags...)
	b.State.SetAppID(id, bundle)
	b.State.SetTitle(id, title)
	if floating && ok {
		// keep floating windows where the app put them: wimy's titlebar
		// goes above
		b.State.SetFloatRect(id, floatOuter(cur, b.State.TitlebarHeight))
	}
	b.markDirty()
}

// dropWindow takes a window out of the model. A window that still
// exists (minimized) remembers its views, and if it was parked it is
// put back first, so it isn't stranded in the corner.
func (b *Backend) dropWindow(id wm.WindowID, exists bool) {
	if !b.known[id] {
		return
	}
	delete(b.known, id)
	if w := b.State.Windows[id]; w != nil && exists {
		b.away[id] = w.TagList()
	} else {
		delete(b.away, id)
	}
	if r, parked := b.hidden[id]; parked {
		if exists {
			b.setFrame(id, onScreen(b.outputs, r))
		}
		delete(b.hidden, id)
		b.saveHidden()
	}
	delete(b.parked, id)
	delete(b.lastShown, id)
	C.wimy_deco_destroy(C.uint32_t(id))
	delete(b.decoKeys, id)
	b.applied.forget(id)
	b.State.RemoveWindow(id)
	b.markDirty()
}

//export goWindowRemoved
func goWindowRemoved(wid C.uint32_t) {
	b, id := current, wm.WindowID(wid)
	defer b.guard()
	// a closed tab: its now-visible sibling takes the tile
	if b.known[id] {
		b.reconcileTabs(b.pids[id], 0, id)
	}
	b.dropWindow(id, false)
	delete(b.tabbed, id)
	delete(b.pids, id)
}

// goWindowGone: the window still exists but leaves the tiling
// (minimized).
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
func goFocusChanged(wid C.uint32_t, pid C.int) {
	b, id := current, wm.WindowID(wid)
	defer b.guard()
	b.reconcileTabs(int(pid), 0, 0) // switching tabs focuses the shown tab
	if !b.known[id] {
		return
	}
	if id == b.State.Focused || b.echo.isEcho(id, int(pid), time.Now()) {
		// no focus change, but the app raised its window: order the
		// panels again (a window brought back from the Dock)
		b.markDirty()
		return
	}
	b.State.FocusWindow(id)
	b.lastFocus = b.State.Focused // already in front: don't activate it again
	b.markDirty()
}

// goDecoClicked: a click on a window's titlebar, border or stack strip
// focuses it (a strip expands).
//
//export goDecoClicked
func goDecoClicked(wid C.uint32_t) {
	b, id := current, wm.WindowID(wid)
	defer b.guard()
	if b.known[id] {
		b.State.FocusWindow(id)
		b.markDirty()
	}
}

// goWindowMoved: a window moved or resized itself, or the user dragged
// it. A floating window keeps its new place (its panel follows).
// Tiled windows keep their layout place (mouse handling is Phase 4).
//
//export goWindowMoved
func goWindowMoved(wid C.uint32_t) {
	b, id := current, wm.WindowID(wid)
	defer b.guard()
	if !b.known[id] || b.parked[id] {
		return
	}
	v := b.State.ActiveViewOf(id)
	if v == nil || !v.FloatContains(id) {
		return
	}
	cur, ok := b.frame(id)
	if !ok {
		return
	}
	if outer := floatOuter(cur, b.State.TitlebarHeight); outer != b.State.FloatRectOf(id) {
		b.State.SetFloatRect(id, outer)
		b.markDirty()
	}
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

// timeNow is time.Now, a variable so the handoff clock is explicit.
var timeNow = time.Now

// reconcileTabs keeps one tile per macOS tab group for app pid (see
// tabs.go): a window that joined the app's window list takes the place
// of one that left it, a lone leaver is set aside as a background tab,
// a lone background tab coming back is added again. adding is a window
// being added right now; closed one being removed (it isn't set aside).
// It reports whether adding took a place.
func (b *Backend) reconcileTabs(pid int, adding, closed wm.WindowID) bool {
	if pid <= 0 || b.reconciling {
		return false
	}
	b.reconciling = true
	defer func() { b.reconciling = false }()

	var buf [256]C.uint32_t
	n := int(C.wimy_app_windows(C.int(pid), &buf[0], C.int(len(buf))))
	if n == 0 {
		return false // app not answering (or quitting): don't guess
	}
	listed := make([]wm.WindowID, n)
	for i := range listed {
		listed[i] = wm.WindowID(buf[i])
	}
	var tiled, tabbed []wm.WindowID
	for id, p := range b.pids {
		if p != pid {
			continue
		}
		if b.known[id] {
			tiled = append(tiled, id)
		} else if b.tabbed[id] {
			tabbed = append(tabbed, id)
		}
	}
	pairs, hide, show := tabChanges(tiled, listed, tabbed, adding)
	took := false
	for _, pr := range pairs {
		b.replaceTab(pr[0], pr[1])
		took = took || pr[1] == adding
	}
	for _, id := range hide {
		if id != closed {
			b.dropWindow(id, true) // remembers its views in away
			b.tabbed[id] = true
		}
	}
	for _, id := range show {
		if id != adding {
			delete(b.tabbed, id)
			C.wimy_report_window(C.uint32_t(id)) // re-added with its views
		}
	}
	if len(pairs)+len(hide)+len(show) > 0 {
		b.markDirty()
	}
	return took
}

// replaceTab puts window nu (the now-visible tab) in old's tile.
func (b *Backend) replaceTab(old, nu wm.WindowID) {
	b.State.ReplaceWindow(old, nu)
	delete(b.known, old)
	b.known[nu] = true
	delete(b.tabbed, nu)
	b.tabbed[old] = true
	if r, ok := b.lastShown[old]; ok {
		b.lastShown[nu] = r
	}
	delete(b.lastShown, old)
	b.applied.forget(old)
	b.applied.forget(nu)
	C.wimy_deco_destroy(C.uint32_t(old))
	delete(b.decoKeys, old)
	if b.lastFocus == old {
		b.lastFocus = nu
	}
}

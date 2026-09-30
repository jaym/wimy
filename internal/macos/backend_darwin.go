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
	known     map[wm.WindowID]bool
	output    string // model name of the managed (primary) screen
	usableW   int32  // its usable width in points
	scheduled bool   // an apply pass is queued on the main queue
	lastFocus wm.WindowID
	notify    func()
}

var (
	_ backend.Platform = (*Backend)(nil)
)

// New creates the macOS backend. notify is called on the main thread
// after every apply pass; it must not block.
func New(cfg *config.Config, configArg string, notify func()) *Backend {
	b := &Backend{applied: newFrames(), known: make(map[wm.WindowID]bool), notify: notify}
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
	b.syncScreens()
	b.rebind()
	C.wimy_start_tracking()
	// the windows already open would otherwise all stack in one
	// column (new windows join the focused column, as in wmii)
	for _, o := range b.State.Outputs {
		b.State.SpreadColumns(o.View, spreadColumnCount(b.usableW))
	}
	b.StartAutostart()
	b.markDirty()
	C.wimy_app_run()
	return nil
}

// dispatch runs fn on the main thread's queue. Safe from any
// goroutine; it does not wait.
func dispatch(fn func()) {
	C.wimy_dispatch(C.uintptr_t(cgo.NewHandle(fn)))
}

//export goRunDispatched
func goRunDispatched(h C.uintptr_t) {
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
// titlebars are drawn from Phase 3 on; only bindings apply here.
func (b *Backend) ApplyConfigChange(ch backend.ConfigChange) {
	if ch.Binds || ch.Mod {
		b.rebind()
	}
}

// Kill implements backend.Platform by pressing the window's close
// button.
func (b *Backend) Kill(id wm.WindowID) { C.wimy_window_close(C.uint32_t(id)) }

// Quit implements backend.Platform: wimy exits; windows stay where
// they are.
func (b *Backend) Quit() { C.wimy_app_stop() }

//export goApply
func goApply() { current.apply() }

// apply is one manage pass: run queued commands, lay out, and push
// changed frames and focus to AX.
func (b *Backend) apply() {
	b.scheduled = false
	b.DrainQueue()
	// wimy draws no titlebars on macOS until Phase 3: reserve no space
	b.State.TitlebarHeight = 0

	var moved []wm.Placement
	start := time.Now()
	for _, p := range b.State.Layout() {
		// hiding the windows of other views is Phase 2
		if p.Hidden || !b.applied.changed(p.ID, p.Rect) {
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

	b.checkSecureInput()
	if f := b.State.Focused; f != 0 && f != b.lastFocus {
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

// syncScreens makes the primary screen the model's only output.
// Multiple screens are Phase 2.
func (b *Backend) syncScreens() {
	var buf [16]C.wimy_screen
	n := int(C.wimy_screens(&buf[0], C.int(len(buf))))
	if n == 0 {
		return
	}
	s := buf[0]
	primaryH := float64(s.frame.h)
	name := C.GoString(&s.name[0])
	if name == "" {
		name = fmt.Sprintf("display-%d", uint32(s.display))
	}
	full := toModel(frameOf(s.frame), primaryH)
	usable := toModel(frameOf(s.visible), primaryH)
	switch {
	case b.output == "":
		b.State.AddOutput(name)
	case b.output != name:
		b.State.RenameOutput(b.output, name)
	}
	b.output = name
	b.State.SetOutputGeometry(name, full.X, full.Y, full.W, full.H)
	b.State.SetOutputUsable(name, usable.X, usable.Y, usable.W, usable.H)
	b.usableW = usable.W
}

func frameOf(r C.wimy_rect) Frame {
	return Frame{X: float64(r.x), Y: float64(r.y), W: float64(r.w), H: float64(r.h)}
}

//export goScreensChanged
func goScreensChanged() {
	current.syncScreens()
	current.markDirty()
}

//export goWindowAdded
func goWindowAdded(wid C.uint32_t, pid C.int, bundle, title, subrole *C.char, hasZoom, minimized C.int) {
	current.windowAdded(wm.WindowID(wid), C.GoString(bundle), C.GoString(title), C.GoString(subrole),
		hasZoom != 0, minimized != 0)
}

func (b *Backend) windowAdded(id wm.WindowID, bundle, title, subrole string, hasZoom, minimized bool) {
	// minimized windows are left alone; tracking (de)miniaturize is Phase 2
	if minimized || b.known[id] {
		return
	}
	b.known[id] = true
	floating := ShouldFloat(subrole, hasZoom)
	b.State.AddWindow(id, floating)
	b.State.SetAppID(id, bundle)
	b.State.SetTitle(id, title)
	if floating {
		// keep floating windows where the app put them
		var f C.wimy_rect
		if C.wimy_window_frame(C.uint32_t(id), &f) == 0 {
			b.State.SetFloatRect(id, wm.Rect{X: int32(f.x), Y: int32(f.y), W: int32(f.w), H: int32(f.h)})
		}
	}
	b.markDirty()
}

//export goWindowRemoved
func goWindowRemoved(wid C.uint32_t) {
	b, id := current, wm.WindowID(wid)
	if !b.known[id] {
		return
	}
	delete(b.known, id)
	b.applied.forget(id)
	b.State.RemoveWindow(id)
	b.markDirty()
}

//export goTitleChanged
func goTitleChanged(wid C.uint32_t, title *C.char) {
	b, id := current, wm.WindowID(wid)
	if b.known[id] {
		b.State.SetTitle(id, C.GoString(title))
		b.markDirty()
	}
}

//export goHotKey
func goHotKey(id C.uint32_t) {
	b := current
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

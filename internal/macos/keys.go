package macos

import (
	"sync/atomic"

	"wimy/internal/config"
)

// Carbon modifier bits for RegisterEventHotKey (Events.h: cmdKey,
// shiftKey, optionKey, controlKey).
const (
	carbonCmd     = 1 << 8
	carbonShift   = 1 << 9
	carbonOption  = 1 << 11
	carbonControl = 1 << 12
)

// keycodes maps the keysyms config.parseKeysym produces to macOS
// virtual key codes (Carbon kVK_*). The kVK_ANSI_* codes are physical
// positions on an ANSI keyboard, independent of the active layout: on a
// non-US layout a combo names the key at the US position. That differs
// from Linux, where river matches the active layout's base keysym
// (documented in the README's macOS section).
var keycodes = map[uint32]uint16{
	'a': 0x00, 's': 0x01, 'd': 0x02, 'f': 0x03, 'h': 0x04, 'g': 0x05,
	'z': 0x06, 'x': 0x07, 'c': 0x08, 'v': 0x09, 'b': 0x0B, 'q': 0x0C,
	'w': 0x0D, 'e': 0x0E, 'r': 0x0F, 'y': 0x10, 't': 0x11, 'o': 0x1F,
	'u': 0x20, 'i': 0x22, 'p': 0x23, 'l': 0x25, 'j': 0x26, 'k': 0x28,
	'n': 0x2D, 'm': 0x2E,
	'1': 0x12, '2': 0x13, '3': 0x14, '4': 0x15, '5': 0x17, '6': 0x16,
	'7': 0x1A, '8': 0x1C, '9': 0x19, '0': 0x1D,
	'=': 0x18, '-': 0x1B, ']': 0x1E, '[': 0x21, '\'': 0x27, ';': 0x29,
	'\\': 0x2A, ',': 0x2B, '/': 0x2C, '.': 0x2F, '`': 0x32,
	0x0020: 0x31,                                           // space
	0xff0d: 0x24,                                           // Return
	0xff09: 0x30,                                           // Tab
	0xff08: 0x33,                                           // BackSpace (kVK_Delete)
	0xff1b: 0x35,                                           // Escape
	0xffff: 0x75,                                           // Delete (kVK_ForwardDelete)
	0xff63: 0x72,                                           // Insert (kVK_Help)
	0xff50: 0x73,                                           // Home
	0xff57: 0x77,                                           // End
	0xff55: 0x74,                                           // Prior / Page Up
	0xff56: 0x79,                                           // Next / Page Down
	0xff51: 0x7B,                                           // Left
	0xff53: 0x7C,                                           // Right
	0xff54: 0x7D,                                           // Down
	0xff52: 0x7E,                                           // Up
	0xffbe: 0x7A, 0xffbf: 0x78, 0xffc0: 0x63, 0xffc1: 0x76, // F1-F4
	0xffc2: 0x60, 0xffc3: 0x61, 0xffc4: 0x62, 0xffc5: 0x64, // F5-F8
	0xffc6: 0x65, 0xffc7: 0x6D, 0xffc8: 0x67, 0xffc9: 0x6F, // F9-F12
}

// hotkey is one key binding. Combos with Control or Command are
// registered as Carbon hotkeys; the rest go through the event tap (see
// viaTap).
type hotkey struct {
	combo string // as written in the config, for messages
	code  uint16 // kVK_* virtual key code
	mods  uint32 // Carbon modifier bits
	cmd   string
}

// carbonMods converts a config modifier mask to Carbon modifier bits:
// Option is Mod1 (Alt) and Command is Mod4 (Super). Mod3 and Mod5 have
// no macOS equivalent.
func carbonMods(mods uint32) (uint32, bool) {
	var out uint32
	for _, m := range []struct{ cfg, carbon uint32 }{
		{config.ModShift, carbonShift},
		{config.ModCtrl, carbonControl},
		{config.Mod1, carbonOption},
		{config.Mod4, carbonCmd},
	} {
		if mods&m.cfg != 0 {
			out |= m.carbon
			mods &^= m.cfg
		}
	}
	return out, mods == 0
}

// hotkeysFor resolves config bindings to Carbon hotkeys. Bindings
// whose key has no macOS key code or whose modifiers have no macOS
// equivalent are returned by combo so they can be reported. A combo
// bound twice keeps the later command, in the earlier position: a
// hotkey can only be registered once.
func hotkeysFor(binds []config.Bind) (keys []hotkey, unsupported []string) {
	index := make(map[[2]uint32]int)
	for _, b := range binds {
		code, okCode := keycodes[b.Keysym]
		mods, okMods := carbonMods(b.Mods)
		if !okCode || !okMods {
			unsupported = append(unsupported, b.Combo)
			continue
		}
		k := hotkey{combo: b.Combo, code: code, mods: mods, cmd: b.Command}
		id := [2]uint32{uint32(code), mods}
		if i, dup := index[id]; dup {
			keys[i] = k
			continue
		}
		index[id] = len(keys)
		keys = append(keys, k)
	}
	return keys, unsupported
}

// viaTap reports whether the event tap must deliver this binding
// instead of a Carbon hotkey. On current macOS (15+), RegisterEventHotKey
// never fires for combos whose only modifiers are Option/Shift (they
// type characters, so they are withheld as a keylogging measure), and
// the event tap can see them. But the tap receives nothing while any
// app holds secure input (Terminal's Secure Keyboard Entry, password
// fields), which Carbon hotkeys survive. So: Carbon whenever the combo
// has Control or Command, the tap otherwise.
func (k hotkey) viaTap() bool { return k.mods&(carbonCmd|carbonControl) == 0 }

// CGEventFlags modifier bits (CGEventTypes.h). Caps Lock (1<<16), the
// numeric-pad bit (1<<21, set on arrow keys) and Fn (1<<23, set on
// arrow and F keys) are deliberately not binding modifiers.
const (
	flagShift   = 1 << 17
	flagControl = 1 << 18
	flagOption  = 1 << 19
	flagCommand = 1 << 20
)

// carbonModsFromFlags converts event tap flags to Carbon modifier bits.
func carbonModsFromFlags(flags uint64) uint32 {
	var m uint32
	if flags&flagShift != 0 {
		m |= carbonShift
	}
	if flags&flagControl != 0 {
		m |= carbonControl
	}
	if flags&flagOption != 0 {
		m |= carbonOption
	}
	if flags&flagCommand != 0 {
		m |= carbonCmd
	}
	return m
}

// tapKeys maps the (key code, Carbon modifiers) of tap-delivered
// bindings to their index in the hotkey list.
type tapKeys map[[2]uint32]int

// newTapKeys indexes the bindings the event tap delivers.
func newTapKeys(keys []hotkey) tapKeys {
	t := make(tapKeys)
	for i, k := range keys {
		if k.viaTap() {
			t[[2]uint32{uint32(k.code), k.mods}] = i
		}
	}
	return t
}

// keyDown decides what the event tap does with a key-down: swallow it
// when it is bound, and run the binding (hotkey index id) only on the
// initial press, not on autorepeat. Unbound keys pass through.
func (t tapKeys) keyDown(code uint16, flags uint64, repeat bool) (id int, run, swallow bool) {
	id, ok := t[[2]uint32{uint32(code), carbonModsFromFlags(flags)}]
	if !ok {
		return 0, false, false
	}
	return id, !repeat, true
}

// tapRouter holds the bindings the event tap delivers. The tap runs on
// its own thread (so blocking AX calls on the main thread never stall
// system-wide typing), while reload replaces the bindings on the main
// thread; the table is swapped atomically. The zero value delivers
// nothing.
type tapRouter struct {
	table atomic.Pointer[tapTable]
}

type tapTable struct {
	keys tapKeys
	cmds []string // by hotkey index
}

// set replaces the bindings with the tap-delivered ones among keys.
func (r *tapRouter) set(keys []hotkey) {
	t := &tapTable{keys: newTapKeys(keys), cmds: make([]string, len(keys))}
	for i, k := range keys {
		t.cmds[i] = k.cmd
	}
	r.table.Store(t)
}

// active reports whether any binding goes through the tap.
func (r *tapRouter) active() bool {
	t := r.table.Load()
	return t != nil && len(t.keys) > 0
}

// keyDown is tapKeys.keyDown on the current table, returning the
// command to run. Safe from any thread.
func (r *tapRouter) keyDown(code uint16, flags uint64, repeat bool) (cmd string, run, swallow bool) {
	t := r.table.Load()
	if t == nil {
		return "", false, false
	}
	id, run, swallow := t.keys.keyDown(code, flags, repeat)
	if swallow {
		cmd = t.cmds[id]
	}
	return cmd, run, swallow
}

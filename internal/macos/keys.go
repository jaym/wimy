package macos

import "wimy/internal/config"

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
// positions on an ANSI keyboard, independent of the active layout, so
// combos keep naming the physical key as on Linux.
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

// hotkey is one key binding as registered with RegisterEventHotKey.
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
//
// Carbon hotkeys (rather than a CGEventTap) keep working while another
// app holds secure input (Terminal's Secure Keyboard Entry, password
// fields), fire once per press, and need no permission.
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

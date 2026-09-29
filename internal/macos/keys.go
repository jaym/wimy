package macos

import "wimy/internal/config"

// CGEventFlags modifier bits (CGEventTypes.h). Caps Lock (1<<16), the
// numeric-pad bit (1<<21, set on arrow keys) and Fn (1<<23, set on
// arrow and F keys) are deliberately not modifiers for bindings.
const (
	flagShift   = 1 << 17
	flagControl = 1 << 18
	flagOption  = 1 << 19
	flagCommand = 1 << 20
)

// modsFromFlags converts CGEventFlags to config modifier masks:
// Option is Mod1 (Alt) and Command is Mod4 (Super).
func modsFromFlags(flags uint64) uint32 {
	var m uint32
	if flags&flagShift != 0 {
		m |= config.ModShift
	}
	if flags&flagControl != 0 {
		m |= config.ModCtrl
	}
	if flags&flagOption != 0 {
		m |= config.Mod1
	}
	if flags&flagCommand != 0 {
		m |= config.Mod4
	}
	return m
}

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

// combo is a physical key plus an exact modifier mask.
type combo struct {
	code uint16
	mods uint32
}

// Bindings maps physical key combos to command strings.
type Bindings map[combo]string

// NewBindings resolves config bindings to macOS key codes. Combos
// whose key has no macOS key code are returned so they can be
// reported; they are not bound.
func NewBindings(binds []config.Bind) (Bindings, []string) {
	out := make(Bindings, len(binds))
	var unsupported []string
	for _, b := range binds {
		code, ok := keycodes[b.Keysym]
		if !ok {
			unsupported = append(unsupported, b.Combo)
			continue
		}
		out[combo{code: code, mods: b.Mods}] = b.Command
	}
	return out, unsupported
}

// Match returns the command bound to a key-down with the given
// CGEventFlags. Modifiers must match exactly.
func (b Bindings) Match(code uint16, flags uint64) (string, bool) {
	cmd, ok := b[combo{code: code, mods: modsFromFlags(flags)}]
	return cmd, ok
}

// keyDown decides what the event tap does with a key-down: swallow
// it when it is bound, and run the command only on the initial press,
// not on autorepeat. Unbound keys pass through to the app.
func keyDown(b Bindings, code uint16, flags uint64, repeat bool) (cmd string, run, swallow bool) {
	cmd, ok := b.Match(code, flags)
	if !ok {
		return "", false, false
	}
	return cmd, !repeat, true
}

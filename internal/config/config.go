// Package config loads and validates wimy's KDL configuration file.
//
// Example:
//
//	mod "Mod4"
//	terminal "alacritty"
//	menu "fuzzel"
//	border width=2 focused="#8aadf4" normal="#363a4f"
//	stack-strip 28
//
//	bind "Mod-Return" { spawn "alacritty" }
//	bind "Mod-h" { focus "left" }
//	bind "Mod-Shift-l" { move "right" }
//
//	action "quit" { run "wimyctl quit" }
//	autostart { exec "waybar" }
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sblinch/kdl-go"
	"github.com/sblinch/kdl-go/document"
)

// Color is an RGBA color with 32-bit channels as the river protocol
// expects them (0 .. 0xffffffff, pre-multiplied alpha).
type Color struct{ R, G, B, A uint32 }

// ParseColor parses "#rrggbb" or "#rrggbbaa".
func ParseColor(s string) (Color, error) {
	var c Color
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 && len(s) != 8 {
		return c, fmt.Errorf("invalid color %q: want #rrggbb or #rrggbbaa", s)
	}
	var v [4]uint32
	v[3] = 0xff
	for i := 0; i*2 < len(s); i++ {
		var x uint32
		if _, err := fmt.Sscanf(s[i*2:i*2+2], "%02x", &x); err != nil {
			return c, fmt.Errorf("invalid color %q: %v", s, err)
		}
		v[i] = x
	}
	// scale 8-bit channels to 32-bit
	c = Color{R: v[0] * 0x1010101, G: v[1] * 0x1010101, B: v[2] * 0x1010101, A: v[3] * 0x1010101}
	return c, nil
}

// Border settings.
type Border struct {
	Width   int32
	Focused Color
	Normal  Color
}

// Bind maps a key combination to a command string.
type Bind struct {
	Combo   string // as written in the config, for error messages
	Mods    uint32 // river_seat_v1.modifiers mask
	Keysym  uint32 // xkbcommon keysym
	Command string // e.g. "focus left"
}

// Modifier masks, mirroring river_seat_v1.modifiers.
const (
	ModShift uint32 = 1 << 0
	ModCtrl  uint32 = 1 << 2
	Mod1     uint32 = 1 << 3 // Alt
	Mod3     uint32 = 1 << 4
	Mod4     uint32 = 1 << 5 // Super/Logo
	Mod5     uint32 = 1 << 6
)

// Config is the resolved wimy configuration.
type Config struct {
	Mod        string // name of the primary modifier: Mod1..Mod5 (default Mod4)
	ModMask    uint32 // mask of the primary modifier
	Terminal   string
	Menu       string // dmenu-compatible launcher
	Border     Border
	StackStrip int32
	Binds      []Bind
	Actions    map[string]string // name -> shell command
	Autostart  []string
}

// Default returns the built-in configuration: wmii's key binding set
// with Mod4 as the modifier (wmii used Mod1; set `mod "Mod1"` for the
// classic feel).
func Default() *Config {
	c := &Config{
		Mod:        "Mod4",
		ModMask:    Mod4,
		Terminal:   "alacritty",
		Menu:       "fuzzel --dmenu",
		StackStrip: 28,
		Actions:    map[string]string{},
	}
	c.Border.Width = 2
	c.Border.Focused, _ = ParseColor("#8aadf4")
	c.Border.Normal, _ = ParseColor("#363a4f")

	binds := []struct{ combo, cmd string }{
		{"Mod-Return", "spawn-terminal"},
		{"Mod-p", "spawn-menu"},
		{"Mod-a", "action"},
		{"Mod-Shift-c", "kill"},

		{"Mod-h", "focus left"},
		{"Mod-l", "focus right"},
		{"Mod-j", "focus down"},
		{"Mod-k", "focus up"},
		{"Mod-space", "focus-toggle-layer"},
		{"Mod-t", "view"},
		{"Mod-n", "view-next"},
		{"Mod-b", "view-prev"},

		{"Mod-Shift-h", "move left"},
		{"Mod-Shift-l", "move right"},
		{"Mod-Shift-j", "move down"},
		{"Mod-Shift-k", "move up"},
		{"Mod-Shift-space", "toggle-float"},
		{"Mod-Shift-t", "moveto"},

		{"Mod-d", "mode default"},
		{"Mod-s", "mode stack"},
		{"Mod-m", "mode max"},
	}
	for n := 0; n <= 9; n++ {
		binds = append(binds,
			struct{ combo, cmd string }{fmt.Sprintf("Mod-%d", n), fmt.Sprintf("view-n %d", n)},
			struct{ combo, cmd string }{fmt.Sprintf("Mod-Shift-%d", n), fmt.Sprintf("moveto-n %d", n)},
		)
	}
	for _, b := range binds {
		bind, err := c.parseBind(b.combo, b.cmd)
		if err != nil { // cannot happen with the table above
			panic(err)
		}
		c.Binds = append(c.Binds, bind)
	}
	return c
}

// DefaultPath returns ~/.config/wimy/config.kdl.
func DefaultPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "wimy", "config.kdl")
	}
	return ""
}

// Load reads the config file at path, merging it over the defaults.
// If path is empty the default path is used; a missing file at the
// default path yields the defaults without error.
func Load(path string) (*Config, error) {
	c := Default()
	explicit := path != ""
	if path == "" {
		path = DefaultPath()
		if path == "" {
			return c, nil
		}
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			return c, nil
		}
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	doc, err := kdl.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	c.Binds = nil // user config replaces the default bindings
	for _, n := range doc.Nodes {
		if err := c.applyNode(n); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return c, nil
}

func strArg(n *document.Node, i int) (string, error) {
	if i >= len(n.Arguments) {
		return "", fmt.Errorf("%s: missing argument %d", n.Name, i+1)
	}
	s, ok := n.Arguments[i].Value.(string)
	if !ok {
		return "", fmt.Errorf("%s: argument %d must be a string", n.Name, i+1)
	}
	return s, nil
}

func prop(n *document.Node, name string) (*document.Value, bool) {
	return n.Properties.Get(name)
}

// commandString flattens a command node (e.g. `focus "left"`) into a
// command string ("focus left").
func commandString(n *document.Node) (string, error) {
	parts := []string{n.Name.Value.(string)}
	for _, a := range n.Arguments {
		switch v := a.Value.(type) {
		case string:
			parts = append(parts, v)
		case int64:
			parts = append(parts, fmt.Sprintf("%d", v))
		case float64:
			parts = append(parts, fmt.Sprintf("%v", v))
		default:
			return "", fmt.Errorf("%s: unsupported argument %v", n.Name, a.Value)
		}
	}
	return strings.Join(parts, " "), nil
}

func (c *Config) applyNode(n *document.Node) error {
	name, _ := n.Name.Value.(string)
	switch name {
	case "mod":
		m, err := strArg(n, 0)
		if err != nil {
			return err
		}
		mask, err := parseModName(m)
		if err != nil {
			return err
		}
		c.Mod, c.ModMask = m, mask

	case "terminal":
		t, err := strArg(n, 0)
		if err != nil {
			return err
		}
		c.Terminal = t

	case "menu":
		m, err := strArg(n, 0)
		if err != nil {
			return err
		}
		c.Menu = m

	case "stack-strip":
		if len(n.Arguments) < 1 {
			return fmt.Errorf("stack-strip: missing pixel value")
		}
		v, ok := n.Arguments[0].Value.(int64)
		if !ok || v < 1 {
			return fmt.Errorf("stack-strip: want a positive integer")
		}
		c.StackStrip = int32(v)

	case "border":
		for _, key := range []string{"width", "focused", "normal"} {
			p, ok := prop(n, key)
			if !ok {
				continue
			}
			switch key {
			case "width":
				w, ok := p.Value.(int64)
				if !ok || w < 0 {
					return fmt.Errorf("border width: want a non-negative integer")
				}
				c.Border.Width = int32(w)
			case "focused", "normal":
				s, ok := p.Value.(string)
				if !ok {
					return fmt.Errorf("border %s: want a string", key)
				}
				col, err := ParseColor(s)
				if err != nil {
					return err
				}
				if key == "focused" {
					c.Border.Focused = col
				} else {
					c.Border.Normal = col
				}
			}
		}

	case "bind":
		combo, err := strArg(n, 0)
		if err != nil {
			return err
		}
		if len(n.Children) != 1 {
			return fmt.Errorf("bind %q: want exactly one command child", combo)
		}
		cmd, err := commandString(n.Children[0])
		if err != nil {
			return fmt.Errorf("bind %q: %w", combo, err)
		}
		b, err := c.parseBind(combo, cmd)
		if err != nil {
			return err
		}
		c.Binds = append(c.Binds, b)

	case "action":
		aname, err := strArg(n, 0)
		if err != nil {
			return err
		}
		if len(n.Children) != 1 || n.Children[0].Name.Value != "run" {
			return fmt.Errorf("action %q: want a single `run \"...\"` child", aname)
		}
		cmdline, err := strArg(n.Children[0], 0)
		if err != nil {
			return err
		}
		c.Actions[aname] = cmdline

	case "autostart":
		for _, ch := range n.Children {
			if ch.Name.Value != "exec" {
				return fmt.Errorf("autostart: unexpected %q, want exec", ch.Name)
			}
			cmdline, err := strArg(ch, 0)
			if err != nil {
				return err
			}
			c.Autostart = append(c.Autostart, cmdline)
		}

	default:
		return fmt.Errorf("unknown setting %q", name)
	}
	return nil
}

func parseModName(s string) (uint32, error) {
	switch strings.ToLower(s) {
	case "mod1", "alt":
		return Mod1, nil
	case "mod3":
		return Mod3, nil
	case "mod4", "super", "logo":
		return Mod4, nil
	case "mod5":
		return Mod5, nil
	}
	return 0, fmt.Errorf("unknown modifier %q (want Mod1, Mod3, Mod4 or Mod5)", s)
}

// parseBind parses a key combination like "Mod-Shift-h" plus a command
// string into a Bind. "Mod" refers to the configured primary modifier;
// Shift, Ctrl, Alt and Super are also recognized.
func (c *Config) parseBind(combo, cmd string) (Bind, error) {
	b := Bind{Combo: combo, Command: cmd}
	parts := strings.Split(combo, "-")
	key := parts[len(parts)-1]
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToLower(m) {
		case "mod":
			b.Mods |= c.ModMask
		case "shift":
			b.Mods |= ModShift
		case "ctrl", "control":
			b.Mods |= ModCtrl
		case "alt":
			b.Mods |= Mod1
		case "super", "logo":
			b.Mods |= Mod4
		default:
			return b, fmt.Errorf("bind %q: unknown modifier %q", combo, m)
		}
	}
	sym, err := parseKeysym(key, b.Mods&ModShift != 0)
	if err != nil {
		return b, fmt.Errorf("bind %q: %w", combo, err)
	}
	b.Keysym = sym
	return b, nil
}

// namedKeysyms maps key names to xkbcommon keysyms.
var namedKeysyms = map[string]uint32{
	"return": 0xff0d, "enter": 0xff0d,
	"escape": 0xff1b, "esc": 0xff1b,
	"space":     0x0020,
	"tab":       0xff09,
	"backspace": 0xff08,
	"delete":    0xffff, "del": 0xffff,
	"insert": 0xff63, "ins": 0xff63,
	"home": 0xff50, "end": 0xff57,
	"prior": 0xff55, "page_up": 0xff55, "pageup": 0xff55,
	"next": 0xff56, "page_down": 0xff56, "pagedown": 0xff56,
	"left": 0xff51, "up": 0xff52, "right": 0xff53, "down": 0xff54,
	"f1": 0xffbe, "f2": 0xffbf, "f3": 0xffc0, "f4": 0xffc1,
	"f5": 0xffc2, "f6": 0xffc3, "f7": 0xffc4, "f8": 0xffc5,
	"f9": 0xffc6, "f10": 0xffc7, "f11": 0xffc8, "f12": 0xffc9,
}

// parseKeysym resolves a key token to a keysym. Single characters map
// to their Latin-1 keysym; if Shift is held and the character is a
// lowercase ASCII letter, the uppercase keysym is used (pressing
// Shift+h produces the keysym H).
func parseKeysym(key string, shifted bool) (uint32, error) {
	if sym, ok := namedKeysyms[strings.ToLower(key)]; ok {
		return sym, nil
	}
	runes := []rune(key)
	if len(runes) == 1 {
		r := runes[0]
		if shifted && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		if r > 0xff {
			return 0, fmt.Errorf("key %q is outside the Latin-1 range", key)
		}
		return uint32(r), nil
	}
	return 0, fmt.Errorf("unknown key %q", key)
}

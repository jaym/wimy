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
	"image/color"
	"os"
	"path/filepath"
	"strings"

	"github.com/sblinch/kdl-go"
	"github.com/sblinch/kdl-go/document"
)

// Color is an RGBA color with 32-bit channels as the river protocol
// expects them (0 .. 0xffffffff, pre-multiplied alpha).
type Color struct{ R, G, B, A uint32 }

// RGBA converts the color to 8 bits per channel.
func (c Color) RGBA() color.RGBA {
	return color.RGBA{R: uint8(c.R >> 24), G: uint8(c.G >> 24), B: uint8(c.B >> 24), A: uint8(c.A >> 24)}
}

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

// Titlebar settings. Height 0 disables titlebars (dwm-style: border
// only).
type Titlebar struct {
	Height    int32
	FocusedBg Color
	FocusedFg Color
	NormalBg  Color
	NormalFg  Color
}

// Bind maps a key combination to a command string.
type Bind struct {
	Combo   string // as written in the config, for error messages
	Mods    uint32 // river_seat_v1.modifiers mask
	Keysym  uint32 // xkbcommon keysym
	Command string // e.g. "focus left"
}

// Modifier masks, mirroring the river_seat_v1.modifiers enum.
// Note the gaps: 2 is capslock and 16 is numlock, which river and
// wlroots use internally but are not part of the enum.
const (
	ModShift uint32 = 1
	ModCtrl  uint32 = 4
	Mod1     uint32 = 8 // Alt
	Mod3     uint32 = 32
	Mod4     uint32 = 64 // Super/Logo
	Mod5     uint32 = 128
)

// osDefaults are the defaults that differ between operating systems
// (defaults_darwin.go, defaults_other.go).
type osDefaults struct {
	mod      string
	modMask  uint32
	terminal string
	launcher string
	menu     string
	macOS    bool // status-item and start-at-login default to on
}

// Config is the resolved wimy configuration.
type Config struct {
	Mod      string // name of the primary modifier: Mod1..Mod5 (default Mod4; Option on macOS)
	ModMask  uint32 // mask of the primary modifier
	Terminal string
	Launcher string // program launcher (Mod-p)
	Menu     string // dmenu-compatible prompter (tag/action prompts)
	// FocusFollowsMouse makes the pointer focus windows it enters
	// (sloppy focus); when false only clicks focus.
	FocusFollowsMouse bool
	Border            Border
	Titlebar          Titlebar
	StackStrip        int32
	// BarGap is the space in points reserved at the top of every screen
	// for a bar the window manager doesn't know about (SketchyBar).
	// macOS only: on Linux bars reserve space through the layer shell.
	BarGap int32
	// StatusItem shows wimy's menu bar item (macOS only).
	StatusItem bool
	// StartAtLogin registers Wimy.app as a login item (macOS only).
	StartAtLogin bool
	Binds        []Bind
	Actions      map[string]string // name -> shell command
	Autostart    []string
}

// Default returns the built-in configuration: wmii's key binding set
// with Mod4 as the modifier on Linux (wmii used Mod1; set `mod "Mod1"`
// for the classic feel) and Option on macOS.
func Default() *Config {
	c := &Config{
		Mod:               platformDefaults.mod,
		ModMask:           platformDefaults.modMask,
		Terminal:          platformDefaults.terminal,
		FocusFollowsMouse: true,
		Launcher:          platformDefaults.launcher,
		Menu:              platformDefaults.menu,
		StatusItem:        platformDefaults.macOS,
		StartAtLogin:      platformDefaults.macOS,
		StackStrip:        28,
		Actions: map[string]string{
			"quit": "wimyctl quit",
		},
	}
	c.Border.Width = 2
	c.Border.Focused, _ = ParseColor("#8aadf4")
	c.Border.Normal, _ = ParseColor("#363a4f")
	c.Titlebar.Height = 22
	c.Titlebar.FocusedBg, _ = ParseColor("#8aadf4")
	c.Titlebar.FocusedFg, _ = ParseColor("#1e2030")
	c.Titlebar.NormalBg, _ = ParseColor("#24273a")
	c.Titlebar.NormalFg, _ = ParseColor("#a5adcb")

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
		{"Mod-f", "fullscreen"},

		{"Mod-Ctrl-h", "grow left"},
		{"Mod-Ctrl-l", "grow right"},
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

// DefaultPath returns the config file path used when -config is not
// given (see defaultPath).
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return defaultPath(os.Getenv, home)
}

// defaultPath is $XDG_CONFIG_HOME/wimy/config.kdl, else
// ~/.config/wimy/config.kdl — on macOS too, rather than
// ~/Library/Application Support, so dotfile managers put it in the
// same place on both systems. A relative XDG_CONFIG_HOME is ignored,
// as the XDG spec requires.
func defaultPath(getenv func(string) string, home string) string {
	if dir := getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "wimy", "config.kdl")
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "wimy", "config.kdl")
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
	// The default bindings are replaced only if the config declares
	// at least one bind of its own; a config that only sets options
	// keeps the defaults.
	hasBinds := false
	for _, n := range doc.Nodes {
		if name, _ := n.Name.Value.(string); name == "bind" {
			hasBinds = true
			break
		}
	}
	if hasBinds {
		c.Binds = nil
	}
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

	case "launcher":
		l, err := strArg(n, 0)
		if err != nil {
			return err
		}
		c.Launcher = l

	case "menu":
		m, err := strArg(n, 0)
		if err != nil {
			return err
		}
		c.Menu = m

	case "status-item", "start-at-login":
		if len(n.Arguments) < 1 {
			return fmt.Errorf("%s: missing true/false", name)
		}
		v, ok := n.Arguments[0].Value.(bool)
		if !ok {
			return fmt.Errorf("%s: want true or false", name)
		}
		if name == "status-item" {
			c.StatusItem = v
		} else {
			c.StartAtLogin = v
		}

	case "focus-follows-mouse":
		if len(n.Arguments) < 1 {
			return fmt.Errorf("focus-follows-mouse: missing true/false")
		}
		v, ok := n.Arguments[0].Value.(bool)
		if !ok {
			return fmt.Errorf("focus-follows-mouse: want true or false")
		}
		c.FocusFollowsMouse = v

	case "bar-gap":
		if len(n.Arguments) < 1 {
			return fmt.Errorf("bar-gap: missing point value")
		}
		v, ok := n.Arguments[0].Value.(int64)
		if !ok || v < 0 {
			return fmt.Errorf("bar-gap: want a non-negative integer")
		}
		c.BarGap = int32(v)

	case "stack-strip":
		if len(n.Arguments) < 1 {
			return fmt.Errorf("stack-strip: missing pixel value")
		}
		v, ok := n.Arguments[0].Value.(int64)
		if !ok || v < 1 {
			return fmt.Errorf("stack-strip: want a positive integer")
		}
		c.StackStrip = int32(v)

	case "titlebar":
		// `titlebar "off"` disables titlebars entirely (so does
		// height=0). Note: the string must be quoted — bare `off` is
		// not a valid KDL value.
		if len(n.Arguments) >= 1 {
			if s, ok := n.Arguments[0].Value.(string); ok && s == "off" {
				c.Titlebar.Height = 0
				return nil
			}
			return fmt.Errorf("titlebar: unexpected argument (want `off` or properties)")
		}
		colors := map[string]*Color{
			"focused-bg": &c.Titlebar.FocusedBg,
			"focused-fg": &c.Titlebar.FocusedFg,
			"normal-bg":  &c.Titlebar.NormalBg,
			"normal-fg":  &c.Titlebar.NormalFg,
		}
		for key, dst := range colors {
			if p, ok := prop(n, key); ok {
				s, ok := p.Value.(string)
				if !ok {
					return fmt.Errorf("titlebar %s: want a string", key)
				}
				col, err := ParseColor(s)
				if err != nil {
					return err
				}
				*dst = col
			}
		}
		if p, ok := prop(n, "height"); ok {
			h, ok := p.Value.(int64)
			if !ok || h < 0 {
				return fmt.Errorf("titlebar height: want a non-negative integer")
			}
			c.Titlebar.Height = int32(h)
		}

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
		// actions merge with (and can override) the defaults
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
	case "mod1", "alt", "option", "opt":
		return Mod1, nil
	case "mod3":
		return Mod3, nil
	case "mod4", "super", "logo", "cmd", "command":
		return Mod4, nil
	case "mod5":
		return Mod5, nil
	}
	// Compound modifiers (Ctrl-Option, Hyper) are deliberately not
	// supported: bindings name extra modifiers explicitly instead.
	return 0, fmt.Errorf("mod %q: must be a single modifier (Mod1, Mod3, Mod4, Mod5, Alt/Option or Super/Cmd); "+
		"combinations such as Ctrl-Option are not supported", s)
}

// parseBind parses a key combination like "Mod-Shift-h" plus a command
// string into a Bind. "Mod" refers to the configured primary modifier;
// Shift, Ctrl, Alt/Option and Super/Cmd are also recognized.
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
		case "alt", "option", "opt":
			b.Mods |= Mod1
		case "super", "logo", "cmd", "command":
			b.Mods |= Mod4
		default:
			return b, fmt.Errorf("bind %q: unknown modifier %q", combo, m)
		}
	}
	sym, err := parseKeysym(key)
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

// parseKeysym resolves a key token to a keysym. Single characters
// map to their literal Latin-1 keysym: combos name the PHYSICAL key,
// not the shifted symbol — river matches bindings against the
// base-layer keysym plus the full modifier mask, so "Mod-Shift-c"
// must use keysym 'c', not 'C'.
func parseKeysym(key string) (uint32, error) {
	if sym, ok := namedKeysyms[strings.ToLower(key)]; ok {
		return sym, nil
	}
	runes := []rune(key)
	if len(runes) == 1 {
		r := runes[0]
		if r > 0xff {
			return 0, fmt.Errorf("key %q is outside the Latin-1 range", key)
		}
		return uint32(r), nil
	}
	return 0, fmt.Errorf("unknown key %q", key)
}

package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.kdl")
	err := os.WriteFile(path, []byte(`
mod "Mod1"
terminal "foot"
menu "bemenu-run"
stack-strip 32
focus-follows-mouse false
border width=3 focused="#ff0000" normal="#00ff0080"

bind "Mod-Return" { spawn "foot"; }
bind "Mod-Shift-q" { kill; }
bind "Super-x" { focus "left"; }

action "lock" { run "swaylock"; }
autostart {
	exec "waybar"
	exec "mako"
}
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Mod != "Mod1" || c.ModMask != Mod1 {
		t.Errorf("mod: got %q/%d", c.Mod, c.ModMask)
	}
	if c.Terminal != "foot" || c.Menu != "bemenu-run" {
		t.Errorf("terminal/menu: %q %q", c.Terminal, c.Menu)
	}
	if c.StackStrip != 32 {
		t.Errorf("stack-strip: %d", c.StackStrip)
	}
	if c.FocusFollowsMouse {
		t.Errorf("focus-follows-mouse should be false")
	}
	if c.Border.Width != 3 {
		t.Errorf("border width: %d", c.Border.Width)
	}
	if c.Border.Focused.R != 0xffffffff || c.Border.Focused.A != 0xffffffff {
		t.Errorf("focused color: %+v", c.Border.Focused)
	}
	if c.Border.Normal.G != 0xffffffff || c.Border.Normal.A != 0x80808080 {
		t.Errorf("normal color: %+v", c.Border.Normal)
	}
	if len(c.Binds) != 3 {
		t.Fatalf("binds: %d", len(c.Binds))
	}
	// Mod-Return with Mod1 primary
	if c.Binds[0].Mods != Mod1 || c.Binds[0].Keysym != 0xff0d || c.Binds[0].Command != "spawn foot" {
		t.Errorf("bind 0: %+v", c.Binds[0])
	}
	// Shift combo uses the physical (lowercase) keysym
	if c.Binds[1].Keysym != 'q' || c.Binds[1].Mods != Mod1|ModShift {
		t.Errorf("bind 1: %+v", c.Binds[1])
	}
	// explicit Super
	if c.Binds[2].Mods != Mod4 || c.Binds[2].Keysym != 'x' {
		t.Errorf("bind 2: %+v", c.Binds[2])
	}
	if c.Actions["lock"] != "swaylock" {
		t.Errorf("action: %v", c.Actions)
	}
	if len(c.Autostart) != 2 || c.Autostart[0] != "waybar" {
		t.Errorf("autostart: %v", c.Autostart)
	}
}

// TestTitlebarOff guards the quoted `titlebar "off"` form (bare
// `off` is not a valid KDL value and must stay an error) and the
// height=0 equivalent.
func TestTitlebarOff(t *testing.T) {
	load := func(text string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "config.kdl")
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return Load(p)
	}
	for _, form := range []string{`titlebar "off"`, "titlebar height=0"} {
		c, err := load(form + "\n")
		if err != nil {
			t.Fatalf("%s: %v", form, err)
		}
		if c.Titlebar.Height != 0 {
			t.Errorf("%s: height %d, want 0", form, c.Titlebar.Height)
		}
	}
	if _, err := load("titlebar off\n"); err == nil {
		t.Error("bare `titlebar off` should not parse")
	}
}

func TestLoadMissingDefaultPathOK(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nonexistent.kdl"))
	if err == nil {
		t.Fatalf("explicit missing path should error")
	}
	_ = c
}

func TestDefaultsHaveWmiiBindings(t *testing.T) {
	c := Default()
	if !c.FocusFollowsMouse {
		t.Errorf("default focus-follows-mouse should be true")
	}
	want := map[string]bool{
		"Mod-Return": false, "Mod-p": false, "Mod-a": false, "Mod-Shift-c": false,
		"Mod-h": false, "Mod-l": false, "Mod-j": false, "Mod-k": false,
		"Mod-space": false, "Mod-t": false, "Mod-n": false, "Mod-b": false,
		"Mod-Shift-h": false, "Mod-Shift-l": false, "Mod-Shift-j": false, "Mod-Shift-k": false,
		"Mod-Shift-space": false, "Mod-Shift-t": false,
		"Mod-d": false, "Mod-s": false, "Mod-m": false,
		"Mod-1": false, "Mod-Shift-1": false, "Mod-0": false, "Mod-Shift-0": false,
	}
	for _, b := range c.Binds {
		if _, ok := want[b.Combo]; ok {
			want[b.Combo] = true
		}
	}
	for combo, seen := range want {
		if !seen {
			t.Errorf("missing default binding %q", combo)
		}
	}
	// Mod-Shift-h must use the physical keysym h with Mod|Shift
	for _, b := range c.Binds {
		if b.Combo == "Mod-Shift-h" {
			if b.Keysym != 'h' || b.Mods != c.ModMask|ModShift {
				t.Errorf("Mod-Shift-h: keysym=%x mods=%x", b.Keysym, b.Mods)
			}
		}
	}
}

func TestUnknownSettingFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.kdl")
	_ = os.WriteFile(path, []byte("bogus 1\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Fatalf("unknown setting should error")
	}
}

func TestPlatformDefaultMod(t *testing.T) {
	c := Default()
	want, wantMask := "Mod4", Mod4
	if runtime.GOOS == "darwin" {
		want, wantMask = "Mod1", Mod1 // Option
	}
	if c.Mod != want || c.ModMask != wantMask {
		t.Errorf("default mod = %q/%d, want %q/%d", c.Mod, c.ModMask, want, wantMask)
	}
}

func TestMacModifierNames(t *testing.T) {
	for name, mask := range map[string]uint32{"Option": Mod1, "opt": Mod1, "Cmd": Mod4, "command": Mod4} {
		got, err := parseModName(name)
		if err != nil || got != mask {
			t.Errorf("parseModName(%q) = %d, %v; want %d", name, got, err, mask)
		}
	}
	c := Default()
	b, err := c.parseBind("Cmd-Option-h", "focus left")
	if err != nil {
		t.Fatal(err)
	}
	if b.Mods != Mod4|Mod1 || b.Keysym != 'h' {
		t.Errorf("Cmd-Option-h: mods=%d keysym=%x", b.Mods, b.Keysym)
	}
}

func TestDefaultPathXDG(t *testing.T) {
	env := func(kv map[string]string) func(string) string {
		return func(k string) string { return kv[k] }
	}
	cases := []struct {
		name string
		env  map[string]string
		home string
		want string
	}{
		{"XDG_CONFIG_HOME wins", map[string]string{"XDG_CONFIG_HOME": "/x"}, "/Users/me", "/x/wimy/config.kdl"},
		{"~/.config otherwise (also on macOS)", map[string]string{}, "/Users/me", "/Users/me/.config/wimy/config.kdl"},
		{"relative XDG_CONFIG_HOME is ignored", map[string]string{"XDG_CONFIG_HOME": "rel"}, "/home/me", "/home/me/.config/wimy/config.kdl"},
		{"no home", map[string]string{}, "", ""},
	}
	for _, c := range cases {
		if got := defaultPath(env(c.env), c.home); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCompoundModUnsupported(t *testing.T) {
	for _, mod := range []string{"Ctrl-Option", "Option+Ctrl", "Hyper"} {
		_, err := parseModName(mod)
		if err == nil || !strings.Contains(err.Error(), "single modifier") {
			t.Errorf("mod %q: err = %v, want a 'single modifier' error", mod, err)
		}
	}
}

func TestBarGap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.kdl")
	if err := os.WriteFile(path, []byte("bar-gap 37\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil || c.BarGap != 37 {
		t.Fatalf("bar-gap: %d, %v", c.BarGap, err)
	}
	if Default().BarGap != 0 {
		t.Errorf("default bar-gap %d, want 0", Default().BarGap)
	}
	if err := os.WriteFile(path, []byte("bar-gap -1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Errorf("negative bar-gap accepted")
	}
}

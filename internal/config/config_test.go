package config

import (
	"os"
	"path/filepath"
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
	// Shift lowercase letter -> uppercase keysym
	if c.Binds[1].Keysym != 'Q' || c.Binds[1].Mods != Mod1|ModShift {
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

func TestLoadMissingDefaultPathOK(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nonexistent.kdl"))
	if err == nil {
		t.Fatalf("explicit missing path should error")
	}
	_ = c
}

func TestDefaultsHaveWmiiBindings(t *testing.T) {
	c := Default()
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
	// Mod-Shift-h must produce uppercase H with Mod4|Shift
	for _, b := range c.Binds {
		if b.Combo == "Mod-Shift-h" {
			if b.Keysym != 'H' || b.Mods != Mod4|ModShift {
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

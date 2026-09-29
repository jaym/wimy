package macos

import (
	"slices"
	"testing"

	"wimy/internal/config"
)

// CGEventFlags bits, as delivered by the event tap.
const (
	tShift   = 1 << 17
	tControl = 1 << 18
	tOption  = 1 << 19
	tCommand = 1 << 20
	tNumpad  = 1 << 21
	tFn      = 1 << 23
	tCaps    = 1 << 16
)

func binds(t *testing.T, cfgSrc map[string]string) []config.Bind {
	t.Helper()
	var out []config.Bind
	for combo, cmd := range cfgSrc {
		b, err := config.ParseBind("Mod1", combo, cmd)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func TestModsFromFlags(t *testing.T) {
	got := modsFromFlags(tShift | tOption | tCaps | tFn | tNumpad)
	if got != config.ModShift|config.Mod1 {
		t.Errorf("modsFromFlags = %d, want Shift|Mod1", got)
	}
	if got := modsFromFlags(tControl | tCommand); got != config.ModCtrl|config.Mod4 {
		t.Errorf("ctrl|cmd = %d", got)
	}
}

func TestMatchExactModifiers(t *testing.T) {
	b, bad := NewBindings(binds(t, map[string]string{"Mod-h": "focus left", "Mod-Shift-h": "move left"}))
	if len(bad) != 0 {
		t.Fatalf("unsupported: %v", bad)
	}
	if cmd, ok := b.Match(0x04, tOption); !ok || cmd != "focus left" {
		t.Errorf("Option-h = %q %v", cmd, ok)
	}
	if cmd, ok := b.Match(0x04, tOption|tShift); !ok || cmd != "move left" {
		t.Errorf("Option-Shift-h = %q %v", cmd, ok)
	}
	if _, ok := b.Match(0x04, tOption|tCommand); ok {
		t.Errorf("Option-Cmd-h matched; modifiers must match exactly")
	}
}

func TestMatchIgnoresFnAndNumpad(t *testing.T) {
	b, _ := NewBindings(binds(t, map[string]string{"Mod-Left": "focus left", "Mod-F1": "view 1"}))
	if cmd, ok := b.Match(0x7B, tOption|tFn|tNumpad); !ok || cmd != "focus left" {
		t.Errorf("Option-Left with fn/numpad flags = %q %v", cmd, ok)
	}
	if _, ok := b.Match(0x7A, tOption|tFn); !ok {
		t.Errorf("Option-F1 with fn flag did not match")
	}
}

func TestMatchUnboundPassesThrough(t *testing.T) {
	b, _ := NewBindings(binds(t, map[string]string{"Mod-h": "focus left"}))
	if _, ok := b.Match(0x0E, tOption); ok { // Option-e types an accent
		t.Errorf("unbound Option-e was matched")
	}
}

func TestKeyRepeatSwallowedNotRun(t *testing.T) {
	b, _ := NewBindings(binds(t, map[string]string{"Mod-h": "focus left"}))
	if cmd, run, swallow := keyDown(b, 0x04, tOption, false); !run || !swallow || cmd != "focus left" {
		t.Errorf("first press: cmd=%q run=%v swallow=%v", cmd, run, swallow)
	}
	if _, run, swallow := keyDown(b, 0x04, tOption, true); run || !swallow {
		t.Errorf("autorepeat: run=%v swallow=%v, want false true", run, swallow)
	}
	if _, run, swallow := keyDown(b, 0x0E, tOption, false); run || swallow {
		t.Errorf("unbound key: run=%v swallow=%v, want false false", run, swallow)
	}
}

func TestNewBindingsReportsUnsupported(t *testing.T) {
	bs := binds(t, map[string]string{"Mod-h": "focus left"})
	bs = append(bs, config.Bind{Combo: "Mod-ä", Mods: config.Mod1, Keysym: 0xe4, Command: "x"})
	b, bad := NewBindings(bs)
	if !slices.Equal(bad, []string{"Mod-ä"}) {
		t.Errorf("unsupported = %v", bad)
	}
	if len(b) != 1 {
		t.Errorf("%d bindings, want 1", len(b))
	}
}

func TestDefaultBindingsAllMap(t *testing.T) {
	_, bad := NewBindings(config.Default().Binds)
	if len(bad) != 0 {
		t.Errorf("default bindings without a macOS keycode: %v", bad)
	}
}

package macos

import (
	"slices"
	"testing"

	"wimy/internal/config"
)

// binds parses combos with Option (Mod1) as the primary modifier.
func binds(t *testing.T, pairs ...string) []config.Bind {
	t.Helper()
	var out []config.Bind
	for i := 0; i+1 < len(pairs); i += 2 {
		b, err := config.ParseBind("Mod1", pairs[i], pairs[i+1])
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func TestHotkeysForCarbonModifiers(t *testing.T) {
	keys, bad := hotkeysFor(binds(t,
		"Mod-h", "focus left",
		"Mod-Shift-h", "move left",
		"Cmd-Ctrl-x", "kill",
	))
	if len(bad) != 0 {
		t.Fatalf("unsupported: %v", bad)
	}
	want := []hotkey{
		{combo: "Mod-h", code: 0x04, mods: carbonOption, cmd: "focus left"},
		{combo: "Mod-Shift-h", code: 0x04, mods: carbonOption | carbonShift, cmd: "move left"},
		{combo: "Cmd-Ctrl-x", code: 0x07, mods: carbonCmd | carbonControl, cmd: "kill"},
	}
	if !slices.Equal(keys, want) {
		t.Errorf("hotkeys =\n%+v\nwant\n%+v", keys, want)
	}
}

func TestHotkeysForArrowAndFKeys(t *testing.T) {
	keys, bad := hotkeysFor(binds(t, "Mod-Left", "focus left", "Mod-F1", "view 1"))
	if len(bad) != 0 || len(keys) != 2 || keys[0].code != 0x7B || keys[1].code != 0x7A {
		t.Errorf("keys=%+v unsupported=%v", keys, bad)
	}
}

func TestHotkeysForReportsUnsupported(t *testing.T) {
	bs := binds(t, "Mod-h", "focus left")
	bs = append(bs,
		config.Bind{Combo: "Mod-ä", Mods: config.Mod1, Keysym: 0xe4, Command: "x"}, // no key code
		config.Bind{Combo: "Mod3-x", Mods: config.Mod3, Keysym: 'x', Command: "y"}, // no macOS modifier
	)
	keys, bad := hotkeysFor(bs)
	if !slices.Equal(bad, []string{"Mod-ä", "Mod3-x"}) {
		t.Errorf("unsupported = %v", bad)
	}
	if len(keys) != 1 {
		t.Errorf("%d hotkeys, want 1", len(keys))
	}
}

func TestHotkeysForDuplicateComboLastWins(t *testing.T) {
	keys, _ := hotkeysFor(binds(t, "Mod-h", "focus left", "Mod-j", "focus down", "Mod-h", "view 1"))
	if len(keys) != 2 {
		t.Fatalf("%d hotkeys, want 2 (a combo can be registered once)", len(keys))
	}
	if keys[0].combo != "Mod-h" || keys[0].cmd != "view 1" {
		t.Errorf("duplicate combo: got %+v, want the later command in the first position", keys[0])
	}
}

func TestDefaultBindingsAllMap(t *testing.T) {
	_, bad := hotkeysFor(config.Default().Binds)
	if len(bad) != 0 {
		t.Errorf("default bindings without a macOS hotkey: %v", bad)
	}
}

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

func TestViaTapOnlyWithoutCtrlOrCmd(t *testing.T) {
	keys, _ := hotkeysFor(binds(t,
		"Mod-h", "a", "Mod-Shift-h", "b", "Ctrl-Option-h", "c", "Cmd-Option-h", "d", "Ctrl-x", "e",
	))
	want := []bool{true, true, false, false, false}
	for i, k := range keys {
		if k.viaTap() != want[i] {
			t.Errorf("%s: viaTap = %v, want %v", k.combo, k.viaTap(), want[i])
		}
	}
}

func TestCarbonModsFromFlagsIgnoresCapsFnNumpad(t *testing.T) {
	if got := carbonModsFromFlags(tShift | tOption | tCaps | tFn | tNumpad); got != carbonShift|carbonOption {
		t.Errorf("got %#x, want shift|option", got)
	}
	if got := carbonModsFromFlags(tControl | tCommand); got != carbonControl|carbonCmd {
		t.Errorf("got %#x, want control|cmd", got)
	}
}

func TestTapKeysMatchExactly(t *testing.T) {
	keys, _ := hotkeysFor(binds(t, "Mod-h", "focus left", "Mod-Shift-h", "move left", "Mod-Left", "focus left"))
	tk := newTapKeys(keys)
	if id, run, swallow := tk.keyDown(0x04, tOption, false); !run || !swallow || keys[id].cmd != "focus left" {
		t.Errorf("Option-h: id=%d run=%v swallow=%v", id, run, swallow)
	}
	if id, _, _ := tk.keyDown(0x04, tOption|tShift, false); keys[id].cmd != "move left" {
		t.Errorf("Option-Shift-h matched %q", keys[id].cmd)
	}
	if _, run, swallow := tk.keyDown(0x04, tOption|tCommand, false); run || swallow {
		t.Errorf("Option-Cmd-h matched; modifiers must match exactly")
	}
	if _, run, _ := tk.keyDown(0x7B, tOption|tFn|tNumpad, false); !run {
		t.Errorf("Option-Left with the fn/numpad flags macOS sets on arrows did not match")
	}
}

func TestTapKeysRepeatAndPassthrough(t *testing.T) {
	keys, _ := hotkeysFor(binds(t, "Mod-h", "focus left"))
	tk := newTapKeys(keys)
	if _, run, swallow := tk.keyDown(0x04, tOption, true); run || !swallow {
		t.Errorf("autorepeat: run=%v swallow=%v, want false true", run, swallow)
	}
	if _, run, swallow := tk.keyDown(0x0E, tOption, false); run || swallow { // Option-e: accent dead key
		t.Errorf("unbound Option-e: run=%v swallow=%v, want false false", run, swallow)
	}
}

func TestTapKeysSkipCarbonRoutedCombos(t *testing.T) {
	keys, _ := hotkeysFor(binds(t, "Ctrl-Option-h", "focus left"))
	if tk := newTapKeys(keys); len(tk) != 0 {
		t.Errorf("tap handles %d combos that Carbon delivers", len(tk))
	}
}

func TestTapRouterEmptyPassesEverything(t *testing.T) {
	var r tapRouter
	if _, run, swallow := r.keyDown(0x04, tOption, false); run || swallow {
		t.Errorf("router with no bindings swallowed a key")
	}
}

func TestTapRouterSetAndMatch(t *testing.T) {
	var r tapRouter
	keys, _ := hotkeysFor(binds(t, "Mod-h", "focus left", "Ctrl-Option-l", "focus right"))
	r.set(keys)
	if cmd, run, swallow := r.keyDown(0x04, tOption, false); !run || !swallow || cmd != "focus left" {
		t.Errorf("Option-h: cmd=%q run=%v swallow=%v", cmd, run, swallow)
	}
	if _, _, swallow := r.keyDown(0x25, tOption|tControl, false); swallow {
		t.Errorf("tap swallowed a Carbon-routed combo")
	}
	if !r.active() {
		t.Errorf("router with a tap binding reports inactive")
	}
	r.set(nil)
	if r.active() {
		t.Errorf("router without tap bindings reports active")
	}
}

// The tap runs on its own thread while reload replaces the bindings
// on the main thread; run with -race.
func TestTapRouterConcurrent(t *testing.T) {
	var r tapRouter
	a, _ := hotkeysFor(binds(t, "Mod-h", "focus left"))
	b, _ := hotkeysFor(binds(t, "Mod-h", "focus right"))
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			if cmd, run, _ := r.keyDown(0x04, tOption, false); run && cmd != "focus left" && cmd != "focus right" {
				t.Errorf("torn read: %q", cmd)
				return
			}
		}
	}()
	for i := 0; i < 2000; i++ {
		if i%2 == 0 {
			r.set(a)
		} else {
			r.set(b)
		}
	}
	<-done
}

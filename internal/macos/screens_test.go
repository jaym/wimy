package macos

import (
	"testing"

	"wimy/internal/wm"
)

// laptop: 1512x982 primary; BenQ 2560x1707 to its right, top-aligned.
func twoScreens(visibleTopInset float64) []screenInfo {
	return []screenInfo{
		{Name: "Built-in", Display: 1, Frame: Frame{0, 0, 1512, 982}, Visible: Frame{0, 0, 1512, 982 - visibleTopInset}},
		{Name: "BenQ", Display: 2, Frame: Frame{1512, 982 - 1707, 2560, 1707}, Visible: Frame{1512, 982 - 1707, 2560, 1707 - visibleTopInset}},
	}
}

func TestOutputsForMenuBarHidden(t *testing.T) {
	outs := outputsFor(twoScreens(0), 37)
	if len(outs) != 2 {
		t.Fatalf("%d outputs", len(outs))
	}
	if want := (wm.Rect{X: 0, Y: 0, W: 1512, H: 982}); outs[0].Full != want {
		t.Errorf("full = %+v, want %+v", outs[0].Full, want)
	}
	if want := (wm.Rect{X: 0, Y: 37, W: 1512, H: 945}); outs[0].Usable != want {
		t.Errorf("usable = %+v, want %+v (bar gap below the top edge)", outs[0].Usable, want)
	}
	if want := (wm.Rect{X: 1512, Y: 37, W: 2560, H: 1670}); outs[1].Usable != want {
		t.Errorf("second usable = %+v, want %+v", outs[1].Usable, want)
	}
}

func TestOutputsForBarGapMenuBarVisible(t *testing.T) {
	// visible frame already excludes a 24pt menu bar; SketchyBar (37pt)
	// covers it, so the usable top is 37, not 24+37
	outs := outputsFor(twoScreens(24), 37)
	if outs[0].Usable.Y != 37 || outs[0].Usable.H != 945 {
		t.Errorf("usable = %+v, want Y 37 H 945", outs[0].Usable)
	}
	if outs := outputsFor(twoScreens(24), 0); outs[0].Usable.Y != 24 {
		t.Errorf("no gap: usable Y = %d, want 24 (menu bar)", outs[0].Usable.Y)
	}
}

func TestOutputsForDuplicateNames(t *testing.T) {
	s := twoScreens(0)
	s[0].Name, s[1].Name = "DELL U2720Q", "DELL U2720Q"
	outs := outputsFor(s, 0)
	if outs[0].Name == outs[1].Name {
		t.Errorf("duplicate output names: %q", outs[0].Name)
	}
	if outs[0].Name != "DELL U2720Q" || outs[1].Name != "DELL U2720Q (2)" {
		t.Errorf("names = %q, %q", outs[0].Name, outs[1].Name)
	}
}

func TestOutputsForUnnamed(t *testing.T) {
	s := twoScreens(0)
	s[1].Name = ""
	if got := outputsFor(s, 0)[1].Name; got != "display-2" {
		t.Errorf("unnamed screen = %q, want display-2", got)
	}
}

func TestOutputAt(t *testing.T) {
	outs := outputsFor(twoScreens(0), 0)
	if i := outputAt(outs, wm.Rect{X: 2000, Y: 100, W: 400, H: 300}); i != 1 {
		t.Errorf("window on the BenQ -> output %d", i)
	}
	if i := outputAt(outs, wm.Rect{X: 1400, Y: 100, W: 400, H: 300}); i != 1 {
		t.Errorf("window straddling, centre on the BenQ -> output %d", i)
	}
	if i := outputAt(outs, wm.Rect{X: -5000, Y: -5000, W: 10, H: 10}); i != 0 {
		t.Errorf("window off every screen -> output %d, want 0", i)
	}
}

func dells() []screenInfo {
	s := twoScreens(0)
	s[0].Name, s[1].Name = "DELL", "DELL"
	return s
}

func TestKeepNamesAcrossReorder(t *testing.T) {
	prev := outputsFor(dells(), 0) // d1 "DELL", d2 "DELL (2)"
	s := dells()
	s[0], s[1] = s[1], s[0] // the primary display changed: d2 enumerates first
	next := keepNames(prev, outputsFor(s, 0))
	names := map[uint32]string{}
	for _, o := range next {
		names[o.Display] = o.Name
	}
	if names[1] != "DELL" || names[2] != "DELL (2)" {
		t.Errorf("names after reorder = %v, want d1 DELL, d2 DELL (2)", names)
	}
}

func TestKeepNamesNewIdenticalScreen(t *testing.T) {
	one := dells()[:1]
	prev := outputsFor(one, 0) // d1 "DELL"
	third := screenInfo{Name: "DELL", Display: 3, Frame: Frame{-1920, 0, 1920, 1080}, Visible: Frame{-1920, 0, 1920, 1080}}
	next := keepNames(prev, outputsFor([]screenInfo{third, one[0]}, 0)) // d3 enumerates first
	names := map[uint32]string{}
	for _, o := range next {
		names[o.Display] = o.Name
	}
	if names[1] != "DELL" || names[3] != "DELL (2)" {
		t.Errorf("names = %v, want d1 keeps DELL, new d3 DELL (2)", names)
	}
}

func TestKeepNamesReusesFreedName(t *testing.T) {
	prev := outputsFor(dells(), 0)
	next := keepNames(prev, outputsFor(dells()[1:], 0)) // d1 unplugged
	if len(next) != 1 || next[0].Name != "DELL (2)" {
		t.Errorf("remaining screen = %+v, want it to keep DELL (2)", next)
	}
}

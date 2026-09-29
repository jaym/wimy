//go:build darwin

package config

// platformDefaults are the macOS defaults. Mod is Option: Cmd would
// collide with every application's shortcuts. The programs are
// provisional; Phase 1 of plans/macos-port.md verifies them on a real
// Mac. menu is choose-gui (brew install choose-gui).
var platformDefaults = osDefaults{
	mod:      "Mod1",
	modMask:  Mod1,
	terminal: "open -na Terminal",
	launcher: "open -a Spotlight",
	menu:     "choose",
}

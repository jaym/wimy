//go:build darwin

package config

import "os"

// platformDefaults are the macOS defaults. Mod is Option: Cmd would
// collide with every application's shortcuts. The terminal is the
// best one installed (see pickMacTerminal). The launcher is
// provisional; menu is choose-gui (brew install choose-gui).
var platformDefaults = osDefaults{
	mod:      "Mod1",
	modMask:  Mod1,
	terminal: pickMacTerminal(appExists, homeDir()),
	launcher: "open -a Spotlight",
	menu:     "choose",
	macOS:    true,
}

func appExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

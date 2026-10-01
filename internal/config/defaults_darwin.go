//go:build darwin

package config

import "os"

// platformDefaults are the macOS defaults. Mod is Option: Cmd would
// collide with every application's shortcuts. The terminal is the
// best one installed (see pickMacTerminal). No launcher: Mod-p lists
// the installed applications in the menu program, choose-gui (brew
// install choose-gui, or the Nix home-manager module).
var platformDefaults = osDefaults{
	mod:      "Mod1",
	modMask:  Mod1,
	terminal: pickMacTerminal(appExists, homeDir()),
	launcher: "",
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

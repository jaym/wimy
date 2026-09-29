//go:build !darwin

package config

// platformDefaults are the Linux/river defaults.
var platformDefaults = osDefaults{
	mod:      "Mod4",
	modMask:  Mod4,
	terminal: "alacritty",
	// dmenu-style: a bar anchored to the top screen edge
	launcher: "fuzzel --anchor top --width 120 --lines 10 --border-radius 0",
	menu:     "fuzzel --dmenu --anchor top --width 120 --lines 10 --border-radius 0",
}

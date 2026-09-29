package config

import "path/filepath"

// pickMacTerminal returns the default `terminal` command on macOS: the
// first installed of Ghostty, Alacritty and kitty (in /Applications or
// ~/Applications), else Terminal.app. Each command opens a new window
// without leaving a process behind per press: `open -na` starts a new
// app instance every time, and Ghostty, kitty and Terminal.app keep
// running after their last window closes (Alacritty quits, so it can
// use it). The command runs through sh -c.
func pickMacTerminal(exists func(path string) bool, home string) string {
	find := func(app string) string {
		for _, dir := range []string{"/Applications", filepath.Join(home, "Applications")} {
			if p := filepath.Join(dir, app); exists(p) {
				return p
			}
		}
		return ""
	}
	if find("Ghostty.app") != "" {
		// Ghostty has no CLI for this on macOS (+new-window is Linux
		// only), but its AppleScript dictionary has "new window". The
		// first run asks for the Automation permission.
		return `osascript -e 'if application "Ghostty" is running then' ` +
			`-e 'tell application "Ghostty" to new window' ` +
			`-e 'else' -e 'tell application "Ghostty" to activate' -e 'end if'`
	}
	if find("Alacritty.app") != "" {
		return "open -na Alacritty"
	}
	if p := find("kitty.app"); p != "" {
		return "'" + filepath.Join(p, "Contents", "MacOS", "kitty") + "' --single-instance --directory ~"
	}
	return "open -a Terminal ~"
}

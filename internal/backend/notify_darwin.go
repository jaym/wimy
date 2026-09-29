//go:build darwin

package backend

import "os/exec"

// notifyReloadError shows a macOS notification. The message is passed
// as an argument, not spliced into the script, so it needs no quoting.
func notifyReloadError(msg string) {
	detach(exec.Command("osascript",
		"-e", "on run argv",
		"-e", `display notification (item 1 of argv) with title "wimy: config reload failed"`,
		"-e", "end run",
		msg))
}

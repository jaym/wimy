//go:build linux

package river

import (
	"errors"

	"wimy/internal/wm"
)

// Kill requests that the window close. Called from within a manage
// sequence (commands are drained there).
func (b *Backend) Kill(id wm.WindowID) {
	if w := b.windowByID(id); w != nil {
		w.Object.Close()
	}
}

// Restart implements backend.Platform. river assigns window ids per
// connection, so a new wimy couldn't match the old one's windows: not
// supported here.
func (b *Backend) Restart() error {
	return errors.New("restart is supported on macOS only; quit and start wimy again")
}

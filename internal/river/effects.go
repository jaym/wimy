//go:build linux

package river

import "wimy/internal/wm"

// Kill requests that the window close. Called from within a manage
// sequence (commands are drained there).
func (b *Backend) Kill(id wm.WindowID) {
	if w := b.windowByID(id); w != nil {
		w.Object.Close()
	}
}

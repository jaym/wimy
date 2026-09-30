package macos

import (
	"time"

	"wimy/internal/wm"
)

// echoWindow is how long after wimy focuses a window a focus
// notification for it is taken as the echo of that request.
const echoWindow = 500 * time.Millisecond

// focusEcho tells focus notifications caused by wimy's own focus
// requests (which can arrive after the model has moved on) from real
// user focus changes. One request produces two notifications (AX
// focused-window changed, app activated), so every notification for
// the window within echoWindow of the request is an echo.
type focusEcho struct {
	pending map[wm.WindowID]time.Time
}

// sent records that wimy asked for id to be focused.
func (e *focusEcho) sent(id wm.WindowID, now time.Time) {
	if e.pending == nil {
		e.pending = make(map[wm.WindowID]time.Time)
	}
	e.pending[id] = now
}

// isEcho reports whether a focus notification for id is the echo of a
// recent request. Expired requests are dropped.
func (e *focusEcho) isEcho(id wm.WindowID, now time.Time) bool {
	at, ok := e.pending[id]
	if !ok {
		return false
	}
	if now.Sub(at) > echoWindow {
		delete(e.pending, id)
		return false
	}
	return true
}

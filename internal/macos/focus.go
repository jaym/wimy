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
// focused-window changed, app activated), and the activation handler
// may still read the app's previous window, so within echoWindow of a
// request every report for that window or that app is an echo.
type focusEcho struct {
	windows map[wm.WindowID]time.Time
	apps    map[int]time.Time
}

// sent records that wimy asked for window id of app pid to be focused.
func (e *focusEcho) sent(id wm.WindowID, pid int, now time.Time) {
	if e.windows == nil {
		e.windows = make(map[wm.WindowID]time.Time)
	}
	e.windows[id] = now
	e.sentApp(pid, now)
}

// sentApp records that wimy activated app pid (e.g. Finder, to take
// keyboard focus off a parked window).
func (e *focusEcho) sentApp(pid int, now time.Time) {
	if e.apps == nil {
		e.apps = make(map[int]time.Time)
	}
	e.apps[pid] = now
}

// isEcho reports whether a focus report for window id of app pid is
// the echo of a recent request.
func (e *focusEcho) isEcho(id wm.WindowID, pid int, now time.Time) bool {
	recent := func(at time.Time, ok bool) bool { return ok && now.Sub(at) <= echoWindow }
	w, wok := e.windows[id]
	a, aok := e.apps[pid]
	return recent(w, wok) || recent(a, aok)
}

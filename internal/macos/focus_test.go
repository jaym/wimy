package macos

import (
	"testing"
	"time"
)

func TestFocusEchoSuppressed(t *testing.T) {
	var e focusEcho
	t0 := time.Unix(1000, 0)
	e.sent(1, 100, t0)
	e.sent(2, 200, t0.Add(50*time.Millisecond))
	if !e.isEcho(1, 100, t0.Add(120*time.Millisecond)) {
		t.Errorf("late notification for window 1, which wimy focused 120ms ago, not treated as echo")
	}
	if !e.isEcho(2, 200, t0.Add(130*time.Millisecond)) {
		t.Errorf("notification for window 2 not treated as echo")
	}
	if e.isEcho(3, 300, t0.Add(130*time.Millisecond)) {
		t.Errorf("a window of an app wimy never focused treated as echo")
	}
}

func TestFocusEchoExpires(t *testing.T) {
	var e focusEcho
	t0 := time.Unix(1000, 0)
	e.sent(1, 100, t0)
	if e.isEcho(1, 100, t0.Add(2*time.Second)) {
		t.Errorf("a click on window 1 two seconds later ignored")
	}
}

func TestFocusEchoAbsorbsBothNotifications(t *testing.T) {
	// one focus request yields an AX focused-window notification and an
	// app-activation notification; both are echoes
	var e focusEcho
	t0 := time.Unix(1000, 0)
	e.sent(1, 100, t0)
	e.sent(2, 200, t0.Add(50*time.Millisecond))
	for _, dt := range []time.Duration{100, 150} {
		if !e.isEcho(1, 100, t0.Add(dt*time.Millisecond)) {
			t.Errorf("notification for window 1 at +%dms not treated as echo", dt)
		}
	}
}

func TestFocusEchoActivationReportsOtherWindow(t *testing.T) {
	// wimy focuses window 1 of app 100; the activation handler reads the
	// app's focused window before it switched and reports window 7
	var e focusEcho
	t0 := time.Unix(1000, 0)
	e.sent(1, 100, t0)
	if !e.isEcho(7, 100, t0.Add(40*time.Millisecond)) {
		t.Errorf("stale report of another window of the app wimy just activated not treated as echo")
	}
}

func TestFocusEchoAppOnly(t *testing.T) {
	// focusing nothing activates Finder (pid 400); its report is an echo
	var e focusEcho
	t0 := time.Unix(1000, 0)
	e.sentApp(400, t0)
	if !e.isEcho(9, 400, t0.Add(30*time.Millisecond)) {
		t.Errorf("Finder's activation after wimy focused nothing not treated as echo")
	}
	if e.isEcho(9, 400, t0.Add(time.Second)) {
		t.Errorf("a real Finder click a second later ignored")
	}
}

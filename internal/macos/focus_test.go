package macos

import (
	"testing"
	"time"
)

func TestFocusEchoSuppressed(t *testing.T) {
	var e focusEcho
	t0 := time.Unix(1000, 0)
	e.sent(1, t0)
	e.sent(2, t0.Add(50*time.Millisecond))
	if !e.isEcho(1, t0.Add(120*time.Millisecond)) {
		t.Errorf("late notification for window 1, which wimy focused 120ms ago, not treated as echo")
	}
	if !e.isEcho(2, t0.Add(130*time.Millisecond)) {
		t.Errorf("notification for window 2 not treated as echo")
	}
	if e.isEcho(3, t0.Add(130*time.Millisecond)) {
		t.Errorf("a window wimy never focused treated as echo")
	}
}

func TestFocusEchoExpires(t *testing.T) {
	var e focusEcho
	t0 := time.Unix(1000, 0)
	e.sent(1, t0)
	if e.isEcho(1, t0.Add(2*time.Second)) {
		t.Errorf("a click on window 1 two seconds later ignored")
	}
}

func TestFocusEchoConsumed(t *testing.T) {
	var e focusEcho
	t0 := time.Unix(1000, 0)
	e.sent(1, t0)
	if !e.isEcho(1, t0.Add(10*time.Millisecond)) {
		t.Fatal("first echo not recognized")
	}
	if e.isEcho(1, t0.Add(20*time.Millisecond)) {
		t.Errorf("second notification for the same focus still treated as echo (a real click right after is lost)")
	}
}

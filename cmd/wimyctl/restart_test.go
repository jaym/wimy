package main

import (
	"errors"
	"testing"
	"time"
)

func TestWaitRestarted(t *testing.T) {
	old := time.Unix(100, 0)
	answers := []struct {
		t   time.Time
		err error
	}{
		{old, nil},                           // not restarted yet
		{time.Time{}, errors.New("refused")}, // socket gone while execing
		{time.Unix(200, 0), nil},             // the new instance
	}
	i := 0
	fetch := func() (time.Time, error) {
		a := answers[min(i, len(answers)-1)]
		i++
		return a.t, a.err
	}
	if err := waitRestarted(old, fetch, 5*time.Second, func(time.Duration) {}); err != nil {
		t.Errorf("waitRestarted = %v, want nil once a new instance answers", err)
	}
}

func TestWaitRestartedTimesOut(t *testing.T) {
	old := time.Unix(100, 0)
	var slept time.Duration
	err := waitRestarted(old, func() (time.Time, error) { return old, nil }, 2*time.Second,
		func(d time.Duration) { slept += d })
	if err == nil {
		t.Errorf("no error although the same instance kept answering")
	}
	if slept < 2*time.Second {
		t.Errorf("gave up after %v, before the deadline", slept)
	}
}

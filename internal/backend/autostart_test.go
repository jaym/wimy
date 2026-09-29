package backend

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// waitFor polls cond until it holds or 5s pass.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAutostartSyncKillsRemovedSpawnsAdded(t *testing.T) {
	var a Autostart
	a.StartAll([]string{"sleep 30"})
	old := a.procs["sleep 30"][0]

	killed, spawned, restarted := a.Sync([]string{"sleep 30"}, []string{"sleep 31"})
	t.Cleanup(func() { a.Sync([]string{"sleep 31"}, nil) })

	if killed != 1 || spawned != 1 || restarted != 0 {
		t.Fatalf("got killed=%d spawned=%d restarted=%d, want 1 1 0", killed, spawned, restarted)
	}
	waitFor(t, "removed process to exit", old.dead.Load)
	if _, ok := a.procs["sleep 30"]; ok {
		t.Errorf("removed entry still tracked")
	}
	if n := len(a.procs["sleep 31"]); n != 1 {
		t.Errorf("added entry: %d processes, want 1", n)
	}
}

func TestAutostartSyncKeepsUnchanged(t *testing.T) {
	var a Autostart
	a.StartAll([]string{"sleep 30"})
	t.Cleanup(func() { a.Sync([]string{"sleep 30"}, nil) })
	before := a.procs["sleep 30"][0]

	killed, spawned, restarted := a.Sync([]string{"sleep 30"}, []string{"sleep 30"})

	if killed+spawned+restarted != 0 {
		t.Fatalf("got killed=%d spawned=%d restarted=%d, want all 0", killed, spawned, restarted)
	}
	if a.procs["sleep 30"][0] != before {
		t.Errorf("unchanged entry was restarted")
	}
}

func TestAutostartSyncRestartsExited(t *testing.T) {
	var a Autostart
	a.StartAll([]string{"true"})
	p := a.procs["true"][0]
	waitFor(t, "true to exit", p.dead.Load)

	_, _, restarted := a.Sync([]string{"true"}, []string{"true"})

	if restarted != 1 {
		t.Fatalf("restarted=%d, want 1", restarted)
	}
}

func TestAutostartKillsWholeProcessGroup(t *testing.T) {
	var a Autostart
	// the shell forks sleep instead of exec'ing it; killing only the
	// shell's pid would orphan the sleep
	pidfile := filepath.Join(t.TempDir(), "pid")
	cmdline := "sleep 30 & echo $! > " + pidfile + "; wait"
	a.StartAll([]string{cmdline})
	var child int
	waitFor(t, "child pid", func() bool {
		b, err := os.ReadFile(pidfile)
		if err != nil {
			return false
		}
		child, err = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil
	})

	a.Sync([]string{cmdline}, nil)

	waitFor(t, "forked child to die", func() bool {
		return syscall.Kill(child, 0) != nil
	})
}

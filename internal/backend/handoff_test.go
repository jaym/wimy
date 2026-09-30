package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"wimy/internal/config"
	"wimy/internal/wm"
)

func coreWithLayout(t *testing.T) *Core {
	t.Helper()
	c, _ := newTestCore(t, nil)
	c.State.AddWindow(1, false)
	c.State.AddWindow(2, false)
	c.State.MoveDir(wm.DirRight)
	c.State.SetMode(wm.ModeStack)
	c.State.AddWindow(3, true)
	c.State.SetFloatRect(3, wm.Rect{X: 300, Y: 200, W: 400, H: 300})
	c.State.TagSpec("+web")
	c.State.FocusWindow(1)
	return c
}

type extras struct {
	Parked []wm.WindowID `json:"parked"`
}

func TestHandoffRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.json")
	old := coreWithLayout(t)
	if err := old.WriteHandoff(path, 42, extras{Parked: []wm.WindowID{9}}); err != nil {
		t.Fatal(err)
	}
	next, _ := newTestCore(t, nil)
	var ex extras
	ok, err := next.ReadHandoff(path, 42, time.Now(), &ex)
	if err != nil || !ok {
		t.Fatalf("ReadHandoff = %v, %v", ok, err)
	}
	if got, want := layoutOf(next.State), layoutOf(old.State); got != want {
		t.Errorf("layout after handoff:\n%s\nwant\n%s", got, want)
	}
	if next.State.Focused != 1 || len(ex.Parked) != 1 || ex.Parked[0] != 9 {
		t.Errorf("focus %d, extras %+v", next.State.Focused, ex)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("handoff not consumed: %v", err)
	}
}

func TestHandoffKeepsConfigDerivedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.json")
	old := coreWithLayout(t)
	old.State.TitlebarHeight = 99
	if err := old.WriteHandoff(path, 1, nil); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Titlebar.Height = 22
	next, _ := newTestCore(t, cfg)
	if ok, _ := next.ReadHandoff(path, 1, time.Now(), nil); !ok {
		t.Fatal("not applied")
	}
	if next.State.TitlebarHeight != 22 {
		t.Errorf("titlebar height %d from the old process, want 22 from this config", next.State.TitlebarHeight)
	}
}

func TestReadHandoffStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.json")
	old := coreWithLayout(t)
	if err := old.WriteHandoff(path, 1, nil); err != nil {
		t.Fatal(err)
	}
	next, _ := newTestCore(t, nil)
	if ok, err := next.ReadHandoff(path, 1, time.Now().Add(10*time.Minute), nil); ok || err != nil {
		t.Errorf("stale handoff applied: %v %v", ok, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stale handoff not deleted")
	}
}

func TestReadHandoffOtherBoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.json")
	old := coreWithLayout(t)
	if err := old.WriteHandoff(path, 1, nil); err != nil {
		t.Fatal(err)
	}
	next, _ := newTestCore(t, nil)
	if ok, _ := next.ReadHandoff(path, 2, time.Now(), nil); ok {
		t.Errorf("handoff from another boot applied (window ids restart after a reboot)")
	}
}

func TestReadHandoffMissing(t *testing.T) {
	next, _ := newTestCore(t, nil)
	if ok, err := next.ReadHandoff(filepath.Join(t.TempDir(), "none.json"), 1, time.Now(), nil); ok || err != nil {
		t.Errorf("missing handoff: %v %v", ok, err)
	}
}

func TestRestoreStatePrunes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.json")
	old := coreWithLayout(t)
	if err := old.WriteHandoff(path, 1, nil); err != nil {
		t.Fatal(err)
	}
	next, _ := newTestCore(t, nil)
	next.ReadHandoff(path, 1, time.Now(), nil)
	gone := next.PruneWindows(func(id wm.WindowID) bool { return id != 2 }) // 2 closed meanwhile
	if len(gone) != 1 || gone[0] != 2 || next.State.Windows[2] != nil {
		t.Errorf("pruned %v, window 2 still %v", gone, next.State.Windows[2])
	}
}

// layoutOf renders a state's layout for comparison, in a stable order
// (Layout appends hidden windows by ranging over a map).
func layoutOf(s *wm.State) string {
	var lines []string
	for _, p := range s.Layout() {
		lines = append(lines, fmt.Sprintf("%d %+v hidden=%v collapsed=%v layer=%v focused=%v",
			p.ID, p.Rect, p.Hidden, p.Collapsed, p.Layer, p.Focused))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func TestHandoffReconcilesAutostart(t *testing.T) {
	// the old process ran "sleep 31" and "sleep 32" (and "true", which
	// had died); the config the new process loaded lists sleep 31,
	// sleep 33 and true
	path := filepath.Join(t.TempDir(), "handoff.json")
	oldCfg := config.Default()
	oldCfg.Autostart = []string{"sleep 31", "sleep 32", "true"}
	old := NewCore(oldCfg, "", &fakePlatform{})
	old.StartAutostart()
	waitFor(t, "true to exit", func() bool { return len(old.autostart.Pids()["true"]) == 0 })
	if err := old.WriteHandoff(path, 1, nil); err != nil {
		t.Fatal(err)
	}
	newCfg := config.Default()
	newCfg.Autostart = []string{"sleep 31", "sleep 33", "true"}
	next := NewCore(newCfg, "", &fakePlatform{})
	t.Cleanup(func() { next.autostart.Sync(newCfg.Autostart, nil); old.autostart.Sync([]string{"sleep 32"}, nil) })
	if ok, err := next.ReadHandoff(path, 1, time.Now(), nil); !ok || err != nil {
		t.Fatalf("ReadHandoff = %v %v", ok, err)
	}
	pids := next.autostart.Pids()
	if len(pids["sleep 31"]) != 1 || len(pids["sleep 33"]) != 1 || len(pids["sleep 32"]) != 0 {
		t.Errorf("after the handoff: %v, want sleep 31 kept, sleep 33 started, sleep 32 stopped", pids)
	}
	if len(pids["true"]) != 1 && len(next.autostart.procs["true"]) != 1 {
		t.Errorf("an entry that had died before the restart was not restarted: %v", next.autostart.procs["true"])
	}
}

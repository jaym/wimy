package backend

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"wimy/internal/config"
)

// newReloadCore returns a Core whose config path is a temp file
// holding src, with the error notifier captured.
func newReloadCore(t *testing.T, src string) (*Core, *fakePlatform, *[]string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.kdl")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var notes []string
	old := notifyError
	notifyError = func(msg string) { notes = append(notes, msg) }
	t.Cleanup(func() { notifyError = old })

	p := &fakePlatform{}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore(cfg, path, p)
	return c, p, &notes, path
}

func TestReloadBadConfigKeepsOld(t *testing.T) {
	c, p, notes, path := newReloadCore(t, "stack-strip 30\n")
	before := c.Cfg
	if err := os.WriteFile(path, []byte("no-such-setting 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := c.Reload(); err == nil {
		t.Fatal("Reload of a broken config: no error")
	}
	if c.Cfg != before {
		t.Errorf("config replaced by a broken one")
	}
	if len(p.changes) != 0 {
		t.Errorf("platform asked to apply %v", p.changes)
	}
	if len(*notes) != 1 || !strings.Contains((*notes)[0], "keeping the old config") {
		t.Errorf("notifications = %q", *notes)
	}
}

func TestReloadAppliesModelSections(t *testing.T) {
	c, p, _, path := newReloadCore(t, "stack-strip 30\n")
	if err := os.WriteFile(path, []byte("stack-strip 40\ntitlebar height=30\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := c.Reload(); err != nil {
		t.Fatal(err)
	}
	if c.State.StackStrip != 40 || c.State.TitlebarHeight != 30 {
		t.Errorf("state strip=%d titlebar=%d, want 40 30", c.State.StackStrip, c.State.TitlebarHeight)
	}
	want := []ConfigChange{{Titlebar: true}}
	if !slices.Equal(p.changes, want) {
		t.Errorf("platform changes = %+v, want %+v", p.changes, want)
	}
}

func TestApplyConfigReportOrder(t *testing.T) {
	c, _, _, _ := newReloadCore(t, "")
	next := *c.Cfg
	next.Binds = nil
	next.Terminal = "foot"
	next.StackStrip = c.Cfg.StackStrip + 1
	next.Autostart = []string{"true"}

	got := c.applyConfig(&next)
	t.Cleanup(func() { c.autostart.Sync(next.Autostart, nil) })

	want := []string{"keybindings", "stack-strip", "terminal", "autostart (0 killed, 1 spawned, 0 restarted)"}
	if !slices.Equal(got, want) {
		t.Errorf("report = %q, want %q", got, want)
	}
}

func TestApplyConfigNoChanges(t *testing.T) {
	c, p, _, _ := newReloadCore(t, "")
	same := *c.Cfg
	if got := c.applyConfig(&same); len(got) != 0 {
		t.Errorf("report = %q, want nothing", got)
	}
	if !slices.Equal(p.changes, []ConfigChange{{}}) {
		t.Errorf("platform changes = %+v, want one empty change", p.changes)
	}
}

func TestReloadBarGap(t *testing.T) {
	c, p, _, path := newReloadCore(t, "")
	if err := os.WriteFile(path, []byte("bar-gap 37\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Reload(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.changes, []ConfigChange{{BarGap: true}}) {
		t.Errorf("platform changes = %+v, want BarGap", p.changes)
	}
}

func TestReloadSwipesRebinds(t *testing.T) {
	c, p, _, path := newReloadCore(t, "")
	if err := os.WriteFile(path, []byte("swipe \"left\" { view-next; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Reload(); err != nil {
		t.Fatal(err)
	}
	if len(p.changes) != 1 || !p.changes[0].Binds {
		t.Fatalf("changes = %+v, want Binds (swipes go with the bindings)", p.changes)
	}
	if c.Cfg.Swipes["left"] != "view-next" {
		t.Errorf("swipes = %v", c.Cfg.Swipes)
	}
}

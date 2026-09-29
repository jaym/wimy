package backend

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"wimy/internal/command"
	"wimy/internal/config"
	"wimy/internal/wm"
)

// fakePlatform records what the Core asks of its backend.
type fakePlatform struct {
	wakes   atomic.Int32
	changes []ConfigChange
	killed  []wm.WindowID
	quit    bool
}

func (f *fakePlatform) Wake()                             { f.wakes.Add(1) }
func (f *fakePlatform) ApplyConfigChange(ch ConfigChange) { f.changes = append(f.changes, ch) }
func (f *fakePlatform) Kill(id wm.WindowID)               { f.killed = append(f.killed, id) }
func (f *fakePlatform) Quit()                             { f.quit = true }

// newTestCore returns a Core over a state with one output.
func newTestCore(t *testing.T, cfg *config.Config) (*Core, *fakePlatform) {
	t.Helper()
	if cfg == nil {
		cfg = config.Default()
	}
	p := &fakePlatform{}
	c := NewCore(cfg, "", p)
	o := c.State.AddOutput("out")
	o.Rect = wm.Rect{W: 1280, H: 720}
	return c, p
}

func queued(c *Core) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.queue)
}

func TestQueueCommandWakesAndDrainRuns(t *testing.T) {
	c, p := newTestCore(t, nil)
	c.QueueCommand("view web")
	if p.wakes.Load() != 1 {
		t.Errorf("wakes = %d, want 1", p.wakes.Load())
	}
	c.DrainQueue()
	if got := c.State.Outputs[0].View; got != "web" {
		t.Errorf("view = %q, want web", got)
	}
	if q := queued(c); len(q) != 0 {
		t.Errorf("queue not drained: %v", q)
	}
}

func TestEnqueueDoesNotWake(t *testing.T) {
	c, p := newTestCore(t, nil)
	c.Enqueue("view web")
	if p.wakes.Load() != 0 {
		t.Errorf("Enqueue woke the backend; key bindings run on the dispatch thread")
	}
	if q := queued(c); !slices.Equal(q, []string{"view web"}) {
		t.Errorf("queue = %v", q)
	}
}

func TestQueueCommandConcurrent(t *testing.T) {
	c, _ := newTestCore(t, nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				c.QueueCommand("view web")
			}
		}()
	}
	for j := 0; j < 50; j++ {
		c.Enqueue("view main")
	}
	wg.Wait()
	if n := len(queued(c)); n != 450 {
		t.Fatalf("queued %d commands, want 450", n)
	}
	c.DrainQueue()
	if n := len(queued(c)); n != 0 {
		t.Errorf("%d commands left after drain", n)
	}
}

func TestDrainQueueSurvivesBadCommand(t *testing.T) {
	c, _ := newTestCore(t, nil)
	c.Enqueue("no-such-command")
	c.Enqueue("view web")
	c.DrainQueue()
	if got := c.State.Outputs[0].View; got != "web" {
		t.Errorf("command after a bad one did not run: view = %q", got)
	}
}

func TestKillAndQuitReachPlatform(t *testing.T) {
	c, p := newTestCore(t, nil)
	c.State.AddWindow(7, false)
	c.Enqueue("kill")
	c.Enqueue("quit")
	c.DrainQueue()
	if !slices.Equal(p.killed, []wm.WindowID{7}) {
		t.Errorf("killed = %v, want [7]", p.killed)
	}
	if !p.quit {
		t.Errorf("quit did not reach the platform")
	}
}

func TestActionsSortedAndUnknown(t *testing.T) {
	cfg := config.Default()
	cfg.Actions = map[string]string{"zeta": "true", "alpha": "true", "mid": "true"}
	c, _ := newTestCore(t, cfg)
	if got := c.Actions(); !slices.Equal(got, []string{"alpha", "mid", "zeta"}) {
		t.Errorf("Actions() = %v", got)
	}
	if err := c.Action("nope"); err == nil {
		t.Errorf("unknown action: no error")
	}
	if err := c.Spawn(nil); err == nil {
		t.Errorf("empty spawn: no error")
	}
}

func TestPromptFollowUp(t *testing.T) {
	cases := []struct {
		kind   command.PromptKind
		out    string
		failed bool
		want   string
	}{
		{command.PromptView, "web\n", false, "view web"},
		{command.PromptMoveTo, " 3 ", false, "moveto 3"},
		{command.PromptAction, "lock", false, "action lock"},
		{command.PromptView, "web", true, ""},   // menu canceled (non-zero exit)
		{command.PromptView, "  \n", false, ""}, // empty answer
	}
	for _, tc := range cases {
		got, ok := promptFollowUp(tc.kind, tc.out, tc.failed)
		if (tc.want != "") != ok || got != tc.want {
			t.Errorf("promptFollowUp(%v, %q, %v) = %q, %v; want %q", tc.kind, tc.out, tc.failed, got, ok, tc.want)
		}
	}
}

func TestPromptQueuesAnswer(t *testing.T) {
	menu := filepath.Join(t.TempDir(), "menu")
	if err := os.WriteFile(menu, []byte("#!/bin/sh\ncat >/dev/null\necho web\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Menu = menu
	c, p := newTestCore(t, cfg)

	if err := c.Prompt(command.PromptView, []string{"1", "web"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "prompt answer to be queued", func() bool { return p.wakes.Load() == 1 })
	if q := queued(c); !slices.Equal(q, []string{"view web"}) {
		t.Errorf("queue = %v, want [view web]", q)
	}
}

func TestSpawnTerminalAndMenuUseShell(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Terminal = `printf '%s' "terminal ok" > '` + filepath.Join(dir, "t") + `'`
	cfg.Launcher = `printf '%s' "launcher ok" > '` + filepath.Join(dir, "l") + `'`
	c, _ := newTestCore(t, cfg)
	if err := c.SpawnTerminal(); err != nil {
		t.Fatal(err)
	}
	if err := c.SpawnMenu(); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"t": "terminal ok", "l": "launcher ok"} {
		waitFor(t, name+" written by the shell", func() bool {
			b, err := os.ReadFile(filepath.Join(dir, name))
			return err == nil && string(b) == want
		})
	}
}

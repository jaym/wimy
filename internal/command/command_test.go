package command

import (
	"testing"

	"wimy/internal/wm"
)

type fakeFx struct {
	promptKind    PromptKind
	promptChoices []string
	actions       []string
	acted         string
	killed        wm.WindowID
	quit          bool
}

func (f *fakeFx) Spawn(argv []string) error { return nil }
func (f *fakeFx) SpawnTerminal() error      { return nil }
func (f *fakeFx) SpawnMenu() error          { return nil }
func (f *fakeFx) Action(name string) error  { f.acted = name; return nil }
func (f *fakeFx) Actions() []string         { return f.actions }
func (f *fakeFx) Kill(id wm.WindowID)       { f.killed = id }
func (f *fakeFx) Quit()                     { f.quit = true }
func (f *fakeFx) Prompt(kind PromptKind, choices []string) error {
	f.promptKind = kind
	f.promptChoices = choices
	return nil
}

func newTestEnv() (*Env, *fakeFx) {
	fx := &fakeFx{actions: []string{"lock", "quit"}}
	s := wm.NewState()
	o := s.AddOutput("HEADLESS-1")
	o.Rect = wm.Rect{W: 1280, H: 720}
	return &Env{State: s, Fx: fx}, fx
}

func TestActionPromptListsActions(t *testing.T) {
	env, fx := newTestEnv()
	r := New(env)
	if err := r.Run("action"); err != nil {
		t.Fatal(err)
	}
	if fx.promptKind != PromptAction {
		t.Fatalf("kind: %v", fx.promptKind)
	}
	if len(fx.promptChoices) != 2 || fx.promptChoices[0] != "lock" {
		t.Fatalf("choices: %v", fx.promptChoices)
	}
}

func TestActionByName(t *testing.T) {
	env, fx := newTestEnv()
	r := New(env)
	if err := r.Run("action lock"); err != nil {
		t.Fatal(err)
	}
	if fx.acted != "lock" {
		t.Fatalf("acted: %q", fx.acted)
	}
}

func TestUnknownCommand(t *testing.T) {
	env, _ := newTestEnv()
	r := New(env)
	if err := r.Run("frobnicate"); err == nil {
		t.Fatal("unknown command should error")
	}
}

func TestKillFocused(t *testing.T) {
	env, fx := newTestEnv()
	env.State.AddWindow(1, false)
	r := New(env)
	if err := r.Run("kill"); err != nil {
		t.Fatal(err)
	}
	if fx.killed != 1 {
		t.Fatalf("killed: %d", fx.killed)
	}
}

func TestViewPromptChoices(t *testing.T) {
	env, fx := newTestEnv()
	env.State.AddWindow(1, false, "web")
	env.State.FocusWindow(1) // jumps to view web; empty view 1 is GC'd
	r := New(env)
	if err := r.Run("view"); err != nil {
		t.Fatal(err)
	}
	if fx.promptKind != PromptView || len(fx.promptChoices) != 1 || fx.promptChoices[0] != "web" {
		t.Fatalf("choices: %v", fx.promptChoices)
	}
}

// Package command implements the command registry shared by all of
// wimy's frontends: key bindings, the JSON-RPC interface, and the
// configuration file. A command is a name plus string arguments, e.g.
// "focus left" or "tag +web".
package command

import (
	"fmt"
	"strconv"
	"strings"

	"wimy/internal/wm"
)

// PromptKind identifies what an interactive prompt is asking for.
type PromptKind int

const (
	// PromptView asks for the name of a view to select (Mod-t).
	PromptView PromptKind = iota
	// PromptMoveTo asks for the name of a view to move the focused
	// window to (Mod-Shift-t).
	PromptMoveTo
	// PromptAction asks for the name of an action to run (Mod-a).
	PromptAction
)

// Effects are the side effects a command may trigger. They are
// implemented by the Wayland backend. Prompt is asynchronous: the
// backend runs the menu program and feeds the chosen answer back into
// the command queue as a new command.
type Effects interface {
	// Spawn starts a program without waiting for it.
	Spawn(argv []string) error
	// SpawnTerminal starts the configured terminal emulator (Mod-Return).
	SpawnTerminal() error
	// SpawnMenu starts the configured program launcher (Mod-p).
	SpawnMenu() error
	// Prompt asks the user to choose or enter a string, using the
	// configured menu program. choices are suggestions; free-form
	// input is allowed.
	Prompt(kind PromptKind, choices []string) error
	// Action runs the named action from the configuration.
	Action(name string) error
	// Actions returns the names of the configured actions (for the
	// Mod-a prompt).
	Actions() []string
	// Kill requests that the given window close.
	Kill(id wm.WindowID)
	// Quit exits the window manager.
	Quit()
}

// Env is the context command handlers run in.
type Env struct {
	State *wm.State
	Fx    Effects
}

// Handler implements one command.
type Handler func(e *Env, args []string) error

// Registry dispatches command strings to handlers.
type Registry struct {
	env      *Env
	handlers map[string]Handler
}

// New returns a registry with all built-in commands.
func New(env *Env) *Registry {
	r := &Registry{env: env, handlers: make(map[string]Handler)}
	r.handlers = map[string]Handler{
		"focus":              cmdFocus,
		"focus-toggle-layer": func(e *Env, _ []string) error { e.State.FocusToggleLayer(); return nil },
		"focus-output":       cmdFocusOutput,
		"focus-window":       cmdFocusWindow,
		"move":               cmdMove,
		"toggle-float":       func(e *Env, _ []string) error { e.State.ToggleFloat(); return nil },
		"mode":               cmdMode,
		"grow":               cmdGrow,
		"view":               cmdView,
		"view-next":          func(e *Env, _ []string) error { e.State.CycleView(1); return nil },
		"view-prev":          func(e *Env, _ []string) error { e.State.CycleView(-1); return nil },
		"view-n":             cmdViewN,
		"moveto":             cmdMoveTo,
		"moveto-n":           cmdMoveToN,
		"tag":                cmdTag,
		"kill":               cmdKill,
		"spawn":              cmdSpawn,
		"spawn-terminal":     func(e *Env, _ []string) error { return e.Fx.SpawnTerminal() },
		"spawn-menu":         func(e *Env, _ []string) error { return e.Fx.SpawnMenu() },
		"action":             cmdAction,
		"quit":               func(e *Env, _ []string) error { e.Fx.Quit(); return nil },
	}
	return r
}

// Run parses and executes a command string.
func (r *Registry) Run(cmd string) error {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return nil
	}
	h, ok := r.handlers[fields[0]]
	if !ok {
		return fmt.Errorf("unknown command %q", fields[0])
	}
	return h(r.env, fields[1:])
}

// Names returns the names of all registered commands (sorted).
func (r *Registry) Names() []string {
	var out []string
	for n := range r.handlers {
		out = append(out, n)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func arg(args []string, i int) (string, error) {
	if i >= len(args) {
		return "", fmt.Errorf("missing argument %d", i+1)
	}
	return args[i], nil
}

func cmdFocus(e *Env, args []string) error {
	a, err := arg(args, 0)
	if err != nil {
		return err
	}
	d, err := wm.ParseDirection(a)
	if err != nil {
		return err
	}
	e.State.FocusDir(d)
	return nil
}

func cmdFocusOutput(e *Env, args []string) error {
	a, err := arg(args, 0)
	if err != nil {
		return err
	}
	switch a {
	case "next":
		e.State.CycleOutput(1)
	case "prev":
		e.State.CycleOutput(-1)
	default:
		return fmt.Errorf("usage: focus-output <next|prev>")
	}
	return nil
}

func cmdFocusWindow(e *Env, args []string) error {
	a, err := arg(args, 0)
	if err != nil {
		return err
	}
	id, err := strconv.ParseUint(a, 10, 32)
	if err != nil {
		return fmt.Errorf("usage: focus-window <id>")
	}
	e.State.FocusWindow(wm.WindowID(id))
	return nil
}

func cmdMove(e *Env, args []string) error {
	a, err := arg(args, 0)
	if err != nil {
		return err
	}
	d, err := wm.ParseDirection(a)
	if err != nil {
		return err
	}
	e.State.MoveDir(d)
	return nil
}

func cmdMode(e *Env, args []string) error {
	a, err := arg(args, 0)
	if err != nil {
		return err
	}
	m, err := wm.ParseMode(a)
	if err != nil {
		return err
	}
	e.State.SetMode(m)
	return nil
}

func cmdGrow(e *Env, args []string) error {
	a, err := arg(args, 0)
	if err != nil {
		return err
	}
	d, err := wm.ParseDirection(a)
	if err != nil {
		return err
	}
	if d != wm.DirLeft && d != wm.DirRight {
		return fmt.Errorf("usage: grow <left|right> [percent]")
	}
	pct := 10.0
	if len(args) > 1 {
		if v, err := strconv.ParseFloat(args[1], 64); err == nil {
			pct = v
		}
	}
	e.State.Grow(d, pct)
	return nil
}

func viewNames(s *wm.State) []string {
	var out []string
	for _, v := range s.Views {
		out = append(out, v.Name)
	}
	return out
}

func cmdView(e *Env, args []string) error {
	if len(args) == 0 {
		return e.Fx.Prompt(PromptView, viewNames(e.State))
	}
	e.State.SelectView(args[0])
	return nil
}

func cmdViewN(e *Env, args []string) error {
	a, err := arg(args, 0)
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(a)
	if err != nil {
		return fmt.Errorf("usage: view-n <number>")
	}
	e.State.SelectViewN(n)
	return nil
}

func cmdMoveTo(e *Env, args []string) error {
	if len(args) == 0 {
		return e.Fx.Prompt(PromptMoveTo, viewNames(e.State))
	}
	e.State.MoveToView(args[0])
	return nil
}

func cmdMoveToN(e *Env, args []string) error {
	a, err := arg(args, 0)
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(a)
	if err != nil {
		return fmt.Errorf("usage: moveto-n <number>")
	}
	e.State.MoveToViewN(n)
	return nil
}

func cmdTag(e *Env, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: tag <[+-]name>...")
	}
	e.State.TagSpec(args...)
	return nil
}

func cmdKill(e *Env, _ []string) error {
	if e.State.Focused != 0 {
		e.Fx.Kill(e.State.Focused)
	}
	return nil
}

func cmdSpawn(e *Env, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: spawn <command> [args...]")
	}
	return e.Fx.Spawn(args)
}

func cmdAction(e *Env, args []string) error {
	if len(args) == 0 {
		return e.Fx.Prompt(PromptAction, e.Fx.Actions())
	}
	return e.Fx.Action(args[0])
}

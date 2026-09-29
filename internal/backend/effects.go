package backend

import (
	"bytes"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"wimy/internal/command"
	"wimy/internal/wm"
)

// Spawn starts a program detached from wimy.
func (c *Core) Spawn(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("spawn: empty command")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn %q: %w", argv[0], err)
	}
	go func() { _, _ = cmd.Process.Wait() }()
	return nil
}

// SpawnTerminal starts the configured terminal emulator.
func (c *Core) SpawnTerminal() error {
	return c.Spawn(strings.Fields(c.Cfg.Terminal))
}

// SpawnMenu starts the configured program launcher.
func (c *Core) SpawnMenu() error {
	return c.Spawn(strings.Fields(c.Cfg.Launcher))
}

// Prompt runs the configured menu program in dmenu mode with the given
// choices and feeds the answer back as a command. It runs
// asynchronously; canceling the menu does nothing.
func (c *Core) Prompt(kind command.PromptKind, choices []string) error {
	argv := strings.Fields(c.Cfg.Menu)
	if len(argv) == 0 {
		return fmt.Errorf("no menu program configured")
	}
	var label string
	switch kind {
	case command.PromptView:
		label = "go to tag: "
	case command.PromptMoveTo:
		label = "move to tag: "
	case command.PromptAction:
		label = "action: "
	}
	if label != "" {
		argv = append(argv, "-p", label)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewBufferString(strings.Join(choices, "\n") + "\n")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	go func() {
		err := cmd.Wait()
		if follow, ok := promptFollowUp(kind, out.String(), err != nil); ok {
			c.QueueCommand(follow)
		}
	}()
	return nil
}

// promptFollowUp turns the menu program's output into the command to
// queue. failed is true when the menu exited non-zero (canceled).
func promptFollowUp(kind command.PromptKind, out string, failed bool) (string, bool) {
	answer := strings.TrimSpace(out)
	if failed || answer == "" {
		return "", false
	}
	switch kind {
	case command.PromptView:
		return "view " + answer, true
	case command.PromptMoveTo:
		return "moveto " + answer, true
	case command.PromptAction:
		return "action " + answer, true
	}
	return "", false
}

// Action runs the named action from the configuration via the shell.
func (c *Core) Action(name string) error {
	cmdline, ok := c.Cfg.Actions[name]
	if !ok {
		return fmt.Errorf("unknown action %q", name)
	}
	return c.Spawn([]string{"sh", "-c", cmdline})
}

// Actions returns the sorted names of the configured actions.
func (c *Core) Actions() []string {
	var out []string
	for name := range c.Cfg.Actions {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// Kill implements command.Effects via the platform.
func (c *Core) Kill(id wm.WindowID) { c.platform.Kill(id) }

// Quit implements command.Effects via the platform.
func (c *Core) Quit() { c.platform.Quit() }

// detach starts cmd without waiting for it; it reaps the process in
// the background. It reports whether the start succeeded.
func detach(cmd *exec.Cmd) bool {
	if err := cmd.Start(); err != nil {
		return false
	}
	go func() { _, _ = cmd.Process.Wait() }()
	return true
}

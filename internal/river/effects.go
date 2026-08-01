package river

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"wimy/internal/command"
	"wimy/internal/wm"
)

// --- command.Effects implementation ---

// Spawn starts a program detached from wimy.
func (b *Backend) Spawn(argv []string) error {
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
func (b *Backend) SpawnTerminal() error {
	return b.Spawn(strings.Fields(b.cfg.Terminal))
}

// SpawnMenu starts the configured program launcher.
func (b *Backend) SpawnMenu() error {
	return b.Spawn(strings.Fields(b.cfg.Launcher))
}

// Prompt runs the configured menu program in dmenu mode with the given
// choices and feeds the answer back as a command. It runs
// asynchronously; canceling the menu does nothing.
func (b *Backend) Prompt(kind command.PromptKind, choices []string) error {
	argv := strings.Fields(b.cfg.Menu)
	if len(argv) == 0 {
		return fmt.Errorf("no menu program configured")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewBufferString(strings.Join(choices, "\n") + "\n")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			return // canceled
		}
		answer := strings.TrimSpace(out.String())
		if answer == "" {
			return
		}
		var follow string
		switch kind {
		case command.PromptView:
			follow = "view " + answer
		case command.PromptMoveTo:
			follow = "moveto " + answer
		case command.PromptAction:
			follow = "action " + answer
		default:
			return
		}
		b.QueueCommand(follow)
	}()
	return nil
}

// Action runs the named action from the configuration via the shell.
func (b *Backend) Action(name string) error {
	cmdline, ok := b.cfg.Actions[name]
	if !ok {
		return fmt.Errorf("unknown action %q", name)
	}
	return b.Spawn([]string{"sh", "-c", cmdline})
}

// Kill requests that the window close. Called from within a manage
// sequence (commands are drained there).
func (b *Backend) Kill(id wm.WindowID) {
	if w := b.windowByID(id); w != nil {
		w.Object.Close()
	}
}

// runAutostart executes the configured autostart commands.
func (b *Backend) runAutostart() {
	for _, cmdline := range b.cfg.Autostart {
		if err := b.Spawn([]string{"sh", "-c", cmdline}); err != nil {
			log.Printf("autostart %q: %v", cmdline, err)
		}
	}
}

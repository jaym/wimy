package backend

import (
	"log"
	"sync"

	"wimy/internal/command"
	"wimy/internal/config"
	"wimy/internal/wm"
)

// Platform is what the Core needs from the concrete backend that
// embeds it.
type Platform interface {
	// Wake asks the backend to run a manage pass soon; the backend
	// calls DrainQueue from that pass. It is called from goroutines
	// other than the backend's dispatch thread.
	Wake()
	// ApplyConfigChange applies the platform side of a config reload.
	// Core.Cfg already points at the new config when it is called.
	ApplyConfigChange(ch ConfigChange)
	// Kill asks the window to close.
	Kill(id wm.WindowID)
	// Quit exits the window manager.
	Quit()
	// Restart replaces the process with the executable on disk, handing
	// over the state (see WriteHandoff); an error means wimy keeps running.
	Restart() error
}

// Core is the platform-neutral backend state: the model, the config,
// the command registry and queue, and autostart supervision. It
// implements command.Effects.
type Core struct {
	Cfg   *config.Config
	State *wm.State
	Reg   *command.Registry

	// configArg is the -config flag value as passed at startup;
	// Reload re-runs config.Load with it.
	configArg string
	platform  Platform
	autostart Autostart

	mu    sync.Mutex
	queue []string
}

// NewCore creates the core for a backend. configArg is the config
// file path argument (as for config.Load) used by Reload.
func NewCore(cfg *config.Config, configArg string, p Platform) *Core {
	c := &Core{Cfg: cfg, State: wm.NewState(), configArg: configArg, platform: p}
	c.State.StackStrip = cfg.StackStrip
	c.State.TitlebarHeight = cfg.Titlebar.Height
	c.Reg = command.New(&command.Env{State: c.State, Fx: c})
	return c
}

// CommandNames returns the registered command names.
func (c *Core) CommandNames() []string { return c.Reg.Names() }

// Enqueue appends a command to the pending queue without waking the
// backend. It is for the backend's own dispatch thread (key
// bindings), which must not wait on itself.
func (c *Core) Enqueue(cmd string) {
	c.mu.Lock()
	c.queue = append(c.queue, cmd)
	c.mu.Unlock()
}

// QueueCommand appends a command and asks the backend for a manage
// pass, in which the queue is drained. Safe from any goroutine except
// the backend's dispatch thread.
func (c *Core) QueueCommand(cmd string) {
	c.Enqueue(cmd)
	c.platform.Wake()
}

// DrainQueue executes pending commands (key bindings, RPC, prompts)
// in the order they were queued.
func (c *Core) DrainQueue() {
	c.mu.Lock()
	queue := c.queue
	c.queue = nil
	c.mu.Unlock()
	for _, cmd := range queue {
		if err := c.Reg.Run(cmd); err != nil {
			log.Printf("command %q: %v", cmd, err)
		}
	}
}

// StartAutostart runs the configured autostart commands.
func (c *Core) StartAutostart() {
	c.autostart.StartAll(c.Cfg.Autostart)
}

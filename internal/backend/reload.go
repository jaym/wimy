package backend

import (
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"

	"wimy/internal/config"
)

// ConfigChange lists the reloaded config sections a platform must
// apply itself; everything else the Core applies.
type ConfigChange struct {
	Binds    bool // key bindings: recreate and re-enable
	Mod      bool // primary modifier: recreate pointer bindings
	Border   bool // border width or colors
	Titlebar bool // titlebar height or colors
	BarGap   bool // reserved top space: re-read screen areas
}

// notifyError surfaces a reload failure on the desktop; tests replace it.
var notifyError = notifyReloadError

// Reload implements command.Effects: re-read the configuration file
// and apply it. It runs inside the backend's manage pass (commands
// drain there). A config that fails to load leaves the old config
// active; the error is logged and surfaced on the desktop.
func (c *Core) Reload() error {
	newCfg, err := config.Load(c.configArg)
	if err != nil {
		notifyError("wimy config reload failed, keeping the old config:\n" + err.Error())
		return fmt.Errorf("reload: keeping old config: %w", err)
	}
	changes := c.applyConfig(newCfg)
	if len(changes) == 0 {
		log.Printf("config reloaded: no changes")
	} else {
		log.Printf("config reloaded: %s", strings.Join(changes, ", "))
	}
	return nil
}

// applyConfig switches to newCfg, applies every section that changed
// (the platform's through ApplyConfigChange) and returns the names of
// the changed sections. Values read live (terminal, launcher, menu,
// focus-follows-mouse, actions) take effect with the pointer swap.
func (c *Core) applyConfig(newCfg *config.Config) []string {
	old := c.Cfg
	ch := ConfigChange{
		Binds:    !slices.Equal(old.Binds, newCfg.Binds),
		Mod:      old.ModMask != newCfg.ModMask,
		Border:   old.Border != newCfg.Border,
		Titlebar: old.Titlebar != newCfg.Titlebar,
		BarGap:   old.BarGap != newCfg.BarGap,
	}
	c.Cfg = newCfg
	c.platform.ApplyConfigChange(ch)

	var changes []string
	if ch.Binds {
		changes = append(changes, "keybindings")
	}
	if ch.Mod {
		changes = append(changes, "modifier (pointer drags)")
	}
	if ch.Border {
		changes = append(changes, "border")
	}
	if ch.Titlebar {
		c.State.TitlebarHeight = newCfg.Titlebar.Height
		changes = append(changes, "titlebar")
	}
	if old.StackStrip != newCfg.StackStrip {
		c.State.StackStrip = newCfg.StackStrip
		changes = append(changes, "stack-strip")
	}
	if ch.BarGap {
		changes = append(changes, "bar-gap")
	}
	if old.Terminal != newCfg.Terminal {
		changes = append(changes, "terminal")
	}
	if old.Launcher != newCfg.Launcher {
		changes = append(changes, "launcher")
	}
	if old.Menu != newCfg.Menu {
		changes = append(changes, "menu")
	}
	if old.FocusFollowsMouse != newCfg.FocusFollowsMouse {
		changes = append(changes, "focus-follows-mouse")
	}
	if !maps.Equal(old.Actions, newCfg.Actions) {
		changes = append(changes, "actions")
	}
	// autostart: removed → killed, added → spawned, changed →
	// re-executed, exited-but-configured → restarted
	killed, spawned, restarted := c.autostart.Sync(old.Autostart, newCfg.Autostart)
	if !slices.Equal(old.Autostart, newCfg.Autostart) || killed+spawned+restarted > 0 {
		changes = append(changes, fmt.Sprintf("autostart (%d killed, %d spawned, %d restarted)",
			killed, spawned, restarted))
	}
	return changes
}

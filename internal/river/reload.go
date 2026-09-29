//go:build linux

package river

import (
	"fmt"
	"log"
	"maps"
	"os/exec"
	"slices"
	"strings"

	"wimy/internal/config"
	"wimy/internal/titlebar"
)

// newTitlebarRenderer builds the titlebar renderer from the config
// (it embeds the border colors and width for the frame).
func newTitlebarRenderer(cfg *config.Config) *titlebar.Renderer {
	return titlebar.New(cfg.Titlebar.Height, titlebar.Colors{
		FocusedBg:     toRGBA(cfg.Titlebar.FocusedBg),
		FocusedFg:     toRGBA(cfg.Titlebar.FocusedFg),
		NormalBg:      toRGBA(cfg.Titlebar.NormalBg),
		NormalFg:      toRGBA(cfg.Titlebar.NormalFg),
		BorderFocused: toRGBA(cfg.Border.Focused),
		BorderNormal:  toRGBA(cfg.Border.Normal),
	}, cfg.Border.Width)
}

// Reload implements command.Effects: re-read the configuration file
// and apply it. It runs inside a manage sequence (commands drain
// there), so binding re-creation, layout changes and border/deco
// invalidation all apply within that same sequence. A config that
// fails to load leaves the old config active; the error is logged and
// surfaced on the desktop when a notifier is available.
func (b *Backend) Reload() error {
	newCfg, err := config.Load(b.configArg)
	if err != nil {
		notifyReloadError("wimy config reload failed, keeping the old config:\n" + err.Error())
		return fmt.Errorf("reload: keeping old config: %w", err)
	}
	b.applyConfig(newCfg)
	return nil
}

// notifyReloadError surfaces a reload failure on the desktop when
// zenity or notify-send is installed; detached, best-effort.
func notifyReloadError(msg string) {
	detach := func(cmd *exec.Cmd) bool {
		if err := cmd.Start(); err != nil {
			return false
		}
		go func() { _, _ = cmd.Process.Wait() }()
		return true
	}
	if path, err := exec.LookPath("zenity"); err == nil {
		if detach(exec.Command(path, "--error", "--title=wimy", "--width=420", "--text="+msg)) {
			return
		}
	}
	if path, err := exec.LookPath("notify-send"); err == nil {
		detach(exec.Command(path, "-u", "critical", "wimy: config reload failed", msg))
	}
}

// applyConfig switches the backend to newCfg, applying every section
// that changed and logging a per-section report. Sections read live
// (terminal, launcher, menu, focus-follows-mouse, actions) take
// effect with the pointer swap alone.
func (b *Backend) applyConfig(newCfg *config.Config) {
	old := b.cfg
	var changes []string
	bindsChanged := !slices.Equal(old.Binds, newCfg.Binds)
	modChanged := old.ModMask != newCfg.ModMask
	borderChanged := old.Border != newCfg.Border
	titlebarChanged := old.Titlebar != newCfg.Titlebar

	b.cfg = newCfg

	// key bindings: destroy and recreate the protocol objects, then
	// enable them in this manage sequence (applyManage honors
	// bindsNeedEnable). A mod change re-resolves every "Mod-" combo,
	// so it implies bindsChanged via the resolved masks.
	if bindsChanged {
		for _, xb := range b.bindings {
			xb.Object.Destroy()
		}
		b.bindings = nil
		for _, s := range b.seats {
			b.createXkbBindings(s)
		}
		b.bindsNeedEnable = true
		changes = append(changes, "keybindings")
	}
	if modChanged {
		for _, pb := range b.pointerBindings {
			pb.Object.Destroy()
		}
		b.pointerBindings = nil
		for _, s := range b.seats {
			b.createPointerBindings(s)
		}
		b.bindsNeedEnable = true
		changes = append(changes, "modifier (pointer drags)")
	}

	// borders: force re-application on the next render
	if borderChanged {
		for _, w := range b.windows {
			w.BorderSet = false
		}
		changes = append(changes, "border")
	}
	// the titlebar renderer embeds the border colors and width
	if borderChanged || titlebarChanged {
		b.tbr = newTitlebarRenderer(newCfg)
	}
	if titlebarChanged {
		b.state.TitlebarHeight = newCfg.Titlebar.Height
		for _, w := range b.windows {
			if newCfg.Titlebar.Height <= 0 {
				// titlebars off: tear decorations down (also
				// resets the cached render state)
				w.destroyDeco()
			} else {
				// force re-render with the new colors/height
				w.DecoWidth = -1
			}
		}
		changes = append(changes, "titlebar")
	}
	if old.StackStrip != newCfg.StackStrip {
		b.state.StackStrip = newCfg.StackStrip
		changes = append(changes, "stack-strip")
	}

	// live-read values: applied by the swap, just report them
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

	// autostart: reconcile tracked processes with the new list
	// (removed → killed, added → spawned, changed → re-executed,
	// crashed-but-configured → restarted)
	killed, spawned, restarted := b.syncAutostart(old.Autostart, newCfg.Autostart)
	if !slices.Equal(old.Autostart, newCfg.Autostart) || killed+spawned+restarted > 0 {
		changes = append(changes, fmt.Sprintf("autostart (%d killed, %d spawned, %d restarted)",
			killed, spawned, restarted))
	}

	if len(changes) == 0 {
		log.Printf("config reloaded: no changes")
	} else {
		log.Printf("config reloaded: %s", strings.Join(changes, ", "))
	}
}

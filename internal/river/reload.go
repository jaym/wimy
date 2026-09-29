//go:build linux

package river

import (
	"wimy/internal/backend"
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

// ApplyConfigChange implements backend.Platform: it applies the parts
// of a config reload that live in protocol objects. b.Cfg already
// holds the new config. It runs inside a manage sequence, so binding
// re-creation and border/deco invalidation apply within it.
func (b *Backend) ApplyConfigChange(ch backend.ConfigChange) {
	// key bindings: destroy and recreate the protocol objects, then
	// enable them in this manage sequence (applyManage honors
	// bindsNeedEnable).
	if ch.Binds {
		for _, xb := range b.bindings {
			xb.Object.Destroy()
		}
		b.bindings = nil
		for _, s := range b.seats {
			b.createXkbBindings(s)
		}
		b.bindsNeedEnable = true
	}
	if ch.Mod {
		for _, pb := range b.pointerBindings {
			pb.Object.Destroy()
		}
		b.pointerBindings = nil
		for _, s := range b.seats {
			b.createPointerBindings(s)
		}
		b.bindsNeedEnable = true
	}
	// borders: force re-application on the next render
	if ch.Border {
		for _, w := range b.windows {
			w.BorderSet = false
		}
	}
	// the titlebar renderer embeds the border colors and width
	if ch.Border || ch.Titlebar {
		b.tbr = newTitlebarRenderer(b.Cfg)
	}
	if ch.Titlebar {
		for _, w := range b.windows {
			if b.Cfg.Titlebar.Height <= 0 {
				// titlebars off: tear decorations down (also
				// resets the cached render state)
				w.destroyDeco()
			} else {
				// force re-render with the new colors/height
				w.DecoWidth = -1
			}
		}
	}
}

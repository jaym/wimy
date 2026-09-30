package backend

import (
	"wimy/internal/config"
	"wimy/internal/titlebar"
)

// NewTitlebarRenderer builds a titlebar renderer of the given height
// from the config's titlebar and border colors and border width (the
// titlebar image draws the top of the border frame).
func NewTitlebarRenderer(cfg *config.Config, height int32) *titlebar.Renderer {
	return titlebar.New(height, titlebar.Colors{
		FocusedBg:     cfg.Titlebar.FocusedBg.RGBA(),
		FocusedFg:     cfg.Titlebar.FocusedFg.RGBA(),
		NormalBg:      cfg.Titlebar.NormalBg.RGBA(),
		NormalFg:      cfg.Titlebar.NormalFg.RGBA(),
		BorderFocused: cfg.Border.Focused.RGBA(),
		BorderNormal:  cfg.Border.Normal.RGBA(),
	}, cfg.Border.Width)
}

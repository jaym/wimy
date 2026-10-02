package backend

import (
	"strings"

	"wimy/internal/config"
	"wimy/internal/titlebar"
)

// NewTitlebarRenderer builds a titlebar renderer of the given height
// from the config's titlebar and border colors, border width (the
// titlebar image draws the top of the border frame) and font family
// list.
func NewTitlebarRenderer(cfg *config.Config, height int32) *titlebar.Renderer {
	return titlebar.New(height, titlebar.Colors{
		FocusedBg:     cfg.Titlebar.FocusedBg.RGBA(),
		FocusedFg:     cfg.Titlebar.FocusedFg.RGBA(),
		NormalBg:      cfg.Titlebar.NormalBg.RGBA(),
		NormalFg:      cfg.Titlebar.NormalFg.RGBA(),
		BorderFocused: cfg.Border.Focused.RGBA(),
		BorderNormal:  cfg.Border.Normal.RGBA(),
	}, cfg.Border.Width, fontFamilies(cfg.Titlebar.Font))
}

// fontFamilies splits a comma-separated font family list, dropping
// empty entries.
func fontFamilies(s string) []string {
	var out []string
	for f := range strings.SplitSeq(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

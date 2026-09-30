package backend

import (
	"testing"

	"wimy/internal/config"
)

func TestNewTitlebarRenderer(t *testing.T) {
	cfg := config.Default()
	r := NewTitlebarRenderer(cfg, 30)
	if r.Height != 30 || r.Border != cfg.Border.Width {
		t.Errorf("height %d border %d", r.Height, r.Border)
	}
	if r.Colors.FocusedBg != cfg.Titlebar.FocusedBg.RGBA() || r.Colors.BorderNormal != cfg.Border.Normal.RGBA() {
		t.Errorf("colors not taken from the config: %+v", r.Colors)
	}
}

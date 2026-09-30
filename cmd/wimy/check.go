package main

import (
	"fmt"

	"wimy/internal/config"
)

// check is what `wimy -check` runs: can this binary load the config?
// A restart runs it with the new binary first, so it never replaces a
// working window manager with one that can't start.
func check(configPath string) error {
	if _, err := config.Load(configPath); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

//go:build darwin

package main

import (
	"errors"

	"wimy/internal/config"
)

// newBackend will return the macOS backend (plans/macos-port.md,
// Phase 1). Until then wimy builds on darwin but refuses to start.
func newBackend(cfg *config.Config, configArg string, notify func()) (wmBackend, error) {
	return nil, errors.New("the macOS backend is not implemented yet (plans/macos-port.md, Phase 1)")
}

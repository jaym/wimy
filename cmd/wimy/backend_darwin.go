//go:build darwin && cgo

package main

import (
	"wimy/internal/config"
	"wimy/internal/macos"
)

// newBackend returns the macOS (Accessibility API) backend.
func newBackend(cfg *config.Config, configArg string, notify func()) (wmBackend, error) {
	return macos.New(cfg, configArg, notify), nil
}

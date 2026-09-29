//go:build linux

package main

import (
	"wimy/internal/config"
	"wimy/internal/river"
)

// newBackend returns the river backend.
func newBackend(cfg *config.Config, configArg string, notify func()) (wmBackend, error) {
	return river.New(cfg, configArg, notify), nil
}

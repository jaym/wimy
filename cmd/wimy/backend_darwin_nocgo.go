//go:build darwin && !cgo

package main

import (
	"errors"

	"wimy/internal/config"
)

// newBackend: the macOS backend talks to AppKit and the Accessibility
// API through cgo, so a CGO_ENABLED=0 build can't run it.
func newBackend(cfg *config.Config, configArg string, notify func()) (wmBackend, error) {
	return nil, errors.New("the macOS backend needs cgo: build with CGO_ENABLED=1")
}

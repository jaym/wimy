//go:build !darwin

package backend

import "fmt"

// appDirs: there is no built-in app picker outside macOS.
func appDirs() []string { return nil }

// LaunchApp is macOS-only (Linux launches through the `launcher`).
func (c *Core) LaunchApp(name string) error {
	return fmt.Errorf("launch: only on macOS; use the launcher (Mod-p)")
}

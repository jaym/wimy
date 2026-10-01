//go:build darwin

package backend

import (
	"os"
	"path/filepath"
)

// appDirs are the folders the macOS app picker (Mod-p without a
// launcher) lists applications from.
func appDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		"/Applications",
		"/System/Applications",
		filepath.Join(home, "Applications"),
	}
}

// LaunchApp starts the named application, or brings it to the front if
// it runs (open -a resolves the name through Launch Services).
func (c *Core) LaunchApp(name string) error {
	return c.Spawn([]string{"open", "-a", name})
}

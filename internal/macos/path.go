package macos

import (
	"path/filepath"
	"slices"
	"strings"
)

// systemPath is what launchd gives its agents (wimy's login agent
// included): no Homebrew, no Nix.
var systemPath = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}

// loginPath extends PATH (cur) with the folders Homebrew and Nix
// install programs into, so the menu (choose), terminal, launcher and
// actions are found when wimy runs as a launchd agent. The inherited
// entries keep their order and come first, then the missing Nix and
// Homebrew folders, then the system folders.
func loginPath(cur, home, user string) string {
	extra := []string{
		filepath.Join(home, ".nix-profile/bin"),
		"/etc/profiles/per-user/" + user + "/bin", // home-manager under nix-darwin
		"/run/current-system/sw/bin",
		"/nix/var/nix/profiles/default/bin",
		"/opt/homebrew/bin",
		"/usr/local/bin",
	}
	var out []string
	add := func(dirs ...string) {
		for _, d := range dirs {
			if d != "" && !slices.Contains(out, d) {
				out = append(out, d)
			}
		}
	}
	for _, d := range strings.Split(cur, ":") {
		if !slices.Contains(systemPath, d) {
			add(d)
		}
	}
	add(extra...)
	add(systemPath...)
	return strings.Join(out, ":")
}

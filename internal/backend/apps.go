package backend

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// listApps returns the names of the application bundles (Name.app) in
// dirs and in their subfolders one level down (/Applications/Utilities),
// sorted, each name once. It skips hidden ones (helpers such as
// Karabiner's), doesn't look inside bundles, follows symlinked folders
// (~/Applications/Home Manager Apps) and skips folders it can't read.
func listApps(dirs []string) []string {
	seen := map[string]bool{}
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue // hidden: helpers such as Karabiner's
			}
			path := filepath.Join(dir, e.Name())
			info, err := os.Stat(path) // follows symlinks
			if err != nil || !info.IsDir() {
				continue
			}
			if name, ok := strings.CutSuffix(e.Name(), ".app"); ok {
				seen[name] = true
			} else if depth > 0 {
				walk(path, depth-1)
			}
		}
	}
	for _, d := range dirs {
		walk(d, 1)
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

package backend

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListApps(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	mkdirs(t, a,
		"Ghostty.app/Contents/MacOS",
		"Utilities/Terminal.app",                          // one folder down, like /Applications/Utilities
		"Visual Studio Code.app",                          // spaces
		"Xcode.app/Contents/Applications/Instruments.app", // inside an app: not listed
		"Deep/Er/Hidden.app",                              // two folders down: not listed
		"NotAnApp",
		".Karabiner-VirtualHIDDevice-Manager.app", // hidden helper
	)
	if err := os.WriteFile(filepath.Join(a, "notes.app.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	mkdirs(t, b, "Ghostty.app", "Safari.app") // Ghostty again: listed once
	got := listApps([]string{a, b, filepath.Join(a, "missing")})
	want := []string{"Ghostty", "Safari", "Terminal", "Visual Studio Code", "Xcode"}
	if !slices.Equal(got, want) {
		t.Errorf("listApps = %q, want %q", got, want)
	}
}

func TestListAppsFollowsALinkedFolder(t *testing.T) {
	// ~/Applications/Home Manager Apps is a symlink into the Nix store
	real, home := t.TempDir(), t.TempDir()
	mkdirs(t, real, "Firefox.app")
	if err := os.Symlink(real, filepath.Join(home, "Home Manager Apps")); err != nil {
		t.Fatal(err)
	}
	if got := listApps([]string{home}); !slices.Equal(got, []string{"Firefox"}) {
		t.Errorf("listApps = %q", got)
	}
}

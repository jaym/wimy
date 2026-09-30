package rpc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSocketPath(t *testing.T) {
	env := func(kv map[string]string) func(string) string {
		return func(k string) string { return kv[k] }
	}
	cases := []struct {
		name string
		goos string
		env  map[string]string
		want string
	}{
		{"override wins", "darwin", map[string]string{"WIMY_SOCKET": "/x.sock", "WAYLAND_DISPLAY": "w"}, "/x.sock"},
		{"linux session", "linux", map[string]string{"XDG_RUNTIME_DIR": "/run/user/1", "WAYLAND_DISPLAY": "wayland-1"}, "/run/user/1/wimy-wayland-1.sock"},
		{"linux tty falls back to wayland-0", "linux", map[string]string{"XDG_RUNTIME_DIR": "/run/user/1"}, "/run/user/1/wimy-wayland-0.sock"},
		{"linux no runtime dir", "linux", map[string]string{}, "/tmp/wimy-wayland-0.sock"},
		{"darwin", "darwin", map[string]string{}, "/tmp/wimy-501/wimy.sock"},
		{"darwin ignores TMPDIR", "darwin", map[string]string{"TMPDIR": "/var/folders/x/T/"}, "/tmp/wimy-501/wimy.sock"},
		{"darwin running a wayland compositor", "darwin", map[string]string{"XDG_RUNTIME_DIR": "/r", "WAYLAND_DISPLAY": "w"}, "/r/wimy-w.sock"},
	}
	for _, c := range cases {
		if got := socketPath(c.goos, env(c.env), "/tmp", 501); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPrepareSocketDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "wimy-test")
	if err := prepareSocketDir(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, %v; want 0700", fi.Mode().Perm(), err)
	}
	if err := prepareSocketDir(dir); err != nil {
		t.Errorf("existing private dir rejected: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := prepareSocketDir(dir); err == nil {
		t.Errorf("dir readable by others accepted")
	}
}

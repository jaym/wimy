package rpc

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"wimy/internal/wm"
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

type fakeBackend struct{}

func (fakeBackend) QueueCommand(string)         {}
func (fakeBackend) CommandNames() []string      { return nil }
func (fakeBackend) Snapshot(fn func(*wm.State)) { fn(wm.NewState()) }

// shortSocket returns a socket path short enough for sun_path.
func shortSocket(t *testing.T) string {
	dir, err := os.MkdirTemp("/tmp", "wimyrpc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

func TestVersionAndRunning(t *testing.T) {
	path := shortSocket(t)
	t.Setenv("WIMY_SOCKET", path)
	started := time.Unix(1700000000, 0)
	srv, err := Listen(fakeBackend{}, Info{Version: "v1.2.3", Started: started})
	if err != nil {
		t.Fatal(err)
	}
	if !Running(path) {
		t.Errorf("Running = false for a live server")
	}
	res, conn, err := Call(path, "version", nil)
	if conn != nil {
		conn.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	var v VersionInfo
	if err := json.Unmarshal(res, &v); err != nil || v.Version != "v1.2.3" || !v.Started.Equal(started) || v.PID != os.Getpid() {
		t.Errorf("version = %+v, %v", v, err)
	}
	srv.Close()
	if Running(path) {
		t.Errorf("Running = true after Close")
	}
}

func TestAnotherInstanceRunning(t *testing.T) {
	path := shortSocket(t)
	t.Setenv("WIMY_SOCKET", path)
	srv, err := Listen(fakeBackend{}, Info{Version: "a"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if _, err := Listen(fakeBackend{}, Info{Version: "b"}); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("second Listen: %v, want ErrAlreadyRunning (it must not take the socket over)", err)
	}
	if !Running(path) {
		t.Errorf("first server lost its socket")
	}
}

func TestRunningOlderWimy(t *testing.T) {
	// a wimy from before the version method answers with an RPC error:
	// it is running all the same, and its socket must not be taken over
	path := shortSocket(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			bufio.NewReader(c).ReadBytes('\n')
			c.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"unknown method"}}` + "\n"))
			c.Close()
		}
	}()
	if !Running(path) {
		t.Errorf("an answering older wimy counted as not running")
	}
}

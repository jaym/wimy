package macos

import (
	"os"
	"path/filepath"
	"testing"

	"wimy/internal/wm"
)

func TestHiddenStoreRoundTrip(t *testing.T) {
	s := hiddenStore{path: filepath.Join(t.TempDir(), "sub", "hidden.json")}
	if m, err := s.load(); err != nil || len(m) != 0 {
		t.Fatalf("missing file: %v %v", m, err)
	}
	want := map[wm.WindowID]wm.Rect{44: {X: 0, Y: 37, W: 756, H: 945}, 60: {X: 756, Y: 37, W: 756, H: 945}}
	if err := s.save(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.load()
	if err != nil || len(got) != 2 || got[44] != want[44] || got[60] != want[60] {
		t.Fatalf("load = %v, %v", got, err)
	}
	if err := s.save(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Errorf("empty save left the file: %v", err)
	}
}

func TestHiddenStoreCorruptFile(t *testing.T) {
	s := hiddenStore{path: filepath.Join(t.TempDir(), "hidden.json")}
	if err := os.WriteFile(s.path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.load(); err == nil {
		t.Errorf("corrupt store loaded without error")
	}
}

func TestStateDir(t *testing.T) {
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	if got := stateDir(env(map[string]string{"XDG_STATE_HOME": "/s"}), "/Users/me"); got != "/s/wimy" {
		t.Errorf("XDG_STATE_HOME: %q", got)
	}
	if got := stateDir(env(nil), "/Users/me"); got != "/Users/me/.local/state/wimy" {
		t.Errorf("default: %q", got)
	}
}

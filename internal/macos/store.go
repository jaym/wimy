package macos

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"wimy/internal/wm"
)

// hiddenStore persists the last on-screen frame of every window wimy
// has parked in a hide corner, so a wimy that died (SIGKILL, crash in
// C) can put them back on its next start. Entries are tagged with the
// boot time: window IDs start over after a reboot, so entries from
// another boot would match unrelated windows.
type hiddenStore struct{ path string }

type storedWindow struct {
	ID   wm.WindowID `json:"id"`
	Rect wm.Rect     `json:"rect"`
}

// save writes m atomically for the given boot; an empty m removes the
// file.
func (s hiddenStore) save(m map[wm.WindowID]wm.Rect, boot int64) error {
	if len(m) == 0 {
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	list := make([]storedWindow, 0, len(m))
	for id, r := range m {
		list = append(list, storedWindow{ID: id, Rect: r})
	}
	data, err := json.Marshal(storeDoc{Boot: boot, Windows: list})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

type storeDoc struct {
	Boot    int64          `json:"boot"`
	Windows []storedWindow `json:"windows"`
}

// load reads the store; a missing file, or one from another boot, is
// an empty store.
func (s hiddenStore) load(boot int64) (map[wm.WindowID]wm.Rect, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[wm.WindowID]wm.Rect{}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc storeDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Boot != boot {
		return map[wm.WindowID]wm.Rect{}, nil
	}
	m := make(map[wm.WindowID]wm.Rect, len(doc.Windows))
	for _, w := range doc.Windows {
		m[w.ID] = w.Rect
	}
	return m, nil
}

// stateDir is $XDG_STATE_HOME/wimy, else ~/.local/state/wimy.
func stateDir(getenv func(string) string, home string) string {
	if d := getenv("XDG_STATE_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, "wimy")
	}
	return filepath.Join(home, ".local", "state", "wimy")
}

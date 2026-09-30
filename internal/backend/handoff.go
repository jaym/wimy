package backend

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"wimy/internal/wm"
)

// handoffMaxAge bounds how old a handoff may be: a restart reads it
// within a second; an older one is left over from a crash.
const handoffMaxAge = 2 * time.Minute

// Handoff is what a restarting wimy passes to the one that replaces it
// (the same process after exec): the whole model, the autostart
// children to adopt, and platform extras.
type Handoff struct {
	Boot      int64            `json:"boot"` // boot time: window ids restart after a reboot
	Written   time.Time        `json:"written"`
	Model     json.RawMessage  `json:"model"`
	Autostart map[string][]int `json:"autostart,omitempty"` // pid 0: exited
	// AutostartConfig is the old process's autostart list: the new one
	// reconciles the adopted children with its own config against it.
	AutostartConfig []string        `json:"autostart_config,omitempty"`
	Platform        json.RawMessage `json:"platform,omitempty"`
}

// WriteHandoff saves the handoff for a restart to path, atomically.
// platform (any JSON-encodable value, or nil) carries backend state
// the model doesn't hold.
func (c *Core) WriteHandoff(path string, boot int64, platform any) error {
	model, err := json.Marshal(c.State)
	if err != nil {
		return err
	}
	h := Handoff{Boot: boot, Written: time.Now(), Model: model,
		Autostart: c.autostart.Entries(), AutostartConfig: c.Cfg.Autostart}
	if platform != nil {
		if h.Platform, err = json.Marshal(platform); err != nil {
			return err
		}
	}
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadHandoff applies the handoff at path, if there is a fresh one from
// this boot: the model replaces c.State (keeping the pointer, which the
// command registry holds, and the config-derived fields of this
// process), the autostart children are adopted, and the platform
// extras are decoded into platform (if non-nil). The file is removed
// in every case, so a handoff is applied at most once. It reports
// whether it applied one.
func (c *Core) ReadHandoff(path string, boot int64, now time.Time, platform any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_ = os.Remove(path)
	var h Handoff
	if err := json.Unmarshal(data, &h); err != nil {
		return false, err
	}
	if h.Boot != boot || now.Sub(h.Written) > handoffMaxAge || now.Before(h.Written.Add(-time.Minute)) {
		return false, nil
	}
	var st wm.State
	if err := json.Unmarshal(h.Model, &st); err != nil {
		return false, err
	}
	if st.Windows == nil {
		st.Windows = make(map[wm.WindowID]*wm.Window)
	}
	st.StackStrip = c.Cfg.StackStrip
	st.TitlebarHeight = c.Cfg.Titlebar.Height
	*c.State = st
	for cmdline, pids := range h.Autostart {
		for _, pid := range pids {
			c.autostart.Adopt(cmdline, pid)
		}
	}
	// this process may have loaded a config the old one never reloaded:
	// start what it adds, stop what it drops, revive what had died
	c.autostart.Sync(h.AutostartConfig, c.Cfg.Autostart)
	if platform != nil && len(h.Platform) > 0 {
		if err := json.Unmarshal(h.Platform, platform); err != nil {
			return true, err
		}
	}
	return true, nil
}

// PruneWindows removes every window of the model that present rejects
// (it closed while wimy restarted) and returns their ids, sorted.
func (c *Core) PruneWindows(present func(wm.WindowID) bool) []wm.WindowID {
	var gone []wm.WindowID
	for id := range c.State.Windows {
		if !present(id) {
			gone = append(gone, id)
		}
	}
	slices.Sort(gone)
	for _, id := range gone {
		c.State.RemoveWindow(id)
	}
	return gone
}

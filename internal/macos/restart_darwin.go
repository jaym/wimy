//go:build darwin && cgo

package macos

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"wimy/internal/wm"
)

// macHandoff is the macOS backend's part of a restart handoff: what it
// knows beyond the model.
type macHandoff struct {
	Outputs   []outputSpec             `json:"outputs"` // keeps output names stable
	Hidden    map[wm.WindowID]wm.Rect  `json:"hidden"`  // saved frames of parked windows
	Parked    []wm.WindowID            `json:"parked"`
	LastShown map[wm.WindowID]wm.Rect  `json:"last_shown"`
	Away      map[wm.WindowID][]string `json:"away"` // minimized windows' views
}

func (b *Backend) handoffPath() string {
	return filepath.Join(filepath.Dir(b.store.path), "handoff.json")
}

// restart replaces this process with the executable on disk (an
// updated Wimy.app), handing over the whole state. Parked windows stay
// parked (and in the hidden store, should the successor never start).
// On failure wimy keeps running.
func (b *Backend) restart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	parked := make([]wm.WindowID, 0, len(b.parked))
	for id := range b.parked {
		parked = append(parked, id)
	}
	slices.Sort(parked)
	h := macHandoff{Outputs: b.outputs, Hidden: b.hidden, Parked: parked, LastShown: b.lastShown, Away: b.away}
	path := b.handoffPath()
	if err := b.WriteHandoff(path, b.boot, h); err != nil {
		return fmt.Errorf("restart: %w", err)
	}
	b.saveHidden()
	log.Printf("restarting: exec %s", exe)
	err = syscall.Exec(exe, os.Args, os.Environ())
	_ = os.Remove(path)
	return fmt.Errorf("restart: exec %s: %w", exe, err)
}

// takeHandoff applies a handoff left by the wimy this process replaced.
// It reports whether there was one.
func (b *Backend) takeHandoff() bool {
	var h macHandoff
	ok, err := b.ReadHandoff(b.handoffPath(), b.boot, timeNow(), &h)
	if err != nil {
		log.Printf("restart handoff: %v (starting fresh)", err)
		return false
	}
	if !ok {
		return false
	}
	b.outputs = h.Outputs
	if h.Hidden != nil {
		b.hidden = h.Hidden
	}
	for _, id := range h.Parked {
		b.parked[id] = true
	}
	if h.LastShown != nil {
		b.lastShown = h.LastShown
	}
	if h.Away != nil {
		b.away = h.Away
	}
	log.Printf("restarted: took over %d windows, %d parked", len(b.State.Windows), len(h.Parked))
	return true
}

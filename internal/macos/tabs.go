package macos

import (
	"slices"

	"wimy/internal/wm"
)

// macOS native tabs (Ghostty, Terminal, Finder, …) are separate windows,
// but an app's AX window list holds only the visible tab of each tab
// group: switching or adding a tab makes one window leave the list and
// another join it, without either being created or destroyed in a way
// the tiling could tell apart from real windows. wimy keeps one tile per
// tab group: the joining window takes the leaving one's place.

// tabChanges compares an app's tiled windows with the windows it lists
// now. tabbed are windows wimy set aside as background tabs; adding is a
// window being added right now (0 if none). Leaving windows are paired,
// in id order, with joining ones (pairs: old, new); unpaired leaving
// windows are set aside (hide), unpaired joining ones come back (show).
func tabChanges(tiled, listed, tabbed []wm.WindowID, adding wm.WindowID) (pairs [][2]wm.WindowID, hide, show []wm.WindowID) {
	var leaving, joining []wm.WindowID
	for _, id := range tiled {
		if !slices.Contains(listed, id) {
			leaving = append(leaving, id)
		}
	}
	for _, id := range listed {
		if slices.Contains(tiled, id) {
			continue
		}
		if id == adding || slices.Contains(tabbed, id) {
			joining = append(joining, id)
		}
	}
	slices.Sort(leaving)
	slices.Sort(joining)
	n := min(len(leaving), len(joining))
	for i := 0; i < n; i++ {
		pairs = append(pairs, [2]wm.WindowID{leaving[i], joining[i]})
	}
	if len(leaving) > n {
		hide = leaving[n:]
	}
	if len(joining) > n {
		show = joining[n:]
	}
	return pairs, hide, show
}

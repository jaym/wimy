package macos

import (
	"reflect"
	"testing"

	"wimy/internal/wm"
)

func ids(v ...wm.WindowID) []wm.WindowID { return v }

func TestTabChangesNewTab(t *testing.T) {
	// window 3862 got a tab: the new tab 3865 is listed, 3862 isn't
	pairs, hide, show := tabChanges(ids(3862, 44), ids(3865, 44), nil, 3865)
	if !reflect.DeepEqual(pairs, [][2]wm.WindowID{{3862, 3865}}) || hide != nil || show != nil {
		t.Errorf("pairs %v hide %v show %v, want 3865 in 3862's place", pairs, hide, show)
	}
}

func TestTabChangesSwitchTab(t *testing.T) {
	pairs, _, _ := tabChanges(ids(3865, 44), ids(3862, 44), ids(3862), 0)
	if !reflect.DeepEqual(pairs, [][2]wm.WindowID{{3865, 3862}}) {
		t.Errorf("switching tabs: pairs %v, want 3862 in 3865's place", pairs)
	}
}

func TestTabChangesCloseTab(t *testing.T) {
	// 3865 (tiled) was closed; its sibling 3862 is listed again
	pairs, _, _ := tabChanges(ids(3865, 44), ids(3862, 44), ids(3862), 0)
	if !reflect.DeepEqual(pairs, [][2]wm.WindowID{{3865, 3862}}) {
		t.Errorf("closing a tab: pairs %v, want 3862 in its place", pairs)
	}
}

func TestTabChangesNewWindowIsNotATab(t *testing.T) {
	pairs, hide, show := tabChanges(ids(44), ids(44, 50), nil, 50)
	if pairs != nil || hide != nil || !reflect.DeepEqual(show, ids(50)) {
		t.Errorf("a plain new window: pairs %v hide %v show %v", pairs, hide, show)
	}
}

func TestTabChangesLoneVanishAndReturn(t *testing.T) {
	// a tiled window drops out of the list with nothing to replace it
	if _, hide, _ := tabChanges(ids(44, 60), ids(44), nil, 0); !reflect.DeepEqual(hide, ids(60)) {
		t.Errorf("hide = %v, want [60]", hide)
	}
	// a background tab is listed again with nothing leaving
	if _, _, show := tabChanges(ids(44), ids(44, 60), ids(60), 0); !reflect.DeepEqual(show, ids(60)) {
		t.Errorf("show = %v, want [60]", show)
	}
}

func TestTabChangesIgnoresUntrackedListed(t *testing.T) {
	// listed windows that are neither tabs wimy set aside nor the one
	// being added (minimized ones, say) don't count as joined
	if pairs, _, show := tabChanges(ids(44), ids(44, 70), nil, 0); pairs != nil || show != nil {
		t.Errorf("pairs %v show %v, want nothing", pairs, show)
	}
}

package macos

import (
	"testing"

	"wimy/internal/wm"
)

var laptop = wm.Rect{X: 0, Y: 0, W: 1512, H: 982}

func TestHidePositionBottomRight(t *testing.T) {
	x, y := hidePosition([]wm.Rect{laptop}, 0, 800, 600)
	if x != 1511 || y != 981 {
		t.Errorf("hide at %d,%d, want 1511,981 (1pt of the window stays on screen)", x, y)
	}
}

func TestHidePositionAvoidsNeighbor(t *testing.T) {
	right := wm.Rect{X: 1512, Y: -725, W: 2560, H: 1707} // BenQ to the right
	x, y := hidePosition([]wm.Rect{laptop, right}, 0, 800, 600)
	if x != -799 || y != 981 {
		t.Errorf("hide at %d,%d, want bottom-left -799,981 (bottom-right would be on the BenQ)", x, y)
	}
	// the BenQ itself has nothing to its right or below
	x, y = hidePosition([]wm.Rect{laptop, right}, 1, 800, 600)
	if x != 1512+2560-1 || y != -725+1707-1 {
		t.Errorf("BenQ hide at %d,%d", x, y)
	}
}

func TestHidePositionMiddleScreen(t *testing.T) {
	// three screens in a row: both corners of the middle one are taken,
	// so park in a free corner of another screen
	left := wm.Rect{X: -1920, Y: 0, W: 1920, H: 1080}
	right := wm.Rect{X: 1512, Y: 0, W: 1920, H: 1080}
	screens := []wm.Rect{laptop, left, right}
	x, y := hidePosition(screens, 0, 800, 600)
	spot := wm.Rect{X: x, Y: y - maxClamp, W: 800, H: 600 + maxClamp}
	for i, s := range screens {
		if intersects(s, spot) && !(x == s.X+s.W-1 || x == s.X-800+1) {
			t.Errorf("parked at %d,%d overlaps screen %d (%+v)", x, y, i, s)
		}
	}
	if !inHideCorner(screens, wm.Rect{X: x, Y: y, W: 800, H: 600}) {
		t.Errorf("parking spot %d,%d not recognized as a hide corner", x, y)
	}
}

func TestHidePositionClampReachesNeighbor(t *testing.T) {
	// the right neighbor ends 20pt above the laptop's bottom edge: the
	// unclamped spot misses it, but macOS pushes the window up into it
	right := wm.Rect{X: 1512, Y: 0, W: 1920, H: 962}
	x, y := hidePosition([]wm.Rect{laptop, right}, 0, 800, 600)
	if x != -799 || y != 981 {
		t.Errorf("hide at %d,%d, want bottom-left -799,981", x, y)
	}
}

func TestInHideCorner(t *testing.T) {
	screens := []wm.Rect{laptop}
	if !inHideCorner(screens, wm.Rect{X: 1511, Y: 981, W: 800, H: 600}) {
		t.Errorf("window at the bottom-right hide spot not recognized")
	}
	if !inHideCorner(screens, wm.Rect{X: -799, Y: 980, W: 800, H: 600}) {
		t.Errorf("window at the bottom-left hide spot (1pt clamp) not recognized")
	}
	if inHideCorner(screens, wm.Rect{X: 100, Y: 100, W: 800, H: 600}) {
		t.Errorf("ordinary window taken for hidden")
	}
}

func TestCenteredIn(t *testing.T) {
	area := wm.Rect{X: 0, Y: 37, W: 1512, H: 945}
	if got, want := centeredIn(area, 800, 600), (wm.Rect{X: 356, Y: 209, W: 800, H: 600}); got != want {
		t.Errorf("centeredIn = %+v, want %+v", got, want)
	}
	if got := centeredIn(area, 3000, 2000); got != area {
		t.Errorf("oversized window = %+v, want the whole area %+v", got, area)
	}
}

func TestInHideCornerAfterMacOSClamp(t *testing.T) {
	// observed on macOS 26: parked at x=-503 (bottom-left, 1pt showing),
	// requested y=981 but macOS clamped it to 942
	screens := []wm.Rect{laptop, {X: 1512, Y: 0, W: 1920, H: 1280}}
	if !inHideCorner(screens, wm.Rect{X: -503, Y: 942, W: 504, H: 945}) {
		t.Errorf("clamped parked window not recognized")
	}
	if inHideCorner(screens, wm.Rect{X: 100, Y: 942, W: 504, H: 40}) {
		t.Errorf("ordinary window near the bottom edge taken for parked")
	}
}

func TestOnScreenKeepsVisibleFrame(t *testing.T) {
	outs := outputsFor(twoScreens(0), 37)
	r := wm.Rect{X: 2000, Y: 100, W: 600, H: 400} // on the BenQ
	if got := onScreen(outs, r); got != r {
		t.Errorf("frame on a connected screen moved to %+v", got)
	}
}

func TestOnScreenScreenGone(t *testing.T) {
	// parked while docked; the BenQ has since been unplugged
	laptopOnly := outputsFor(twoScreens(0)[:1], 37)
	r := wm.Rect{X: 2000, Y: 100, W: 600, H: 400}
	want := centeredIn(laptopOnly[0].Usable, 600, 400)
	if got := onScreen(laptopOnly, r); got != want {
		t.Errorf("frame on an unplugged screen = %+v, want %+v on the primary screen", got, want)
	}
}

func TestBackOnScreen(t *testing.T) {
	screens := []wm.Rect{laptop}
	if !backOnScreen(screens, wm.Rect{X: 0, Y: 37, W: 700, H: 900}, true) {
		t.Errorf("window in the layout not confirmed")
	}
	if backOnScreen(screens, wm.Rect{X: 1511, Y: 942, W: 700, H: 900}, true) {
		t.Errorf("window still in the hide corner confirmed")
	}
	if backOnScreen(screens, wm.Rect{}, false) {
		t.Errorf("unreadable frame confirmed")
	}
}

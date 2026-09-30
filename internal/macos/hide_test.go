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

func TestHidePositionBothCornersTaken(t *testing.T) {
	left := wm.Rect{X: -1920, Y: 0, W: 1920, H: 1080}
	right := wm.Rect{X: 1512, Y: 0, W: 1920, H: 1080}
	x, y := hidePosition([]wm.Rect{laptop, left, right}, 0, 800, 600)
	if x != 1511 || y != 981 {
		t.Errorf("hide at %d,%d, want the bottom-right fallback", x, y)
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

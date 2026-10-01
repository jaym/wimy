package macos

import "testing"

// feed runs touch samples (fingers, average x) through a detector and
// returns the directions it fired.
func feed(d *swipeDetector, samples ...[2]float64) []string {
	var out []string
	for _, s := range samples {
		if dir := d.touch(int(s[0]), s[1]); dir != "" {
			out = append(out, dir)
		}
	}
	return out
}

func TestSwipeDirections(t *testing.T) {
	var d swipeDetector
	// figures from the macOS spike: a left swipe moved 0.27, a right one 0.20
	if got := feed(&d, [2]float64{3, 0.70}, [2]float64{3, 0.60}, [2]float64{3, 0.44}, [2]float64{0, 0}); len(got) != 1 || got[0] != "left" {
		t.Errorf("left swipe: %v", got)
	}
	if got := feed(&d, [2]float64{3, 0.33}, [2]float64{3, 0.45}, [2]float64{3, 0.54}, [2]float64{0, 0}); len(got) != 1 || got[0] != "right" {
		t.Errorf("right swipe: %v", got)
	}
}

func TestSwipeShortSwipes(t *testing.T) {
	// the shortest real swipes measured on macOS 26 moved 0.07-0.10
	for _, dx := range []float64{0.073, -0.104, 0.118} {
		var d swipeDetector
		if got := feed(&d, [2]float64{3, 0.5}, [2]float64{3, 0.5 + dx}); len(got) != 1 {
			t.Errorf("swipe of %+.3f fired %v", dx, got)
		}
	}
}

func TestSwipeFiresOncePerGesture(t *testing.T) {
	var d swipeDetector
	got := feed(&d, [2]float64{3, 0.9}, [2]float64{3, 0.7}, [2]float64{3, 0.5}, [2]float64{3, 0.3}, [2]float64{3, 0.1})
	if len(got) != 1 {
		t.Errorf("long swipe fired %v", got)
	}
	// lifting the fingers ends it: the next swipe fires again
	if got := feed(&d, [2]float64{0, 0}, [2]float64{3, 0.9}, [2]float64{3, 0.7}); len(got) != 1 {
		t.Errorf("second swipe fired %v", got)
	}
}

func TestSwipeIgnoresSmallAndOtherFingerCounts(t *testing.T) {
	var d swipeDetector
	// fingers resting or barely moving aren't a swipe
	if got := feed(&d, [2]float64{3, 0.64}, [2]float64{3, 0.62}, [2]float64{3, 0.60}); len(got) != 0 {
		t.Errorf("small move fired %v", got)
	}
	d = swipeDetector{}
	// two fingers scroll, four belong to macOS
	for _, n := range []float64{1, 2, 4} {
		if got := feed(&d, [2]float64{n, 0.9}, [2]float64{n, 0.5}, [2]float64{n, 0.1}); len(got) != 0 {
			t.Errorf("%v fingers fired %v", n, got)
		}
	}
	// a finger landing mid-swipe starts over from where the three are
	d = swipeDetector{}
	if got := feed(&d, [2]float64{2, 0.9}, [2]float64{3, 0.5}, [2]float64{3, 0.45}); len(got) != 0 {
		t.Errorf("third finger landing fired %v", got)
	}
}

func TestTapRouterSwipes(t *testing.T) {
	var r tapRouter
	if cmd := r.touch(3, 0.9); cmd != "" {
		t.Errorf("zero router swiped %q", cmd)
	}
	r.set(nil, map[string]string{"left": "view-next"})
	if r.active() {
		t.Errorf("swipes activate the key tap (they have their own)")
	}
	var got []string
	for _, x := range []float64{0.9, 0.7, 0.5} {
		if cmd := r.touch(3, x); cmd != "" {
			got = append(got, cmd)
		}
	}
	if len(got) != 1 || got[0] != "view-next" {
		t.Errorf("left swipe ran %v", got)
	}
	// no command for right: nothing runs
	r.touch(0, 0)
	for _, x := range []float64{0.1, 0.3, 0.5} {
		if cmd := r.touch(3, x); cmd != "" {
			t.Errorf("unbound right swipe ran %q", cmd)
		}
	}
}

package macos

// swipeThreshold is how far (in trackpad widths) three fingers must
// move sideways to make a swipe. Real swipes measured on macOS 26
// moved 0.07-0.27.
const swipeThreshold = 0.06

// swipeDetector turns trackpad touch samples into three-finger
// swipes. It is fed from the gesture tap, on the main thread.
type swipeDetector struct {
	tracking bool    // three fingers down
	startX   float64 // their average x when the gesture started
	fired    bool    // this gesture already swiped
}

// touch takes one sample: how many fingers touch and their average x
// (0 left edge, 1 right edge). It returns "left" or "right" the first
// time three fingers have moved that far that way, else "". Any other
// finger count ends the gesture.
func (d *swipeDetector) touch(fingers int, x float64) string {
	if fingers != 3 {
		*d = swipeDetector{}
		return ""
	}
	if !d.tracking {
		*d = swipeDetector{tracking: true, startX: x}
		return ""
	}
	if d.fired {
		return ""
	}
	switch dx := x - d.startX; {
	case dx <= -swipeThreshold:
		d.fired = true
		return "left"
	case dx >= swipeThreshold:
		d.fired = true
		return "right"
	}
	return ""
}

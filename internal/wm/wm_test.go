package wm

import (
	"reflect"
	"testing"
)

// newTestState returns a state with one 1280x720 output and one window
// (id 1) on view "1".
func newTestState(t *testing.T) *State {
	t.Helper()
	s := NewState()
	o := s.AddOutput("HDMI-A-1")
	o.Rect = Rect{X: 0, Y: 0, W: 1280, H: 720}
	s.AddWindow(1, false)
	if s.Focused != 1 {
		t.Fatalf("expected window 1 focused, got %d", s.Focused)
	}
	return s
}

func placements(s *State) map[WindowID]Placement {
	m := make(map[WindowID]Placement)
	for _, p := range s.Layout() {
		m[p.ID] = p
	}
	return m
}

func TestAddWindowInheritsTags(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	if !s.Windows[2].HasTag("1") {
		t.Fatalf("window 2 should inherit tag 1, got %v", s.Windows[2].TagList())
	}
	s.TagSpec("+web")
	s.AddWindow(3, false)
	if !s.Windows[3].HasTag("1") || !s.Windows[3].HasTag("web") {
		t.Fatalf("window 3 should inherit tags 1+web, got %v", s.Windows[3].TagList())
	}
}

func TestDefaultLayout(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.AddWindow(3, false)
	p := placements(s)
	want := map[WindowID]Rect{
		1: {0, 0, 1280, 240},
		2: {0, 240, 1280, 240},
		3: {0, 480, 1280, 240},
	}
	for id, r := range want {
		if p[id].Rect != r {
			t.Errorf("window %d: got %v, want %v", id, p[id].Rect, r)
		}
		if p[id].Hidden || p[id].Collapsed {
			t.Errorf("window %d: unexpected hidden/collapsed", id)
		}
	}
}

func TestDefaultLayoutRemainder(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false) // 2 windows in col: 360/360
	s.AddWindow(3, false)
	s.MoveDir(DirRight) // window 3 into its own column
	p := placements(s)
	// column 1: windows 1,2 stacked 360 each; column 2: window 3 full
	if p[1].Rect != (Rect{0, 0, 640, 360}) || p[2].Rect != (Rect{0, 360, 640, 360}) {
		t.Errorf("column 1 wrong: %v %v", p[1].Rect, p[2].Rect)
	}
	if p[3].Rect != (Rect{640, 0, 640, 720}) {
		t.Errorf("column 2 wrong: %v", p[3].Rect)
	}
}

func TestStackMode(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.AddWindow(3, false)
	s.StackStrip = 28
	// focused is 3 (newest), order in column: 1,2,3
	s.SetMode(ModeStack)
	p := placements(s)
	// focused window 3: strips for 1,2 above -> focusY = 56, H = 720-56
	if p[3].Rect != (Rect{0, 56, 1280, 664}) || p[3].Collapsed {
		t.Errorf("focused window wrong: %+v", p[3])
	}
	// windows 1,2 collapsed to strips at top
	if p[1].Rect.Y != 0 || !p[1].Collapsed {
		t.Errorf("strip 1 wrong: %+v", p[1])
	}
	if p[2].Rect.Y != 28 || !p[2].Collapsed {
		t.Errorf("strip 2 wrong: %+v", p[2])
	}
	// strip windows propose focused dims
	if p[1].Rect.H != 664 {
		t.Errorf("strip should propose focused height, got %d", p[1].Rect.H)
	}
}

func TestStackModeTitlebars(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.AddWindow(3, false)
	s.TitlebarHeight = 22
	s.SetMode(ModeStack)
	p := placements(s)
	// strip height = titlebar height; content sits below the titlebar
	if !p[1].Collapsed || p[1].Rect.Y != 22 || p[1].Rect.H != 676-22 {
		t.Errorf("collapsed strip wrong: %+v", p[1])
	}
	// focused window content inset by bar
	if p[3].Rect != (Rect{0, 44 + 22, 1280, 676 - 22}) {
		t.Errorf("focused window wrong: %+v", p[3])
	}
	if !p[3].Bar {
		t.Errorf("focused window should have a titlebar")
	}
}

func TestTitlebarInsetsContent(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.TitlebarHeight = 22
	p := placements(s)
	if p[1].Rect != (Rect{0, 22, 1280, 360 - 22}) {
		t.Errorf("window 1: %v", p[1].Rect)
	}
	if p[2].Rect != (Rect{0, 360 + 22, 1280, 360 - 22}) {
		t.Errorf("window 2: %v", p[2].Rect)
	}
	if !p[1].Bar || !p[2].Bar {
		t.Errorf("both windows should have titlebars")
	}
}

func TestMaxMode(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.SetMode(ModeMax)
	p := placements(s)
	if p[2].Hidden || p[2].Rect != (Rect{0, 0, 1280, 720}) {
		t.Errorf("focused window wrong in max mode: %+v", p[2])
	}
	if !p[1].Hidden || p[1].Bar {
		t.Errorf("unfocused window should be hidden without a bar in max mode: %+v", p[1])
	}
}

func TestMoveCreatesAndCollectsColumns(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	// focused = 2; move right -> new column
	s.MoveDir(DirRight)
	v := s.activeView()
	if len(v.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(v.Columns))
	}
	// move back left -> appended to column 1, column 2 collected
	s.MoveDir(DirLeft)
	if len(v.Columns) != 1 || len(v.Columns[0].Windows) != 2 {
		t.Fatalf("expected 1 column with 2 windows, got %+v", v.Columns)
	}
	if s.Focused != 2 {
		t.Fatalf("focus should follow moved window, got %d", s.Focused)
	}
}

func TestMoveWithinColumn(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	// column: [1, 2], focused 2 at row 1
	s.MoveDir(DirUp)
	v := s.activeView()
	if !reflect.DeepEqual(v.Columns[0].Windows, []WindowID{2, 1}) {
		t.Fatalf("swap failed: %v", v.Columns[0].Windows)
	}
}

func TestToggleFloat(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.ToggleFloat()
	v := s.activeView()
	if len(v.Float) != 1 || v.Float[0] != 2 {
		t.Fatalf("window 2 should float: %v", v.Float)
	}
	p := placements(s)
	r := p[2].Rect
	if r.W != 640 || r.H != 360 || r.X != 320 || r.Y != 180 {
		t.Errorf("float rect wrong: %v", r)
	}
	if p[1].Rect != (Rect{0, 0, 1280, 720}) {
		t.Errorf("tiled window should fill view: %v", p[1].Rect)
	}
	// toggle back
	s.ToggleFloat()
	if len(v.Float) != 0 || len(v.Columns[0].Windows) != 2 {
		t.Fatalf("window 2 should be tiled again")
	}
}

func TestFocusToggleLayer(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.FocusWindow(2)
	s.ToggleFloat()
	s.FocusWindow(1)
	s.FocusToggleLayer()
	if s.Focused != 2 {
		t.Fatalf("expected float window focused, got %d", s.Focused)
	}
	s.FocusToggleLayer()
	if s.Focused != 1 {
		t.Fatalf("expected tiled window focused, got %d", s.Focused)
	}
}

func TestViewSwitchRestoresFocus(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.FocusWindow(1)
	s.SelectView("web")
	s.AddWindow(3, false) // on view web
	s.SelectView("1")
	if s.Focused != 1 {
		t.Fatalf("view 1 should restore focus to window 1, got %d", s.Focused)
	}
	s.SelectView("web")
	if s.Focused != 3 {
		t.Fatalf("view web should restore focus to window 3, got %d", s.Focused)
	}
}

func TestMoveToView(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.MoveToView("web")
	if !s.Windows[2].HasTag("web") || len(s.Windows[2].Tags) != 1 {
		t.Fatalf("window 2 tags wrong: %v", s.Windows[2].TagList())
	}
	v1 := s.View("1")
	if v1.contains(2) {
		t.Fatalf("window 2 should be gone from view 1")
	}
	if !s.View("web").contains(2) {
		t.Fatalf("window 2 should be on view web")
	}
	if s.Focused != 1 {
		t.Fatalf("focus should fall back to window 1, got %d", s.Focused)
	}
	// view web is not selected anywhere and has a window -> kept
	if s.View("web") == nil {
		t.Fatalf("view web should exist")
	}
}

func TestEmptyViewGC(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.MoveToView("web") // window 2 -> web; focus falls back to window 1
	s.FocusWindow(2)    // follow window 2 to view web
	s.MoveToView("1")   // window 2 -> back to 1; web is empty but selected
	s.SelectView("1")   // leaving the empty view destroys it
	if s.View("web") != nil {
		t.Fatalf("empty unselected view should be destroyed")
	}
}

func TestSelectedEmptyViewSurvives(t *testing.T) {
	s := newTestState(t)
	s.SelectView("empty")
	if s.View("empty") == nil {
		t.Fatalf("selected empty view must survive GC")
	}
}

func TestTagSpec(t *testing.T) {
	s := newTestState(t)
	s.TagSpec("+web", "+mail")
	if got := s.Windows[1].TagList(); !reflect.DeepEqual(got, []string{"1", "mail", "web"}) {
		t.Fatalf("tags wrong: %v", got)
	}
	// window 1 visible on view web (not selected) -> its layout hidden;
	// select web: window appears
	s.SelectView("web")
	p := placements(s)
	if p[1].Hidden {
		t.Fatalf("window 1 should be visible on view web")
	}
	s.SelectView("1")
	s.TagSpec("-web")
	if s.Windows[1].HasTag("web") {
		t.Fatalf("web tag should be removed")
	}
	// bare spec resets
	s.TagSpec("solo")
	if got := s.Windows[1].TagList(); !reflect.DeepEqual(got, []string{"solo"}) {
		t.Fatalf("bare spec should reset tags: %v", got)
	}
}

func TestTagSpecNeverEmpty(t *testing.T) {
	s := newTestState(t)
	s.TagSpec("-1")
	if len(s.Windows[1].Tags) == 0 {
		t.Fatalf("tag set must never be empty")
	}
}

func TestMultiOutputSwap(t *testing.T) {
	s := newTestState(t)
	o2 := s.AddOutput("DP-1")
	o2.Rect = Rect{X: 1280, Y: 0, W: 1920, H: 1080}
	// second output gets a fresh view "2"
	if o2.View == "1" || o2.View == "" {
		t.Fatalf("second output should get its own view, got %q", o2.View)
	}
	v2 := o2.View
	// active output is still the first one; select the view shown on
	// output 2 -> the outputs swap views
	s.SelectView(v2)
	if s.Outputs[0].View != v2 || s.Outputs[1].View != "1" {
		t.Fatalf("swap failed: %q %q", s.Outputs[0].View, s.Outputs[1].View)
	}
	if s.FocusOutput != 0 {
		t.Fatalf("focus should stay on the user's output, got %d", s.FocusOutput)
	}
}

func TestCycleViewSkipsShownElsewhere(t *testing.T) {
	s := newTestState(t)
	o2 := s.AddOutput("DP-1")
	o2.Rect = Rect{X: 1280, Y: 0, W: 1920, H: 1080}
	s.AddWindow(2, false, "extra")
	s.FocusWindow(1) // focus output 0, view 1
	// views: "1" (out 0), o2.View (out 1), "extra"
	if s.activeOutput().Name != "HDMI-A-1" {
		t.Fatalf("expected output 0 active")
	}
	s.CycleView(1)
	// must skip the view shown on output 1 and land on "extra"
	if s.Outputs[0].View != "extra" {
		t.Fatalf("cycle should land on extra, got %q", s.Outputs[0].View)
	}
	s.CycleView(1) // wraps to "1"
	if s.Outputs[0].View != "1" {
		t.Fatalf("cycle should wrap to 1, got %q", s.Outputs[0].View)
	}
}

func TestViewNumbering(t *testing.T) {
	s := newTestState(t)
	s.SelectViewN(3) // creates view "3" (only 1 exists)
	if s.activeView().Name != "3" {
		t.Fatalf("expected view 3, got %q", s.activeView().Name)
	}
	s.SelectViewN(1) // resolves by name to existing view "1"
	if s.activeView().Name != "1" {
		t.Fatalf("expected view 1, got %q", s.activeView().Name)
	}
	s.MoveToViewN(3) // resolves by name to view "3"
	if !s.Windows[1].HasTag("3") {
		t.Fatalf("window should have moved to view 3: %v", s.Windows[1].TagList())
	}
}

func TestSelectViewNEmptyViews(t *testing.T) {
	// Regression: gcViews compacts the Views slice; view-n must still
	// resolve by name, not by position. New session, no windows:
	// 1 -> 2 -> back to 1.
	s := NewState()
	o := s.AddOutput("HDMI-A-1")
	o.Rect = Rect{X: 0, Y: 0, W: 1280, H: 720}
	s.SelectViewN(2)
	if s.activeView().Name != "2" {
		t.Fatalf("expected view 2, got %q", s.activeView().Name)
	}
	s.SelectViewN(1)
	if s.activeView().Name != "1" {
		t.Fatalf("expected view 1, got %q", s.activeView().Name)
	}
}

func TestSelectViewNAfterMiddleViewGC(t *testing.T) {
	// Regression: with a window pinning view "1", going 1 -> 2 -> 3
	// destroys empty view "2"; view-n 2 must recreate it, not alias
	// the current view.
	s := newTestState(t)
	s.SelectViewN(2)
	s.SelectViewN(3)
	if s.activeView().Name != "3" {
		t.Fatalf("expected view 3, got %q", s.activeView().Name)
	}
	s.SelectViewN(2)
	if s.activeView().Name != "2" {
		t.Fatalf("expected view 2, got %q", s.activeView().Name)
	}
}

func TestGrow(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.MoveDir(DirRight) // window 2 to column 2
	s.FocusWindow(1)    // focus column 1
	s.Grow(DirRight, 10)
	v := s.activeView()
	if v.Columns[0].Factor != 1.1 || v.Columns[1].Factor != 0.9 {
		t.Fatalf("factors wrong: %v %v", v.Columns[0].Factor, v.Columns[1].Factor)
	}
	p := placements(s)
	w0 := int32(float64(1280) * 1.1 / 2.0)
	if p[1].Rect.W != w0 {
		t.Fatalf("column width wrong: got %d want %d", p[1].Rect.W, w0)
	}
}

func TestRemoveWindowRefocuses(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.AddWindow(3, false)
	s.RemoveWindow(3) // focused window removed
	if s.Focused != 2 {
		t.Fatalf("focus should fall to window 2, got %d", s.Focused)
	}
	s.RemoveWindow(2)
	s.RemoveWindow(1)
	if s.Focused != 0 {
		t.Fatalf("no windows left, focus should be 0, got %d", s.Focused)
	}
}

func TestWindowRendersOnFirstOutputOnly(t *testing.T) {
	s := newTestState(t)
	o2 := s.AddOutput("DP-1")
	o2.Rect = Rect{X: 1280, Y: 0, W: 1920, H: 1080}
	// tag window 1 onto the view shown on output 2 as well
	v2 := o2.View
	s.FocusWindow(1)
	s.TagSpec("+doesnotexist") // ensure TagSpec works on focused
	s.TagSpec("-" + "doesnotexist")
	s.Windows[1].Tags[v2] = true
	s.syncWindowViews(1, false)
	count := 0
	var p Placement
	for _, pl := range s.Layout() {
		if pl.ID == 1 {
			count++
			p = pl
		}
	}
	if count != 1 {
		t.Fatalf("window must be placed exactly once, got %d", count)
	}
	// first output in order wins
	if p.Rect.X != 0 {
		t.Fatalf("window should render on first output, got x=%d", p.Rect.X)
	}
}

func TestColumnBoundaries(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.MoveDir(DirRight) // window 2 to column 2
	v := s.activeView()
	b := s.ColumnBoundaries(v, Rect{X: 0, Y: 0, W: 1280, H: 720})
	if len(b) != 3 || b[0] != 0 || b[1] != 640 || b[2] != 1280 {
		t.Fatalf("boundaries: %v", b)
	}
	if NearestColumnBoundary(b, 700) != 1 {
		t.Fatalf("nearest to 700 should be 1")
	}
	if NearestColumnBoundary(b, 1279) != 2 {
		t.Fatalf("nearest to 1279 should be 2")
	}
}

func TestResizeColumnBoundary(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.MoveDir(DirRight)
	v := s.activeView()
	// drag boundary 1 (middle) 128px right on a 1280 area
	s.ResizeColumnBoundary(v, 1, 128, 1280)
	f1, f2 := v.Columns[0].Factor, v.Columns[1].Factor
	if f1 <= 1 || f2 >= 1 {
		t.Fatalf("factors should shift: %v %v", f1, f2)
	}
	if f1+f2 < 1.99 || f1+f2 > 2.01 {
		t.Fatalf("total factor must be conserved: %v", f1+f2)
	}
	// outer edges are ignored
	s.ResizeColumnBoundary(v, 0, 500, 1280)
	if v.Columns[0].Factor != f1 {
		t.Fatalf("outer edge must be ignored")
	}
}

func TestFloatRectOps(t *testing.T) {
	s := newTestState(t)
	s.AddWindow(2, false)
	s.ToggleFloat()
	r := s.FloatRectOf(2)
	if r.W != 640 || r.H != 360 {
		t.Fatalf("float rect: %v", r)
	}
	s.SetFloatRect(2, Rect{X: 10, Y: 20, W: 400, H: 300})
	if got := s.FloatRectOf(2); got != (Rect{10, 20, 400, 300}) {
		t.Fatalf("set float rect: %v", got)
	}
	s.SetFloatRect(2, Rect{X: 0, Y: 0, W: 5, H: 5})
	if got := s.FloatRectOf(2); got.W != 50 || got.H != 50 {
		t.Fatalf("min clamp: %v", got)
	}
}

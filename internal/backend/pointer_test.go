package backend

import (
	"testing"

	"wimy/internal/wm"
)

// newPointerState returns a state with one 1280x720 output.
func newPointerState() *wm.State {
	s := wm.NewState()
	o := s.AddOutput("out")
	o.Rect = wm.Rect{X: 0, Y: 0, W: 1280, H: 720}
	return s
}

func TestResizeEdgesGrid(t *testing.T) {
	r := wm.Rect{X: 100, Y: 100, W: 300, H: 300}
	cases := []struct {
		x, y int32
		want Edges
	}{
		{110, 110, EdgeLeft | EdgeTop},
		{390, 250, EdgeRight},
		{250, 390, EdgeBottom},
		{250, 250, EdgeRight | EdgeBottom}, // centre: default corner
	}
	for _, c := range cases {
		if got := ResizeEdges(r, c.x, c.y); got != c.want {
			t.Errorf("ResizeEdges(%d,%d) = %b, want %b", c.x, c.y, got, c.want)
		}
	}
}

func TestMoveOpIsCumulative(t *testing.T) {
	s := newPointerState()
	s.AddWindow(1, true)
	start := wm.Rect{X: 100, Y: 100, W: 400, H: 300}
	s.SetFloatRect(1, start)

	op := StartPointerOp(s, 1, ButtonMove, 150, 150)
	if op == nil {
		t.Fatal("no op for Mod+left on a floating window")
	}
	op.Apply(10, 20)
	op.Apply(30, 5) // deltas are from the op start, not incremental

	want := wm.Rect{X: 130, Y: 105, W: 400, H: 300}
	if got := s.FloatRectOf(1); got != want {
		t.Errorf("float rect = %+v, want %+v", got, want)
	}
}

func TestResizeOpLeftEdgeClamps(t *testing.T) {
	s := newPointerState()
	s.AddWindow(1, true)
	s.SetFloatRect(1, wm.Rect{X: 100, Y: 100, W: 200, H: 200})

	op := ClientResizeOp(s, 1, EdgeLeft, 0)
	op.Apply(190, 0) // would leave 10px wide

	// minimum width 50, right edge (x=300) stays put
	want := wm.Rect{X: 250, Y: 100, W: 50, H: 200}
	if got := s.FloatRectOf(1); got != want {
		t.Errorf("float rect = %+v, want %+v", got, want)
	}
}

func TestResizeOpTopEdgeClamps(t *testing.T) {
	s := newPointerState()
	s.AddWindow(1, true)
	s.SetFloatRect(1, wm.Rect{X: 100, Y: 100, W: 200, H: 200})

	op := ClientResizeOp(s, 1, EdgeTop, 0)
	op.Apply(0, 190)

	want := wm.Rect{X: 100, Y: 250, W: 200, H: 50}
	if got := s.FloatRectOf(1); got != want {
		t.Errorf("float rect = %+v, want %+v", got, want)
	}
}

func TestStartPointerOpTiled(t *testing.T) {
	s := newPointerState()
	s.AddWindow(1, false)
	s.AddWindow(2, false)
	s.MoveDir(wm.DirRight) // window 2 into its own column: boundary at x=640
	v := s.ActiveViewOf(2)
	if len(v.Columns) != 2 {
		t.Fatalf("setup: %d columns, want 2", len(v.Columns))
	}

	if op := StartPointerOp(s, 2, ButtonMove, 700, 300); op != nil {
		t.Errorf("Mod+left on a tiled window started %T, want nothing", op)
	}

	op := StartPointerOp(s, 2, ButtonResize, 650, 300)
	if _, ok := op.(*ColumnResizeOp); !ok {
		t.Fatalf("Mod+right on a tiled window started %T, want *ColumnResizeOp", op)
	}
	op.Apply(128, 0)
	if !(v.Columns[0].Factor > v.Columns[1].Factor) {
		t.Errorf("dragging the boundary right should widen column 0: %v %v",
			v.Columns[0].Factor, v.Columns[1].Factor)
	}
	op.Apply(0, 0) // back to the start position
	if v.Columns[0].Factor != 1 || v.Columns[1].Factor != 1 {
		t.Errorf("factors after returning = %v %v, want 1 1",
			v.Columns[0].Factor, v.Columns[1].Factor)
	}
}

func TestClientMoveOpOnlyFloats(t *testing.T) {
	s := newPointerState()
	s.AddWindow(1, false)
	if op := ClientMoveOp(s, 1); op != nil {
		t.Errorf("client move of a tiled window started %T, want nothing", op)
	}
	s.AddWindow(2, true)
	s.FocusWindow(1)
	if _, ok := ClientMoveOp(s, 2).(*MoveOp); !ok {
		t.Fatal("client move of a floating window started no MoveOp")
	}
	if s.Focused != 2 {
		t.Errorf("focused = %d, want 2 (client move focuses the window)", s.Focused)
	}
}

func TestPointerOpUnknownWindow(t *testing.T) {
	s := newPointerState()
	if op := StartPointerOp(s, 99, ButtonResize, 0, 0); op != nil {
		t.Errorf("got %T for an unknown window", op)
	}
	if op := ClientResizeOp(s, 99, EdgeLeft, 0); op != nil {
		t.Errorf("got %T for an unknown window", op)
	}
}

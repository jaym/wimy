package titlebar

import (
	"image/color"
	"testing"
)

func testRenderer() *Renderer {
	return New(22, Colors{
		FocusedBg:     color.RGBA{0x8a, 0xad, 0xf4, 0xff},
		FocusedFg:     color.RGBA{0x1e, 0x20, 0x30, 0xff},
		NormalBg:      color.RGBA{0x24, 0x27, 0x3a, 0xff},
		NormalFg:      color.RGBA{0xa5, 0xad, 0xcb, 0xff},
		BorderFocused: color.RGBA{0x8a, 0xad, 0xf4, 0xff},
		BorderNormal:  color.RGBA{0x36, 0x3a, 0x4f, 0xff},
	}, 2)
}

func TestRenderSize(t *testing.T) {
	r := testRenderer()
	px := r.Render(640, 1, "hello", true)
	if len(px) != 640*22*4 {
		t.Fatalf("len: %d, want %d", len(px), 640*22*4)
	}
	px = r.Render(640, 2, "hello", true)
	if len(px) != 1280*44*4 {
		t.Fatalf("scaled len: %d, want %d", len(px), 1280*44*4)
	}
}

func TestRenderColors(t *testing.T) {
	r := testRenderer()
	focused := r.Render(640, 1, "title", true)
	normal := r.Render(640, 1, "title", false)

	// top-left pixel is the border frame (BGRA byte order)
	b, g, rd := focused[0], focused[1], focused[2]
	if rd != 0x8a || g != 0xad || b != 0xf4 {
		t.Errorf("focused frame pixel: %02x %02x %02x", rd, g, b)
	}
	// interior pixel (center-left, inside frame) differs by focus state
	mid := (11*640 + 100) * 4 // y=11, x=100
	fb, fg_, fr := focused[mid], focused[mid+1], focused[mid+2]
	nb, ng, nr := normal[mid], normal[mid+1], normal[mid+2]
	if fr == nr && fg_ == ng && fb == nb {
		t.Errorf("focused and normal interiors should differ")
	}
	// focused interior is the accent color
	if fr != 0x8a || fg_ != 0xad || fb != 0xf4 {
		t.Errorf("focused interior: %02x %02x %02x", fr, fg_, fb)
	}
}

func TestRenderLongTitle(t *testing.T) {
	r := testRenderer()
	long := "this is a very long window title that cannot possibly fit into a narrow titlebar"
	px := r.Render(100, 1, long, false)
	if len(px) != 100*22*4 {
		t.Fatalf("len: %d", len(px))
	}
}

func TestRenderZeroWidth(t *testing.T) {
	r := testRenderer()
	px := r.Render(0, 1, "x", true)
	if len(px) != 22*4 {
		t.Fatalf("len: %d", len(px))
	}
}

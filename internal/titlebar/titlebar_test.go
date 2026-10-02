package titlebar

import (
	"image/color"
	"runtime"
	"testing"

	"golang.org/x/image/math/fixed"
)

// testColors are the test titlebar colors (catppuccin-macchiato-ish).
var testColors = Colors{
	FocusedBg:     color.RGBA{0x8a, 0xad, 0xf4, 0xff},
	FocusedFg:     color.RGBA{0x1e, 0x20, 0x30, 0xff},
	NormalBg:      color.RGBA{0x24, 0x27, 0x3a, 0xff},
	NormalFg:      color.RGBA{0xa5, 0xad, 0xcb, 0xff},
	BorderFocused: color.RGBA{0x8a, 0xad, 0xf4, 0xff},
	BorderNormal:  color.RGBA{0x36, 0x3a, 0x4f, 0xff},
}

// testRenderer returns a Renderer with only the ASCII test font
// registered (hermetic: no system font scan).
func testRenderer(t *testing.T) *Renderer {
	t.Helper()
	r := newRenderer(22, testColors, 2, []string{"Noto Sans"})
	if err := r.addFontFile("testdata/NotoSans-ascii.ttf", ""); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRenderSize(t *testing.T) {
	r := testRenderer(t)
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
	r := testRenderer(t)
	focused := r.Render(640, 1, "title", true)
	normal := r.Render(640, 1, "title", false)

	// top-left pixel is the border frame (BGRA byte order)
	b, g, rd := focused[0], focused[1], focused[2]
	if rd != 0x8a || g != 0xad || b != 0xf4 {
		t.Errorf("focused frame pixel: %02x %02x %02x", rd, g, b)
	}
	// interior pixel (center-left, inside frame, past the text)
	// differs by focus state
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

// TestRenderDrawsText verifies the title leaves ink: with text, some
// interior pixel must carry the fg color; without, none does.
func TestRenderDrawsText(t *testing.T) {
	r := testRenderer(t)
	countFg := func(px []byte, w int) int {
		n := 0
		for y := 4; y < 20; y++ { // interior band (frame is 2px)
			for x := 4; x < w-4; x++ {
				i := (y*w + x) * 4
				// BGRA, fg = 0x1e2030 (focused)
				if px[i+2] == 0x1e && px[i+1] == 0x20 && px[i] == 0x30 {
					n++
				}
			}
		}
		return n
	}
	withText := r.Render(640, 1, "hello", true)
	withoutText := r.Render(640, 1, "", true)
	if countFg(withText, 640) == 0 {
		t.Errorf("title drew no fg-colored pixels")
	}
	if countFg(withoutText, 640) != 0 {
		t.Errorf("empty title drew fg-colored pixels")
	}
}

// TestFontFallback checks that runes covered by different fonts are
// split into runs shaped with those fonts (the fallback machinery
// that replaces tofu boxes).
func TestFontFallback(t *testing.T) {
	r := newRenderer(22, testColors, 2, []string{"Test A", "Test B"})
	if err := r.addFontFile("testdata/NotoSans-a.ttf", "Test A"); err != nil {
		t.Fatal(err)
	}
	if err := r.addFontFile("testdata/NotoSans-b.ttf", "Test B"); err != nil {
		t.Fatal(err)
	}
	line, truncated := r.shapeTitle("ab", fixed.I(16), 10000)
	if truncated != 0 {
		t.Fatalf("truncated %d runes in a wide bar", truncated)
	}
	if len(line) != 2 {
		t.Fatalf("got %d runs, want 2 (one per font)", len(line))
	}
	if line[0].Face == line[1].Face {
		t.Errorf("both runs use the same face; fallback did not happen")
	}
	// each run must shape its rune to a real glyph, not .notdef
	for i, run := range line {
		if len(run.Glyphs) != 1 || run.Glyphs[0].GlyphID == 0 {
			t.Errorf("run %d: shaped to .notdef/missing glyphs", i)
		}
	}
}

// TestEllipsis checks long titles are truncated with an ellipsis and
// fit the given width.
func TestEllipsis(t *testing.T) {
	r := testRenderer(t)
	line, truncated := r.shapeTitle("this is a very long window title that cannot possibly fit", fixed.I(16), 100)
	if truncated == 0 {
		t.Fatalf("no truncation at maxW=100")
	}
	total := fixed.Int26_6(0)
	for _, run := range line {
		total += run.Advance
	}
	if total.Ceil() > 100 {
		t.Errorf("truncated line is %dpx wide, max is 100", total.Ceil())
	}
	// the ellipsis run is appended last (visual order)
	last := line[len(line)-1]
	if len(last.Glyphs) == 0 {
		t.Fatalf("ellipsis run has no glyphs")
	}
}

func TestRenderLongTitle(t *testing.T) {
	r := testRenderer(t)
	long := "this is a very long window title that cannot possibly fit into a narrow titlebar"
	px := r.Render(100, 1, long, false)
	if len(px) != 100*22*4 {
		t.Fatalf("len: %d", len(px))
	}
}

// TestRenderNoFonts renders without any font at all: the bar must
// still be drawn, just without text (no panic, no nil dereference).
func TestRenderNoFonts(t *testing.T) {
	r := newRenderer(22, testColors, 2, []string{"sans-serif"})
	px := r.Render(640, 1, "hello 日本語 🎉", true)
	if len(px) != 640*22*4 {
		t.Fatalf("len: %d", len(px))
	}
	// interior is the plain focused background everywhere
	mid := (11*640 + 100) * 4
	if px[mid+2] != 0x8a || px[mid+1] != 0xad || px[mid] != 0xf4 {
		t.Errorf("interior: %02x %02x %02x", px[mid+2], px[mid+1], px[mid])
	}
}

// TestRenderMissingGlyphs renders text no registered font covers:
// glyphs degrade to .notdef in the resolved face, but nothing breaks.
func TestRenderMissingGlyphs(t *testing.T) {
	r := testRenderer(t)
	px := r.Render(640, 1, "日本語", true)
	if len(px) != 640*22*4 {
		t.Fatalf("len: %d", len(px))
	}
}

func TestRenderZeroWidth(t *testing.T) {
	r := testRenderer(t)
	px := r.Render(0, 1, "x", true)
	if len(px) != 22*4 {
		t.Fatalf("len: %d", len(px))
	}
}

func TestSystemFontOnMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS system fonts")
	}
	if r := New(22, testColors, 2, nil); r.fontMap.ResolveFace('A') == nil {
		t.Errorf("no system font found: titlebars would be drawn without text")
	}
}

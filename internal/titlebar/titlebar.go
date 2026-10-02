// Package titlebar renders wmii-style window titlebars in pure Go:
// a slim bar with the window title, focused/normal colors and a border
// frame matching the compositor-drawn window borders.
//
// Text is shaped with go-text/typesetting (a HarfBuzz port) over a
// fontconfig-style font map with per-rune fallback, so titles in any
// script — CJK, Arabic, Indic, emoji — render with real glyphs instead
// of tofu boxes.
package titlebar

import (
	"image"
	"image/color"
	"log"
	"os"
	"slices"
	"sort"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// Colors used to render a titlebar, in 8-bit-per-channel form.
type Colors struct {
	FocusedBg, FocusedFg color.RGBA
	NormalBg, NormalFg   color.RGBA
	BorderFocused        color.RGBA
	BorderNormal         color.RGBA
}

// Renderer renders titlebar images. It is not safe for concurrent
// use; titlebar rendering happens on the Wayland dispatch goroutine.
type Renderer struct {
	Height int32 // logical pixels
	Colors Colors
	Border int32 // border width in logical pixels

	fontMap   *fontscan.FontMap
	segmenter shaping.Segmenter
	shaper    shaping.HarfbuzzShaper
	wrapper   shaping.LineWrapper
	drawer    glyphDrawer
}

// New returns a Renderer using the system fonts. families is the
// fontconfig-style family list (first match wins, e.g. "Iosevka",
// "sans-serif"); fallback to any installed font covering a rune is
// automatic. If no fonts are found, bars are drawn without text (a
// warning is logged) instead of failing.
func New(height int32, colors Colors, borderWidth int32, families []string) *Renderer {
	r := newRenderer(height, colors, borderWidth, families)
	if err := r.fontMap.UseSystemFonts(""); err != nil {
		log.Printf("titlebar: system font scan failed, window titles will not be drawn: %v", err)
	} else if r.fontMap.ResolveFace('A') == nil {
		log.Printf("titlebar: no usable fonts found, window titles will not be drawn")
	}
	return r
}

// quietLogger silences fontscan's per-rune fallback chatter; the
// renderer degrades gracefully (fallback face, then no text) and New
// reports the no-fonts case itself.
type quietLogger struct{}

func (quietLogger) Printf(string, ...interface{}) {}

// newRenderer returns a Renderer with an empty font map. Tests add
// fonts explicitly with addFontFile, keeping them hermetic.
func newRenderer(height int32, colors Colors, borderWidth int32, families []string) *Renderer {
	if len(families) == 0 {
		families = []string{"sans-serif"}
	}
	// color emoji fonts are only reachable through the generic
	// "emoji" family (fontconfig 45-generic.conf substitutions),
	// so make sure every query ends with it
	if !slices.Contains(families, fontscan.Emoji) {
		families = append(slices.Clone(families), fontscan.Emoji)
	}
	fm := fontscan.NewFontMap(quietLogger{})
	fm.SetQuery(fontscan.Query{Families: families})
	return &Renderer{
		Height:  height,
		Colors:  colors,
		Border:  borderWidth,
		fontMap: fm,
	}
}

// addFontFile registers the font file at path, under the given
// family name (the font's own name when empty). The font map keeps
// the file open and parses it on demand.
func (r *Renderer) addFontFile(path, family string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return r.fontMap.AddFont(f, path, family)
}

// Render renders a titlebar of the given logical width for the given
// output scale. The returned pixels are premultiplied BGRA
// (wl_shm ARGB8888 on little-endian), width*scale by Height*scale.
func (r *Renderer) Render(width, scale int32, title string, focused bool) []byte {
	if scale < 1 {
		scale = 1
	}
	w, h := width*scale, r.Height*scale
	if w < 1 {
		w = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))

	bg, fg, frame := r.Colors.NormalBg, r.Colors.NormalFg, r.Colors.BorderNormal
	if focused {
		bg, fg, frame = r.Colors.FocusedBg, r.Colors.FocusedFg, r.Colors.BorderFocused
	}

	// border frame (top/left/right), filled interior
	fill(img, img.Bounds(), frame)
	bw := int(r.Border * scale)
	if 2*bw < int(w) && bw < int(h) {
		fill(img, image.Rectangle{Min: image.Point{X: bw, Y: bw}, Max: image.Point{X: int(w) - bw, Y: int(h)}}, bg)
	}

	// title text, vertically centered, ellipsized to fit
	pad := 6 * int(scale)
	maxW := int(w) - 2*pad
	if maxW > 0 && title != "" && r.fontMap.ResolveFace('A') != nil {
		size := fixed.I(int(max(r.Height-6, 1) * scale)) // logical height minus padding
		r.drawTitle(img, title, fg, pad, int(h), maxW, size)
	}

	return rgbaToBGRA(img)
}

// drawTitle shapes title, truncates it with an ellipsis to fit maxW
// device pixels and draws it into img, vertically centered in a bar
// of h device pixels, starting at x = pad.
func (r *Renderer) drawTitle(img *image.RGBA, title string, fg color.RGBA, pad, h, maxW int, size fixed.Int26_6) {
	final, _ := r.shapeTitle(title, size, maxW)
	if len(final) == 0 {
		return
	}

	// vertical centering on the tallest run metrics
	ascent, descent := 0, 0
	for _, run := range final {
		ascent = max(ascent, run.LineBounds.Ascent.Ceil())
		descent = max(descent, -run.LineBounds.Descent.Ceil())
	}
	baseline := (h-ascent-descent)/2 + ascent

	x := pad
	for _, run := range final {
		x = r.drawer.drawRun(img, run, fg, x, baseline)
	}
}

// shapeTitle shapes title into a single line of runs in visual
// order, truncating with "…" when wider than maxW device pixels. It
// returns the runs to draw and the number of truncated runes.
func (r *Renderer) shapeTitle(title string, size fixed.Int26_6, maxW int) (shaping.Line, int) {
	text := []rune(title)
	in := shaping.Input{
		Text:      text,
		RunStart:  0,
		RunEnd:    len(text),
		Direction: di.DirectionLTR,
		Size:      size,
	}

	// split by bidi direction, script and font coverage, then shape
	// each run with its resolved face
	runs := r.segmenter.Split(in, r.fontMap)
	line := make(shaping.Line, 0, len(runs))
	for _, run := range runs {
		if run.Face == nil {
			continue
		}
		line = append(line, r.shaper.Shape(run))
	}
	if len(line) == 0 {
		return nil, 0
	}

	// single line, truncated with "…" when the title is too wide
	cfg := shaping.WrapConfig{
		Direction:          line[0].Direction,
		TruncateAfterLines: 1,
		BreakPolicy:        shaping.WhenNecessary,
	}
	if face := r.fontMap.ResolveFace('…'); face != nil {
		truncIn := shaping.Input{
			Text:      []rune("…"),
			RunEnd:    1,
			Direction: line[0].Direction,
			Face:      face,
			Size:      size,
		}
		cfg = cfg.WithTruncator(&r.shaper, truncIn)
	}
	r.wrapper.Prepare(cfg, text, shaping.NewSliceIterator(line))
	wrapped, _ := r.wrapper.WrapNextLine(maxW)
	final := wrapped.Line

	// drawing order is visual (left to right), not logical
	sort.Slice(final, func(i, j int) bool { return final[i].VisualIndex < final[j].VisualIndex })
	return final, wrapped.Truncated
}

func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	if r.Empty() {
		return
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i := img.PixOffset(r.Min.X, y)
		for x := r.Min.X; x < r.Max.X; x++ {
			img.Pix[i+0] = c.R
			img.Pix[i+1] = c.G
			img.Pix[i+2] = c.B
			img.Pix[i+3] = c.A
			i += 4
		}
	}
}

// rgbaToBGRA converts a Go RGBA image (premultiplied, R,G,B,A byte
// order) to wl_shm ARGB8888 (premultiplied, B,G,R,A byte order on
// little-endian).
func rgbaToBGRA(img *image.RGBA) []byte {
	out := make([]byte, len(img.Pix))
	for i := 0; i < len(img.Pix); i += 4 {
		out[i+0] = img.Pix[i+2] // B
		out[i+1] = img.Pix[i+1] // G
		out[i+2] = img.Pix[i+0] // R
		out[i+3] = img.Pix[i+3] // A
	}
	return out
}

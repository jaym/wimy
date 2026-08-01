// Package titlebar renders wmii-style window titlebars in pure Go:
// a slim bar with the window title, focused/normal colors and a border
// frame matching the compositor-drawn window borders.
package titlebar

import (
	"image"
	"image/color"
	"os"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Colors used to render a titlebar, in 8-bit-per-channel form.
type Colors struct {
	FocusedBg, FocusedFg color.RGBA
	NormalBg, NormalFg   color.RGBA
	BorderFocused        color.RGBA
	BorderNormal         color.RGBA
}

// Renderer renders titlebar images.
type Renderer struct {
	Height int32 // logical pixels
	Colors Colors
	Border int32 // border width in logical pixels

	fontData []byte // nil: use the built-in fallback font
	faces    map[int32]font.Face
}

// fontCandidates are tried in order; the first that parses wins.
var fontCandidates = []string{
	"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",     // Debian/Ubuntu
	"/usr/share/fonts/dejavu/DejaVuSans.ttf",              // Arch
	"/usr/share/fonts/TTF/DejaVuSans.ttf",                 // Fedora
	"/usr/share/fonts/dejavu-sans/DejaVuSans.ttf",         // openSUSE
	"/usr/share/fonts/noto/NotoSans-Regular.ttf",          // Arch noto
	"/usr/share/fonts/noto-sans/NotoSans-Regular.ttf",     // Fedora noto
	"/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf", // Debian noto
}

// New returns a Renderer. A sans-serif system font is used if found,
// otherwise a small built-in bitmap font.
func New(height int32, colors Colors, borderWidth int32) *Renderer {
	r := &Renderer{
		Height: height,
		Colors: colors,
		Border: borderWidth,
		faces:  make(map[int32]font.Face),
	}
	for _, p := range fontCandidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if _, err := opentype.Parse(data); err == nil {
			r.fontData = data
			break
		}
	}
	return r
}

// face returns the font face for the given output scale, creating and
// caching it on first use.
func (r *Renderer) face(scale int32) font.Face {
	if scale < 1 {
		scale = 1
	}
	if f, ok := r.faces[scale]; ok {
		return f
	}
	var f font.Face
	size := float64(r.Height-6) * float64(scale) // logical height minus padding
	if r.fontData != nil {
		if ttf, err := opentype.Parse(r.fontData); err == nil {
			f, _ = opentype.NewFace(ttf, &opentype.FaceOptions{
				Size: size, DPI: 72, Hinting: font.HintingFull,
			})
		}
	}
	if f == nil {
		f = basicfont.Face7x13
	}
	r.faces[scale] = f
	return f
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
	d := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(fg),
		Face: r.face(scale),
	}
	pad := 6 * int(scale)
	maxW := int(w) - 2*pad
	if maxW > 0 {
		title = ellipsize(&d, title, maxW)
		metrics := d.Face.Metrics()
		ascent := metrics.Ascent.Ceil()
		descent := metrics.Descent.Ceil()
		baseline := (int(h)-ascent-descent)/2 + ascent
		d.Dot = fixed.P(pad, baseline)
		d.DrawString(title)
	}

	return rgbaToBGRA(img)
}

// ellipsize truncates s with "…" until it fits maxW pixels.
func ellipsize(d *font.Drawer, s string, maxW int) string {
	if d.MeasureString(s).Ceil() <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && d.MeasureString(string(r)+"…").Ceil() > maxW {
		r = r[:len(r)-1]
	}
	if len(r) == 0 {
		return ""
	}
	return string(r) + "…"
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

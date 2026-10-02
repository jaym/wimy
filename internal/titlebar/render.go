// Glyph rasterization for shaped text runs, adapted from
// github.com/go-text/render (render.go and bitmap.go at commit
// 72238c6215e4), with SVG glyph support removed and a bitmap/outline
// fallback added for colored (COLR) glyphs, which are not drawn.
// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package titlebar

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg" // bitmap glyph formats
	_ "image/png"
	"math"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/shaping"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/tiff"
	"golang.org/x/image/vector"
)

// glyphDrawer rasterizes shaped runs into an image. It keeps a
// reusable rasterizer and is not safe for concurrent use (titlebar
// rendering happens on the Wayland dispatch goroutine).
type glyphDrawer struct {
	rasterizer  vector.Rasterizer
	fillerScale float32
}

// drawRun draws the shaped run into img, starting at pixel position
// (startX, startY), startY being the baseline. It returns the X pixel
// position of the end of the drawn run.
func (d *glyphDrawer) drawRun(img draw.Image, run shaping.Output, fg color.Color, startX, startY int) int {
	d.rasterizer.Reset(img.Bounds().Dx(), img.Bounds().Dy())
	scale := float32(run.Size) / 64 / float32(run.Face.Upem())
	d.fillerScale = scale

	x := float32(startX)
	y := float32(startY)
	for _, g := range run.Glyphs {
		xPos := x + fixed266ToFloat(g.XOffset)
		yPos := y - fixed266ToFloat(g.YOffset)
		switch data := run.Face.GlyphData(g.GlyphID).(type) {
		case font.GlyphOutline:
			d.drawOutline(data, scale, xPos, yPos)
		case font.GlyphBitmap:
			d.drawBitmap(g, data, img, xPos, yPos, fg)
		default:
			// nil, SVG or COLR glyph data: try the bitmap and
			// outline tables directly (GlyphData prefers COLR,
			// which we cannot paint).
			if b, ok := run.Face.GlyphDataBitmap(uint16(g.GlyphID)); ok {
				d.drawBitmap(g, b, img, xPos, yPos, fg)
			} else if o, ok := run.Face.GlyphDataOutline(uint16(g.GlyphID)); ok {
				d.drawOutline(o, scale, xPos, yPos)
			}
		}
		x += fixed266ToFloat(g.Advance)
	}
	d.rasterizer.Draw(img, img.Bounds(), image.NewUniform(fg), image.Point{})
	return int(math.Ceil(float64(x)))
}

// drawOutline appends the glyph outline (in font units) to the
// rasterizer, scaled to device pixels at origin (x, y).
func (d *glyphDrawer) drawOutline(outline font.GlyphOutline, scale, x, y float32) {
	raster := &d.rasterizer
	for _, s := range outline.Segments {
		switch s.Op {
		case opentype.SegmentOpMoveTo:
			raster.MoveTo(s.Args[0].X*scale+x, -s.Args[0].Y*scale+y)
		case opentype.SegmentOpLineTo:
			raster.LineTo(s.Args[0].X*scale+x, -s.Args[0].Y*scale+y)
		case opentype.SegmentOpQuadTo:
			raster.QuadTo(s.Args[0].X*scale+x, -s.Args[0].Y*scale+y, s.Args[1].X*scale+x, -s.Args[1].Y*scale+y)
		case opentype.SegmentOpCubeTo:
			raster.CubeTo(s.Args[0].X*scale+x, -s.Args[0].Y*scale+y,
				s.Args[1].X*scale+x, -s.Args[1].Y*scale+y,
				s.Args[2].X*scale+x, -s.Args[2].Y*scale+y)
		}
	}
	raster.ClosePath()
}

// drawBitmap draws a bitmap glyph (color emoji and embedded bitmap
// strikes) scaled into the glyph's device-pixel rect at (x, y).
func (d *glyphDrawer) drawBitmap(g shaping.Glyph, bitmap font.GlyphBitmap, img draw.Image, x, y float32, fg color.Color) {
	top := y - fixed266ToFloat(g.YBearing)
	bottom := top - fixed266ToFloat(g.Height)
	right := x + fixed266ToFloat(g.Width)
	switch bitmap.Format {
	case font.BlackAndWhite:
		rec := image.Rect(0, 0, bitmap.Width, bitmap.Height)
		sub := image.NewPaletted(rec, color.Palette{color.Transparent, fg})
		for i := range sub.Pix {
			sub.Pix[i] = bitAt(bitmap.Data, i)
		}
		rect := image.Rect(int(x), int(top), int(right), int(bottom))
		xdraw.NearestNeighbor.Scale(img, rect, sub, sub.Bounds(), draw.Over, nil)
	case font.JPG, font.PNG, font.TIFF:
		pix, _, err := image.Decode(bytes.NewReader(bitmap.Data))
		if err != nil {
			return
		}
		rect := image.Rect(int(x), int(top), int(right), int(bottom))
		xdraw.BiLinear.Scale(img, rect, pix, pix.Bounds(), draw.Over, nil)
	}
	if bitmap.Outline != nil {
		d.drawOutline(*bitmap.Outline, d.fillerScale, x, y)
	}
}

func fixed266ToFloat(i fixed.Int26_6) float32 { return float32(float64(i) / 64) }

// bitAt returns the bit at the given index in the byte slice.
func bitAt(b []byte, i int) byte { return (b[i/8] >> (7 - i%8)) & 1 }

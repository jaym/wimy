package river

import (
	"context"
	"log"

	"golang.org/x/sys/unix"

	"wimy/internal/proto"
	"wimy/internal/wm"
)

// outputScale returns the integer scale factor of the named output.
func (b *Backend) outputScale(name string) int32 {
	for _, o := range b.outputs {
		if o.NameInModel == name && o.Scale > 0 {
			return o.Scale
		}
	}
	return 1
}

// wlBuffer destroys its buffer when the compositor releases it.
type wlBuffer struct {
	proto.WlBufferStub
	obj proto.WlBuffer
}

func (wb *wlBuffer) HandleWlBufferRelease(ctx context.Context) { wb.obj.Destroy() }

// shmBuffer wraps pixels (premultiplied BGRA, w*h*4 bytes) in a
// wl_shm buffer.
func (b *Backend) shmBuffer(w, h int32, pixels []byte) (proto.WlBuffer, error) {
	fd, err := unix.MemfdCreate("wimy-titlebar", 0)
	if err != nil {
		return proto.WlBuffer{}, err
	}
	defer unix.Close(fd)
	if _, err := unix.Write(fd, pixels); err != nil {
		return proto.WlBuffer{}, err
	}
	pool := b.shm.CreatePool(fd, int32(len(pixels)))
	buf := pool.CreateBuffer(0, w, h, w*4, proto.WlShmFormatArgb8888)
	pool.Destroy()
	buf.SetUserData(&wlBuffer{obj: buf})
	return buf, nil
}

// renderTitlebar draws the window's titlebar if anything it shows has
// changed. Called from the render sequence; the decoration commit is
// synced to render_finish.
func (b *Backend) renderTitlebar(w *Window, p wm.Placement) {
	barH := b.cfg.Titlebar.Height
	if barH <= 0 || !p.Bar || w.CSDOnly || !b.comp.IsSet() {
		return
	}
	if !w.Deco.IsSet() {
		surf := b.comp.CreateSurface()
		region := b.comp.CreateRegion() // empty: no input on titlebars
		surf.SetInputRegion(region)
		region.Destroy()
		w.DecoSurface = surf
		w.Deco = w.Object.GetDecorationAbove(surf)
	}
	w.Deco.SetOffset(0, -barH)

	scale := b.outputScale(p.Output)
	width := max32(p.Rect.W, 1)
	if w.DecoWidth == width && w.DecoScale == scale &&
		w.DecoTitle == w.Title && w.DecoFocused == p.Focused {
		return
	}
	w.DecoWidth, w.DecoScale, w.DecoTitle, w.DecoFocused = width, scale, w.Title, p.Focused

	pixels := b.tbr.Render(width, scale, w.Title, p.Focused)
	buf, err := b.shmBuffer(width*scale, barH*scale, pixels)
	if err != nil {
		log.Printf("titlebar: %v", err)
		return
	}
	w.Deco.SyncNextCommit()
	w.DecoSurface.Attach(buf, 0, 0)
	w.DecoSurface.SetBufferScale(scale)
	w.DecoSurface.DamageBuffer(0, 0, width*scale, barH*scale)
	w.DecoSurface.Commit()
}

// destroyDeco releases titlebar protocol objects.
func (w *Window) destroyDeco() {
	if w.Deco.IsSet() {
		w.Deco.Destroy()
	}
	if w.DecoSurface.IsSet() {
		w.DecoSurface.Destroy()
	}
}

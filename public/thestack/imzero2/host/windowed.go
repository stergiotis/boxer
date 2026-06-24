package imzhost

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// AdaptToRenderer wraps an AppI as a func() error usable by the host's
// existing renderer slice. Mount runs lazily on the first call. Per ADR-0026
// Amendment 2026-05-12, for `SurfaceWindowed` apps the runtime — not the
// app — creates the window: `a.Frame` is invoked inside a `c.Window` scope
// built from `Manifest.WindowTitle()` and `SurfaceHints.PreferredWidth/Height`.
// `SurfaceHeadless` apps still call `Frame` raw. The M3 dock host will
// replace this adapter with a tile-bound child UI per app.
func AdaptToRenderer(a app.AppI) (r func() error) {
	m := a.Manifest()
	mountCtx := app.NewStaticMountContext(m.Id, app.AppLogger(log.Logger, m.Id), nil, nil, nil)
	frameCtx := app.NewStaticFrameContext(mountCtx, nil)
	hostIds := c.NewWidgetIdStack()
	var mounted bool
	r = func() (err error) {
		hostIds.Reset()
		if !mounted {
			err = a.Mount(mountCtx)
			if err != nil {
				err = eh.Errorf("app mount: %w", err)
				return
			}
			mounted = true
		}
		if m.Surface != app.SurfaceWindowed {
			err = a.Frame(frameCtx)
			return
		}
		w, h := WindowDefaultSize(m.SurfaceHints)
		windowId := "app:" + string(m.Id)
		for range c.Window(hostIds.PrepareStr(windowId),
			c.WidgetText().Text(m.WindowTitle()).Keep()).
			Resizable(true).
			TitleBar(true).
			DefaultOpen(true).
			DefaultSize(w, h).
			KeepIter() {
			err = a.Frame(frameCtx)
			if err != nil {
				return
			}
		}
		return
	}
	return
}

// WindowDefaultSize returns the initial Window size for a windowed app.
// Honours SurfaceHints when set, falls back to a generic large-enough
// pair that fits most laptop screens without taking the whole viewport.
func WindowDefaultSize(h app.SurfaceHints) (w, height float32) {
	w = float32(h.PreferredWidth)
	if w == 0 {
		w = 960
	}
	height = float32(h.PreferredHeight)
	if height == 0 {
		height = 720
	}
	return
}

package imzhost

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// AdaptBodyOnly wraps an app's body without Window chrome. Used in screenshot
// mode so the app renders directly into the host Frame (no title bar, no
// resize handles) for clean capture output.
func AdaptBodyOnly(a app.AppI) func(hids *c.WidgetIdStack) error {
	m := a.Manifest()
	mc := app.NewStaticMountContext(m.Id, app.AppLogger(log.Logger, m.Id), nil, nil, nil)
	fc := app.NewStaticFrameContext(mc, nil)
	var mounted bool
	return func(hids *c.WidgetIdStack) (err error) {
		mc.SetIds(hids)
		if !mounted {
			if e := a.Mount(mc); e != nil {
				return eb.Build().Str("app", string(m.Id)).Str("error", e.Error()).Errorf("app mount failed")
			}
			mounted = true
		}
		return a.Frame(fc)
	}
}

// DecorateScreenshotRenderer renders body renderers into AllocateUiAtRect so
// the content fills a known bounding rect.  RequestScreenshotRect captures
// only that rect — no window chrome, no whitespace.
func DecorateScreenshotRenderer(bodyRenderers []func(hids *c.WidgetIdStack) error, stageW, stageH float32) func() error {
	ids := c.NewWidgetIdStack()
	return func() error {
		for range c.IdScope(ids.PrepareSeq(0)) {
			for range c.AllocateUiAtRect(0, 0, stageW, stageH).KeepIter() {
				for _, body := range bodyRenderers {
					if e := body(ids); e != nil {
						return e
					}
				}
			}
		}
		return nil
	}
}

// ScreenshotStageSize returns the capture dimensions from the registered
// imzero2env.ScreenshotSize variable (IMZERO2_SCREENSHOT_SIZE, WxH format,
// same as boxer's hmi.sh). Falls back to 1600×900 when the env var is empty
// or malformed — large enough for most UIs.
func ScreenshotStageSize() (w, h float32) {
	w, h = 1600, 900 // fallback matching boxer's testStageDefault caps
	if wi, hi, ok := imzero2env.ScreenshotSizeWH(); ok {
		w, h = float32(wi), float32(hi)
	}
	return
}

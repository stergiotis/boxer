package imzrt

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// manifest is the static AppI descriptor every imzrt instance returns. imzrt is
// observe-only — it declares no capabilities (ADR-0061 SD6): it reads the Go
// runtime's own metrics and mutates nothing.
var manifest = app.Manifest{
	Id:      "github.com/stergiotis/boxer/apps/imzrt",
	Version: "0.1.0",
	Display: "imzrt",
	Title:   "imzrt",
	// Phosphor pulse — the runtime-heartbeat metaphor; distinct from imztop's
	// PhGauge (the system-monitor metaphor) so the two siblings read apart.
	Icon:     icons.PhPulse,
	Category: "Tools",
	Surface:  app.SurfaceWindowed,
}

func init() {
	// Interactive mode hands back a per-window *App. Under IMZERO2_SCREENSHOT_DIR
	// the screenshot tour takes over via a single-instance SeededFuncApp, matching
	// imztop's split (ADR-0061 SD15).
	var ctor app.AppCtor
	if imzero2env.ScreenshotDir.Get() != "" {
		ctor = func() (a app.AppI, err error) {
			a, err = app.NewSeededFuncApp(manifest, RenderLoopHandlerTour)
			return
		}
	} else {
		ctor = func() (a app.AppI, err error) {
			a = newApp()
			return
		}
	}
	if err := app.DefaultRegistry.RegisterFactory(manifest, ctor); err != nil {
		log.Warn().Err(err).Msg("imzrt: failed to register factory")
	}
}

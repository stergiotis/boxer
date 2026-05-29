//go:build llm_generated_opus47

package imztop

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// manifest is the static AppI descriptor every imztop instance
// returns. Kept package-level so the factory ctor can hand a fresh
// instance back without re-running Manifest validation.
var manifest = app.Manifest{
	Id:      "github.com/stergiotis/boxer/apps/imztop",
	Version: "0.1.0",
	Display: "imztop",
	Title:   "imztop",
	// Phosphor gauge — the system-monitor metaphor; rendered from the
	// Phosphor icon font registered at carousel startup (ADR-0044).
	Icon:     icons.PhGauge,
	Category: "Tools",
	Surface:  app.SurfaceWindowed,
}

func init() {
	// Interactive mode registers the per-instance *App directly so each
	// open window has its own UI state (currently the selected network
	// interface). Tour mode goes through SeededFuncApp instead — tours
	// are single-instance by design and don't need per-window state.
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
	err := app.DefaultRegistry.RegisterFactory(manifest, ctor)
	if err != nil {
		log.Warn().Err(err).Msg("imztop: failed to register factory")
	}
}

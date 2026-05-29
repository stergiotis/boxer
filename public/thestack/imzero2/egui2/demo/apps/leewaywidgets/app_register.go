//go:build llm_generated_opus47

package leewaywidgets_demo

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// manifest is the static AppI descriptor every instance returns. Kept
// package-level so the factory ctor can hand a fresh instance back
// without re-running Manifest validation.
var manifest = app.Manifest{
	Id:       "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/leewaywidgets",
	Version:  "0.1.0",
	Display:  "Leeway widgets tour",
	Title:    "leeway widgets — fixture showcase",
	Icon:     "🧪",
	Category: "Demos",
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  1100,
		PreferredHeight: 700,
	},
}

func init() {
	// Interactive mode registers the per-instance *App so each open
	// window has its own selectedView. Tour mode goes through
	// SeededFuncApp — tours are single-instance by design.
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
		log.Warn().Err(err).Msg("leewaywidgets_demo: failed to register factory")
	}
}

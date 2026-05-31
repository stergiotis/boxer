//go:build llm_generated_opus47

package leewaywidgets_demo

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
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
	// leewaywidgets registers an interactive per-window *App so each open
	// window has its own selectedView. Screenshot capture is handled
	// centrally by the widgets TestDriver via the Demos registered in
	// leewaywidgets_tour.go (ADR-0057).
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, err error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("leewaywidgets_demo: failed to register factory")
	}
}

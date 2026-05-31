//go:build llm_generated_opus47

package imztop

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
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
	// imztop registers an interactive per-window *App (its own selected
	// network interface, etc.). Screenshot capture is handled centrally by
	// the widgets TestDriver via the Demos registered in imztop_tour.go
	// (ADR-0057).
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, err error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("imztop: failed to register factory")
	}
}

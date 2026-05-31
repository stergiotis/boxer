package imzrt

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
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
	// imzrt registers an interactive per-window *App. Screenshot capture is
	// handled centrally by the widgets TestDriver via the Demos registered
	// in imzrt_tour.go (ADR-0057).
	if err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, err error) {
		a = newApp()
		return
	}); err != nil {
		log.Warn().Err(err).Msg("imzrt: failed to register factory")
	}
}

package imzrt

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// manifest is the static AppI descriptor every imzrt instance returns. imzrt
// reads the Go runtime's own metrics and mutates no runtime tunables
// (ADR-0061 SD6); the two capabilities below are the one deliberate,
// user-initiated act the dashboard performs — capturing a pprof profile of
// this process and handing it to play (ADR-0061 update 2026-08-01).
var manifest = app.Manifest{
	Id:      "github.com/stergiotis/boxer/apps/imzrt",
	Version: "0.1.0",
	Display: "imzrt",
	Title:   "imzrt",
	// Phosphor pulse — the runtime-heartbeat metaphor; distinct from imztop's
	// PhGauge (the system-monitor metaphor) so the two siblings read apart.
	Icon:     icons.PhPulse,
	Topics:   []app.TopicT{app.TopicObservability},
	Keywords: []string{"render", "telemetry", "fps", "frames", "latency"},
	Surface:  app.SurfaceWindowed,
	Caps: []app.SubjectFilter{
		{
			Pattern:   adhocdata.SubjectPublish,
			Direction: app.CapDirectionPub,
			Reason:    "imzrt: publish captured pprof profiles as ad-hoc datasets, republishing each kind onto its stable handle (ADR-0134)",
		},
		{
			Pattern:   windowhost.OpenSubject,
			Direction: app.CapDirectionPub,
			Reason:    "imzrt: Explore — open a play window seeded on a captured profile, bound to the introspection endpoint (ADR-0135)",
		},
	},
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

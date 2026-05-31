//go:build llm_generated_opus47

package configview

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// ManifestId is exported so callers can host.Open(configview.ManifestId)
// without hard-coding the string.
const ManifestId app.AppIdT = "github.com/stergiotis/boxer/public/keelson/runtime/configview"

// PhGear reads as "settings" — the operator-facing knob set is what
// this inspector surfaces, so gear beats sliders/faders (which
// connote runtime tuning, not registry inspection).
var manifest = app.Manifest{
	Id:       ManifestId,
	Version:  "0.1.0",
	Display:  "Config inspector",
	Title:    "Config inspector",
	Icon:     icons.PhGear,
	Category: "Runtime",
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		PreferredWidth:  720,
		PreferredHeight: 600,
	},
}

func init() {
	// configview registers an interactive per-window App. Screenshot
	// capture is handled centrally by the widgets TestDriver via the Demo
	// registered in configview_tour.go (ADR-0057) — there is no per-app
	// screenshot factory anymore.
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, err error) {
		a = newInstance(manifest)
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("configview: failed to register factory")
	}
}

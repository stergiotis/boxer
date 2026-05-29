//go:build llm_generated_opus47

package configview

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
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
	// Tour mode (IMZERO2_SCREENSHOT_DIR set) routes Frame through
	// the per-scene state machine in configview_tour.go via a
	// SeededFuncApp; interactive mode hands back a regular per-window
	// App. Same pattern as leewaywidgets / regex_explorer / widgets.
	var ctor app.AppCtor
	if imzero2env.ScreenshotDir.Get() != "" {
		ctor = func() (a app.AppI, err error) {
			a, err = app.NewSeededFuncApp(manifest, RenderLoopHandlerTour)
			return
		}
	} else {
		ctor = func() (a app.AppI, err error) {
			a = newInstance(manifest)
			return
		}
	}
	err := app.DefaultRegistry.RegisterFactory(manifest, ctor)
	if err != nil {
		log.Warn().Err(err).Msg("configview: failed to register factory")
	}
}

package adrboard

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// manifest is the per-process AppI descriptor. Static; every newApp() returns
// the same Manifest value.
//
// No Caps are declared: the app reads the ADR markdown directory off the local
// filesystem and touches neither the bus nor the persist store.
var manifest = app.Manifest{
	Id:       "github.com/stergiotis/boxer/apps/adrboard",
	Version:  "0.1.0",
	Display:  "ADR board",
	Title:    "ADR board — decisions and their sub-item progress",
	Icon:     icons.PhKanban,
	Category: "Docs",
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		// Wide enough for the five lifecycle lanes side by side without the
		// board scrolling horizontally: the lane pitch measured ~277pt, so
		// five need ~1400 plus the window's own chrome. Tall enough that the
		// `accepted` lane — which holds most of the corpus — shows a useful
		// run of cards.
		PreferredWidth:  1440,
		PreferredHeight: 820,
	},
}

// init registers the app into app.DefaultRegistry. Factory ctor so two open
// windows get independent App state.
func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("adrboard: failed to register factory")
	}
}

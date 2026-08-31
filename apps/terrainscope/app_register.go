package terrainscope

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
)

// manifest is the per-process AppI descriptor. Static; every newApp()
// returns the same Manifest value.
//
// No Caps are declared: Phase 1 reads swissALTI3D tiles directly from
// the filesystem (ADR-0099), so the app touches neither the bus nor the
// persist store. The ADR-0090-style headless elevation service (which
// would replace the direct read with a bus capability) is deferred to
// Phase 4.
var manifest = app.Manifest{
	Id:       "github.com/stergiotis/boxer/apps/terrainscope",
	Version:  "0.1.0",
	Display:  "Terrain scope",
	Title:    "Terrain scope — swissALTI3D line-of-sight",
	Summary:  "Compute swissALTI3D line-of-sight between two points",
	Icon:     icons.PhMountains,
	Topics:   []app.TopicT{app.TopicGeo},
	Keywords: []string{"terrain", "elevation", "line of sight", "viewshed", "swisstopo"},
	Surface:  app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{
		// Sized to fit the slippy map plus the sweep plot (and its legend)
		// below it without clipping the window body.
		PreferredWidth:  960,
		PreferredHeight: 1000,
	},
}

// init registers the app into app.DefaultRegistry. Factory ctor so two
// open windows get independent App state.
func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("terrainscope: failed to register factory")
	}
}

package terrainscope

import (
	"embed"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/apps/terrainscope/launchcfg"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"
)

// helpFS embeds the inline help (apps/terrainscope/help/*.md), indexed by the
// help library and shown by the HelpHost on F1 (ADR-0047).
//
//go:embed help/*.md
var helpFS embed.FS

// manifest is the per-process AppI descriptor. Static; every newApp()
// returns the same Manifest value.
//
// The caps are the basemap's tile destination (ADR-0262) and the task
// subjects each sweep runs under. Phase 1 reads
// swissALTI3D tiles directly from the filesystem (ADR-0099) and touches
// no persist store; the ADR-0090-style headless elevation service (which
// would replace the direct read with a bus capability) is deferred to
// Phase 4. LaunchKind declares the config another app opens a window with
// (launchcfg, ADR-0135); Operations is the catalog of terrainscope_ops.go.
var manifest = app.Manifest{
	Id:         launchcfg.AppId,
	Version:    "0.1.0",
	Display:    "Terrain scope",
	Title:      "Terrain scope — swissALTI3D line-of-sight",
	Summary:    "Compute swissALTI3D line-of-sight between two points",
	Icon:       icons.PhMountains,
	Topics:     []app.TopicT{app.TopicGeo},
	Keywords:   []string{"terrain", "elevation", "line of sight", "viewshed", "swisstopo"},
	Surface:    app.SurfaceWindowed,
	Help:       help.MustSub(helpFS, "help"),
	Caps:       append(basemap.ClientCaps("terrainscope: basemap tiles under the picked points"), task.ProducerCaps()...),
	LaunchKind: launchcfg.Kind,
	Operations: ops.Catalog(),
	SurfaceHints: app.SurfaceHints{
		// Sized to fit the slippy map plus the sweep plot (and its legend)
		// below it without clipping the window body.
		PreferredWidth:  960,
		PreferredHeight: 1000,
	},
}

// pointsSeedName is the seed variable's name, quoted where it is refused.
const pointsSeedName = "BOXER_TERRAINSCOPE_POINTS"

// PointsSeed places both points in a new window (ADR-0009 seed variable,
// the BOXER_MDEDIT_DOC shape): a scene or a headless run reaches a computed
// state without clicking the map. A caller's launch config wins over it.
var PointsSeed = env.NewString(env.Spec{
	Name:        pointsSeedName,
	Description: "observer and target for a new terrainscope window as four WGS84 decimal degrees, observer latitude, longitude, target latitude, longitude, separated by commas; the sweep starts at once; for scenes and demos",
	Category:    env.CategoryE("boxer-terrainscope"),
})

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

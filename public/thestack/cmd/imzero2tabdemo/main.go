// Command imzero2tabdemo is the browser-tab binary boxer publishes as a demo
// on GitHub Pages (ADR-0278 Update 2026-10-04): apps that need no data and no
// host service, so the published page reaches no third-party endpoint.
// tabhost is the rest — the reactor, `bundle`, `serve`, `tabreport`.
//
// It is its own `package main` for the reason imzero2tab is: the binary is
// the shipped artifact, compiled to a wasm module.
package main

import (
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/apps/splashscreen"
	"github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/gribfield/gfsdemo"
	"github.com/stergiotis/boxer/public/science/geo/vectorfield/keelsonfield"
	"github.com/stergiotis/boxer/public/thestack/imzero2/browserhost/tabhost"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"

	// the apps the demo opens; each registers itself into app.DefaultRegistry
	_ "github.com/stergiotis/boxer/apps/fibscope"
	_ "github.com/stergiotis/boxer/apps/play"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/idsshowcase"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/leewaywidgets"
)

// tab is built at package initialisation, not in main: a wasip1 reactor runs
// no main, and New registers its setup there.
var tab = newTab()

// tabHidden are the gallery demos a tab cannot run, by name, with why. The
// map demos go by their flag instead (newTab).
var tabHidden = map[string]string{
	// egui_extras' date picker reads the wall clock through jiff, whose
	// calls go to JavaScript imports the browser host leaves unwired: the
	// time-range picker on its first frame, the other two when the calendar
	// opens. Either kills the tab.
	"date-picker":       "wall clock",
	"datetime-picker":   "wall clock",
	"timerange-picker":  "wall clock",
	"layered-graph":     "graphviz layout is unavailable under wasm",
	"scrolling-texture": "the streamed texture stays blank in a tab",
	"filepicker":        "a tab has no filesystem",
	// offline by default, but a checkbox turns on tiles from a third-party
	// server, fetched by the visitor's browser
	"flowonmap": "tile checkbox",
	// trial harnesses, not showcases
	"flowbench": "trial harness",
	"landbench": "trial harness",
}

// keelsonTables are what play reads in the tab with no ClickHouse
// (ADR-0290 §SD4): the static introspection tables, and the GFS wind forecast
// as a vector field family (ADR-0291, ADR-0292). A failure leaves play
// without them rather than the demo without its other apps.
func keelsonTables() (reg *introspect.Registry) {
	reg = introspect.NewRegistry()
	if err := providers.RegisterStatic(reg); err != nil {
		log.Error().Err(err).Msg("imzero2tabdemo: the static keelson tables did not register")
		return nil
	}
	// Decoded on first read, so the demo's other apps start without it.
	if err := keelsonfield.RegisterLazy(reg, "gfs_wind", gfsdemo.Field); err != nil {
		log.Error().Err(err).Msg("imzero2tabdemo: the GFS wind did not register")
	}
	return
}

func newTab() (inst *tabhost.Program) {
	// A page on boxer's site says it loads nothing from elsewhere: maps start
	// on their offline outlines, and the tab refuses any request that would
	// leave it.
	basemap.SetOffline()
	// The map demos fetch tiles from the visitor's browser in a tab, a
	// third-party endpoint.
	widgets.HideDemos(func(d registry.Demo) bool {
		_, named := tabHidden[d.Name]
		return named || d.Flags&registry.DemoFlagNeedsNetwork != 0
	})
	inst = tabhost.New(tabhost.Options{
		DefaultApp: splashscreen.ManifestId,
		Services:   tabhost.Services{KeelsonSQL: keelsonTables(), NoEgress: true},
		// An applet document published beside the page — the repository's
		// complexity map the demo build writes — opens by
		// BOXER_SQLAPPLET_TAB_DOC (ADR-0299). The committed applets
		// are not minted: nearly all of them read tables a tab does not have.
		// play's SQL rewrites are not registered either, unlike imzero2tab:
		// nothing the demo runs needs them — keelson reads and literal rows —
		// and each pass re-parses the statement, which on the complexity
		// map's 86 KB of rows came to about 4 s natively and stalled the tab.
		Prepare: sqlapplet.LoadTabApplet,
	},
		&cli.Command{Name: "imzero2tabdemo", Version: vcs.BuildVersionInfo(), Before: logging.Apply})
	return
}

func main() {
	tab.Main()
}

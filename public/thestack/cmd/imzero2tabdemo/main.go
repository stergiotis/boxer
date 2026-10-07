// Command imzero2tabdemo is the browser-tab binary boxer publishes as a demo
// on GitHub Pages (ADR-0278 Update 2026-10-04): apps that need no data and no
// host service, so the published page reaches no third-party endpoint.
// tabhost is the rest — the reactor, `bundle`, `serve`, `tabreport`.
//
// It is its own `package main` for the reason imzero2tab is: the binary is
// the shipped artifact, compiled to a wasm module.
package main

import (
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/apps/splashscreen"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/thestack/imzero2/browserhost/tabhost"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"

	// the apps the demo opens; each registers itself into app.DefaultRegistry
	_ "github.com/stergiotis/boxer/apps/fibscope"
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

func newTab() (inst *tabhost.Program) {
	// The map demos fetch tiles from the visitor's browser in a tab, a
	// third-party endpoint.
	widgets.HideDemos(func(d registry.Demo) bool {
		_, named := tabHidden[d.Name]
		return named || d.Flags&registry.DemoFlagNeedsNetwork != 0
	})
	inst = tabhost.New(tabhost.Options{DefaultApp: splashscreen.ManifestId},
		&cli.App{Name: "imzero2tabdemo", Version: vcs.BuildVersionInfo(), Before: logging.Apply})
	return
}

func main() {
	tab.Main()
}

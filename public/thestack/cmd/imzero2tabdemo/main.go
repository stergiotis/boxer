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

	// the apps the demo opens; each registers itself into app.DefaultRegistry
	_ "github.com/stergiotis/boxer/apps/fibscope"
)

// tab is built at package initialisation, not in main: a wasip1 reactor runs
// no main, and New registers its setup there.
var tab = tabhost.New(tabhost.Options{DefaultApp: splashscreen.ManifestId},
	&cli.App{Name: "imzero2tabdemo", Version: vcs.BuildVersionInfo(), Before: logging.Apply})

func main() {
	tab.Main()
}

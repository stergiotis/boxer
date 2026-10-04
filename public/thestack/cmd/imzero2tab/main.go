// Command imzero2tab is the keelson host for a browser tab (ADR-0263): the
// Go module a Web Worker runs beside the Rust browser host. It links the
// apps a tab may open; tabhost (ADR-0278, proposed) is the rest — the mount
// on an in-process bus, the wasip1 reactor, the native pipe run that
// exercises the tab's Go side without a browser, and `serve`.
//
// Built with `GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared` by
// scripts/dev/build_tab_bundle.sh.
//
// It is its own `package main`, against the standard's preference for a
// subcommand of ./boxer.sh, for the same reason the desktop host is: the
// binary is the shipped artifact. A wasm module is what this command
// compiles to, and boxer's tree does not compile for wasm and should not
// be asked to. It keeps the standard's other half — a urfave/cli app with
// the shared logging and version wiring — so the entry-point audit passes
// it on its merits, not by baseline.
package main

import (
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/thestack/imzero2/browserhost/tabhost"

	// the apps a tab may open; each registers itself into app.DefaultRegistry
	_ "github.com/stergiotis/boxer/apps/fibscope"
	_ "github.com/stergiotis/boxer/apps/mdedit"
	_ "github.com/stergiotis/boxer/apps/play"
	_ "github.com/stergiotis/boxer/apps/taskdemo"
)

// tab is built at package initialisation, not in main: a wasip1 reactor runs
// no main, and New registers its setup there.
var tab = tabhost.New(tabhost.Options{DefaultApp: "github.com/stergiotis/boxer/apps/play"},
	&cli.App{Name: "imzero2tab", Version: vcs.BuildVersionInfo(), Before: logging.Apply})

func main() {
	tab.Main()
}

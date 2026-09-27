// Command imzero2tab is the keelson host for a browser tab (ADR-0263): the
// Go module a Web Worker runs beside the Rust browser host. It links the
// apps a tab may open and mounts one of them on an in-process bus
// (browserhost.Mount); the worker calls its reactor exports per tick, the
// Rust host renders, and the existing viewer page paints.
//
// Built with `GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared` by
// scripts/dev/build_tab_bundle.sh. Natively it runs the same app against a
// client binary over the pipe, which is how the tab's Go side is exercised
// without a browser, and its `serve` subcommand serves a bundle.
package main

import (
	"encoding/binary"
	"os"
	"os/signal"
	"runtime"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/vcs"
	fffiruntime "github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	"github.com/stergiotis/boxer/public/thestack/imzero2/application"
	"github.com/stergiotis/boxer/public/thestack/imzero2/browserhost"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"

	// the apps a tab may open; each registers itself into app.DefaultRegistry
	_ "github.com/stergiotis/boxer/apps/fibscope"
	_ "github.com/stergiotis/boxer/apps/mdedit"
	_ "github.com/stergiotis/boxer/apps/play"
	_ "github.com/stergiotis/boxer/apps/taskdemo"
)

// defaultApp is what a tab opens when the page names nothing.
const defaultApp = "github.com/stergiotis/boxer/apps/play"

// reactorStep is what the action leaves behind under the reactor: the
// per-tick step the worker calls. Natively the action runs the loop itself
// and leaves it nil.
var reactorStep func() int32

func main() {
	run(os.Args[1:])
}

// run is main's body with the arguments passed in: the wasip1 reactor's
// setup export hands the host's arguments to it (reactor_wasip1.go), and it
// returns the per-tick step for the host to call, nil when there is none.
func run(args []string) (step func() int32) {
	cliApp := &cli.App{
		Name:    "imzero2tab",
		Usage:   "the keelson host for a browser tab: one registered app on an in-process bus",
		Version: vcs.BuildVersionInfo(),
		Flags: append([]cli.Flag{
			&cli.StringFlag{Name: "app", Value: defaultApp, Usage: "the registered app id to mount"},
			&cli.StringFlag{Name: "clientBinary", Usage: "native runs: launch this imzero2 client as the peer; unused under the reactor, where the worker is the peer"},
		}, logging.LoggingFlags...),
		Before: logging.Apply,
		Action: tab,
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "serve a bundle build_tab_bundle.sh wrote: its files, /ch/ proxied to ClickHouse, the worker's log and report sinks",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "dir", Required: true, Usage: "the bundle directory"},
					&cli.StringFlag{Name: "listen", Value: "127.0.0.1:8765", Usage: "bind address; port 0 picks one, printed as `PORT <n>`"},
					&cli.StringFlag{Name: "chURL", Value: "http://127.0.0.1:8123/", Usage: "the ClickHouse HTTP endpoint /ch/ proxies to"},
					&cli.BoolFlag{Name: "exitOnReport", Usage: "end after the first POST /report (the trial's browser arms)"},
				},
				Action: serve,
			},
		},
	}
	if err := cliApp.Run(append([]string{"imzero2tab"}, args...)); err != nil {
		log.Error().Err(err).Msg("imzero2tab")
	}
	return reactorStep
}

func tab(ctx *cli.Context) (err error) {
	// Deliberate and once: the module's HTTP leaves through the host.
	browserhost.InstallHostTransport()
	appId := app.AppIdT(ctx.String("app"))
	if _, ok := app.DefaultRegistry.LookupManifest(appId); !ok {
		return browserhost.ErrNoSuchApp
	}
	cfg := &application.Config{ClientBinary: ctx.String("clientBinary")}
	cfg.Validate(true)
	u := fffiruntime.NewUnmarshaller(nil, binary.NativeEndian, nil, nil)
	inst, err := application.NewApplication(cfg, u)
	if err != nil {
		return eh.Errorf("imzero2tab: application: %w", err)
	}
	inst.FffiEstablishedHandler = func(fffi *fffiruntime.Fffi2[*fffiruntime.Unmarshaller]) error {
		typed.SetCurrentFffiVar(fffi)
		return nil
	}
	ids := c.NewWidgetIdStack()
	var mounted *browserhost.Mounted
	inst.RenderLoopHandler = func() (err error) {
		st := c.CurrentApplicationState
		st.StartServersideFrame()
		ids.Reset()
		if mounted == nil {
			mounted, err = browserhost.Mount(appId, ids, log.Logger)
			if err != nil {
				c.Label("imzero2tab: " + err.Error()).Send()
				err = nil
			}
		}
		if mounted != nil {
			mounted.Frame(ids)
		}
		st.FinishServersideFrame()
		return
	}
	if err = inst.Launch(); err != nil {
		return eh.Errorf("imzero2tab: launch: %w", err)
	}
	if runtime.GOOS == "wasip1" {
		// The worker owns the cadence: hand it the step and return.
		reactorStep, err = browserhost.StepLoop(inst, func(e error) {
			if e != nil {
				log.Error().Err(e).Msg("imzero2tab: the render loop stopped with an error")
			}
			if mounted != nil {
				_ = mounted.Unmount()
			}
		})
		return
	}
	err = inst.Run()
	if mounted != nil {
		_ = mounted.Unmount()
	}
	return
}

func serve(ctx *cli.Context) (err error) {
	sigCtx, stop := signal.NotifyContext(ctx.Context, os.Interrupt)
	defer stop()
	return browserhost.Serve(sigCtx, browserhost.ServeConfig{
		Dir:          ctx.String("dir"),
		Listen:       ctx.String("listen"),
		ChURL:        ctx.String("chURL"),
		ExitOnReport: ctx.Bool("exitOnReport"),
	}, log.Logger)
}

// Package tabhost is a browser-tab binary's body as a library (ADR-0278 SD1,
// proposed): the program that mounts one registered keelson app on an
// in-process bus, runs it as a wasip1 reactor inside the page's worker
// (ADR-0263), runs the same app natively against a client binary over the
// pipe, builds a bundle and serves one, and reports what its apps declare
// that a tab does not serve. A tab binary is a `package main`
// that imports the apps it may open and hands its cli.App to [New]:
//
//	var tab = tabhost.New(tabhost.Options{DefaultApp: "example.com/acme/apps/dashboard"},
//		&cli.App{Name: "acmetab", Version: vcs.BuildVersionInfo(), Before: logging.Apply})
//
//	func main() { tab.Main() }
//
// The cli.App stays the binary's own, so its name and version are the
// binary's and the entry-point standard holds where it is checked, in the
// main package. [New] adds the flags, the action and the subcommands.
//
// The variable matters: a c-shared wasip1 module runs package initialisation
// but never main, so [New] registers the reactor's setup itself; natively
// [Program.Main] runs the program on the process arguments.
package tabhost

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
	fffiruntime "github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	"github.com/stergiotis/boxer/public/thestack/imzero2/application"
	"github.com/stergiotis/boxer/public/thestack/imzero2/browserhost"
	"github.com/stergiotis/boxer/public/thestack/imzero2/browserhost/web"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Services selects the services the tab boots beside the mounted app, as
// hostboot.Services does for the native host (ADR-0278 SD2, proposed). The
// zero value is the bus and the app; each service the tab gains is added
// here as a field, so a binary turns it on rather than building it.
type Services struct{}

// Options configure a tab binary.
type Options struct {
	// DefaultApp is the registered app id mounted when -app is not given.
	DefaultApp app.AppIdT
	// Services are the in-tab services to boot (none exist yet).
	Services Services
}

// Program is one tab binary: its options, its cli.App, and the per-tick
// step the reactor leaves behind.
type Program struct {
	opts Options
	app  *cli.App
	step func() int32
	err  error
}

// New builds the program around cliApp, which carries the binary's name,
// version and logging hook; New sets its Usage when empty, appends its flags
// and sets its action and subcommands. Under wasip1 it also registers the
// program as the reactor's setup. Call it once, from a package-level
// variable of the main package.
func New(opts Options, cliApp *cli.App) (inst *Program) {
	inst = &Program{opts: opts, app: cliApp}
	if cliApp.Usage == "" {
		cliApp.Usage = "the keelson host for a browser tab: one registered app on an in-process bus"
	}
	cliApp.Flags = append(append(cliApp.Flags,
		&cli.StringFlag{Name: "app", Value: string(opts.DefaultApp), Usage: "the registered app id to mount"},
		&cli.StringFlag{Name: "clientBinary", Usage: "native runs: launch this imzero2 client as the peer; unused under the reactor, where the worker is the peer"},
	), logging.LoggingFlags...)
	cliApp.Action = inst.tab
	cliApp.Commands = append(cliApp.Commands, &cli.Command{
		Name:  "serve",
		Usage: "serve a tab bundle: its files, the page, worker and shim this binary embeds where the bundle lacks them, /ch/ proxied to ClickHouse, the worker's log and report sinks",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "dir", Required: true, Usage: "the bundle directory"},
			&cli.StringFlag{Name: "listen", Value: "127.0.0.1:8765", Usage: "bind address; port 0 picks one, printed as `PORT <n>`"},
			&cli.StringFlag{Name: "chURL", Value: "http://127.0.0.1:8123/", Usage: "the ClickHouse HTTP endpoint /ch/ proxies to"},
			&cli.BoolFlag{Name: "exitOnReport", Usage: "end after the first POST /report (the trial's browser arms)"},
		},
		Action: serve,
	}, bundleCommand(), hostDigestCommand(), reportCommand(inst))
	registerReactor(inst)
	return
}

// Main runs the program on the process arguments and exits non-zero when it
// failed. Under wasip1 a reactor never calls it; the worker's setup runs the
// program instead.
func (inst *Program) Main() {
	if inst.run(os.Args[1:]); inst.err != nil {
		os.Exit(1)
	}
}

// run is the program on args; it returns the per-tick step when the action
// left one (the reactor), nil otherwise, and keeps the error for Main.
func (inst *Program) run(args []string) (step func() int32) {
	if inst.err = inst.app.Run(append([]string{inst.app.Name}, args...)); inst.err != nil {
		log.Error().Err(inst.err).Str("binary", inst.app.Name).Msg("tabhost")
	}
	return inst.step
}

func (inst *Program) tab(ctx *cli.Context) (err error) {
	// Deliberate and once: the module's HTTP leaves through the host.
	browserhost.InstallHostTransport()
	appId := app.AppIdT(ctx.String("app"))
	if _, ok := app.DefaultRegistry.LookupManifest(appId); !ok {
		return browserhost.ErrNoSuchApp
	}
	cfg := &application.Config{ClientBinary: ctx.String("clientBinary")}
	cfg.Validate(true)
	u := fffiruntime.NewUnmarshaller(nil, binary.NativeEndian, nil, nil)
	host, err := application.NewApplication(cfg, u)
	if err != nil {
		return eh.Errorf("tabhost: application: %w", err)
	}
	host.FffiEstablishedHandler = func(fffi *fffiruntime.Fffi2[*fffiruntime.Unmarshaller]) error {
		typed.SetCurrentFffiVar(fffi)
		return nil
	}
	ids := c.NewWidgetIdStack()
	var mounted *browserhost.Mounted
	host.RenderLoopHandler = func() (err error) {
		st := c.CurrentApplicationState
		st.StartServersideFrame()
		ids.Reset()
		if mounted == nil {
			mounted, err = browserhost.Mount(appId, ids, log.Logger)
			if err != nil {
				c.Label("tabhost: " + err.Error()).Send()
				err = nil
			}
		}
		if mounted != nil {
			mounted.Frame(ids)
		}
		st.FinishServersideFrame()
		return
	}
	if err = host.Launch(); err != nil {
		return eh.Errorf("tabhost: launch: %w", err)
	}
	if runtime.GOOS == "wasip1" {
		// The worker owns the cadence: hand it the step and return.
		inst.step, err = browserhost.StepLoop(host, func(e error) {
			if e != nil {
				log.Error().Err(e).Msg("tabhost: the render loop stopped with an error")
			}
			if mounted != nil {
				_ = mounted.Unmount()
			}
		})
		return
	}
	err = host.Run()
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
		Assets:       web.Assets(),
	}, log.Logger)
}

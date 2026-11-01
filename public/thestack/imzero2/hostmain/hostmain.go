// Package hostmain is the imzero2 host binary's body as a library: the
// command tree of the desktop and headless host — the `imzero2` group with
// `demo`, `drive` and `scene`, the observability and watchbill commands —
// with the logging, profiling, tracing, debugger and coverage flags, and the
// exit hooks that flush them. It is the native counterpart of
// [github.com/stergiotis/boxer/public/thestack/imzero2/browserhost/tabhost]
// (ADR-0278), and reads the same way. A host binary is a `package main` that
// imports the apps it adds and hands its root cli.Command to [New]:
//
//	var host = hostmain.New(hostmain.Options{},
//		&cli.Command{Name: "acme", Version: vcs.BuildVersionInfo(), Before: logging.Apply})
//
//	func main() { host.Main() }
//
// Apps register at package initialisation into app.DefaultRegistry, so a
// blank import of an app's package is what makes `imzero2 demo --launch` and
// `imzero2 scene` see it. The carousel, which carries the `demo` command, links
// boxer's own apps; a binary built on this package adds its own beside them.
//
// The root cli.Command stays the binary's own, so its name and version are
// the binary's and the entry-point standard holds where it is checked, in the
// main package. [New] adds the flags, the subcommands and the exit hook.
package hostmain

import (
	"context"
	"os"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"

	watchbillcli "github.com/stergiotis/boxer/public/app/commands/watchbill"
	"github.com/stergiotis/boxer/public/dev"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
	"github.com/stergiotis/boxer/public/keelson/runtime/loghost"
	"github.com/stergiotis/boxer/public/observability"
	"github.com/stergiotis/boxer/public/observability/coverage"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/profiling"
	"github.com/stergiotis/boxer/public/observability/profiling/pprofhttp"
	"github.com/stergiotis/boxer/public/observability/tracing"
	"github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient/drivecmd"
	demo "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/carousel"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/driver"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene/scenecmd"
	"github.com/stergiotis/boxer/showcase/deploy"
)

// Options configure a host binary. The zero value is the stock host; each
// choice a binary gains is added here as a field, as tabhost.Options does
// for the tab.
type Options struct {
}

// Program is one host binary: its root cli.Command and the log bridge the
// `imzero2` group installs, which the exit hook closes.
type Program struct {
	opts           Options
	app            *cli.Command
	closeLogBridge func() error
}

// New builds the program around cliApp, which carries the binary's name,
// version and logging hook; New appends the host's flags and commands and
// sets the exit hook, after an After the binary set. Call it once.
func New(opts Options, cliApp *cli.Command) (inst *Program) {
	inst = &Program{opts: opts, app: cliApp, closeLogBridge: logbridge.NopCloser()}
	cliApp.Flags = slices.Concat(cliApp.Flags,
		logging.LoggingFlags,
		profiling.ProfilingFlags,
		pprofhttp.Flags,
		tracing.TracingFlags,
		dev.DebuggerFlags,
		dev.IoOverrideFlags,
		coverage.CoverageFlags)
	cliApp.Commands = append(cliApp.Commands,
		&cli.Command{
			Name: "imzero2",
			// Install the bridge in the subcommand's Before so it runs AFTER
			// the root's Before has installed the configured log.Logger via
			// logging.Apply. The bridge wraps the final writer; running here
			// keeps the wrap order (writer → bridge) regardless of --logFormat.
			Before: func(ctx context.Context, cmd *cli.Command) (_ context.Context, err error) {
				inst.closeLogBridge = loghost.Install(ctx)
				return
			},
			Commands: []*cli.Command{
				demo.NewCommand(),
				driver.NewCliCommand(),
				deploy.NewCommand(),
				drivecmd.NewCommand(),
				scenecmd.NewCommand(),
			},
		},
		observability.NewCliCommand(),
		// The job child of ADR-0344 §SD4 is this binary running
		// `watchbill run --job <id>`; the verb lives here for that.
		watchbillcli.NewCliCommand(),
	)
	after := cliApp.After
	cliApp.After = func(ctx context.Context, cmd *cli.Command) (err error) {
		if after != nil {
			err = after(ctx, cmd)
		}
		profiling.ProfilingHandleExit(ctx, cmd)
		tracing.TracingHandleExit(ctx, cmd)
		_ = inst.closeLogBridge()
		return
	}
	return
}

// Main runs the program on the process arguments and exits non-zero when it
// failed.
func (inst *Program) Main() {
	if err := inst.Run(context.Background(), os.Args); err != nil {
		os.Exit(1)
	}
}

// Run runs the program on args, the program name first as in os.Args, and
// logs the error it returns.
func (inst *Program) Run(ctx context.Context, args []string) (err error) {
	err = inst.app.Run(ctx, args)
	if err != nil {
		log.Error().Stack().Err(err).Msg("an error occurred")
	}
	return
}

package main

import (
	"os"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/dev"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
	"github.com/stergiotis/boxer/public/keelson/runtime/loghost"
	"github.com/stergiotis/boxer/public/observability"
	"github.com/stergiotis/boxer/public/observability/coverage"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/profiling"
	"github.com/stergiotis/boxer/public/observability/tracing"
	"github.com/stergiotis/boxer/public/observability/vcs"
	demo2 "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/carousel"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/driver"
	"github.com/stergiotis/boxer/showcase/deploy"
	"github.com/urfave/cli/v2"
)

func mainC() (exitCode int) {
	//defer ph.PanicHandler(2, nil, nil)
	closeLogBridge := logbridge.NopCloser()
	app := cli.App{
		Name:                 vcs.ModuleInfo(),
		Copyright:            vcs.CopyrightInfo(),
		HelpName:             "",
		Usage:                "",
		UsageText:            "",
		ArgsUsage:            "",
		Version:              vcs.BuildVersionInfo(),
		Description:          "",
		DefaultCommand:       "",
		EnableBashCompletion: false,
		Flags: slices.Concat(
			logging.LoggingFlags,
			profiling.ProfilingFlags,
			tracing.TracingFlags,
			dev.DebuggerFlags,
			dev.IoOverrideFlags,
			coverage.CoverageFlags),
		Commands: []*cli.Command{
			{
				Name: "imzero2",
				// Install the bridge in the subcommand's Before so it
				// runs AFTER App.Before has installed the configured
				// log.Logger via logging.Apply. The bridge wraps the
				// final writer; running here keeps the wrap order
				// (writer → bridge) regardless of --logFormat.
				Before: func(c *cli.Context) (err error) {
					closeLogBridge = loghost.Install(c.Context)
					return
				},
				Subcommands: []*cli.Command{
					demo2.NewCommand(),
					driver.NewCliCommand(),
					deploy.NewCommand(),
				},
			},
			observability.NewCliCommand(),
		},
		Before: logging.Apply,
		After: func(context *cli.Context) error {
			profiling.ProfilingHandleExit(context)
			tracing.TracingHandleExit(context)
			_ = closeLogBridge()
			return nil
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		exitCode = 1
		log.Error().Stack().Err(err).Msg("an error occurred")
	}
	return
}

func main() {
	exitCode := mainC()
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

//go:build llm_generated_opus46

package main

import (
	"os"
	"slices"

	"github.com/stergiotis/boxer/public/dev"
	"github.com/stergiotis/boxer/public/observability"
	"github.com/stergiotis/boxer/public/observability/coverage"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/profiling"
	"github.com/stergiotis/boxer/public/observability/tracing"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/driver"
	"github.com/urfave/cli/v2"
)

import (
	"github.com/rs/zerolog/log"
)

func mainC() (exitCode int) {
	app := cli.App{
		Name:                 "egui2gen",
		Copyright:            vcs.CopyrightInfo(),
		Usage:                "FFFI2 code generator for egui2 widgets",
		Version:              vcs.BuildVersionInfo(),
		EnableBashCompletion: false,
		Flags: slices.Concat(
			logging.LoggingFlags,
			profiling.ProfilingFlags,
			tracing.TracingFlags,
			dev.DebuggerFlags,
			dev.IoOverrideFlags,
			coverage.CoverageFlags),
		Commands: []*cli.Command{
			driver.NewCliCommand(),
			observability.NewCliCommand(),
		},
		Before: logging.Apply,
		After: func(context *cli.Context) error {
			profiling.ProfilingHandleExit(context)
			tracing.TracingHandleExit(context)
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

package main

import (
	"os"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl"
	"github.com/stergiotis/boxer/public/dev"
	"github.com/stergiotis/boxer/public/docgen"
	"github.com/stergiotis/boxer/public/fffi/compiletime"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/ph"
	"github.com/stergiotis/boxer/public/observability/profiling"
	"github.com/stergiotis/boxer/public/observability/tracing"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/semistructured/cbor"
	et7 "github.com/stergiotis/boxer/public/semistructured/et7/cli"
	"github.com/urfave/cli/v2"
)

func mainC() (exitCode int) {
	logging.SetupZeroLog()
	defer ph.PanicHandler(2, nil, nil)
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
			docgen.DocFlags,
			dev.DebuggerFlags,
			dev.IoOverrideFlags),
		Commands: []*cli.Command{
			dsl.NewCommand(),
			cbor.NewCommand(),
			compiletime.NewCommand(nil, nil),
			et7.NewCommand(),
			tracing.NewCliCommand(),
			docgen.NewDocCli(),
			dev.NewCliCommand(),
		},
		After: func(context *cli.Context) error {
			profiling.ProfilingHandleExit(context)
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

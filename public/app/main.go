package main

import (
	"os"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl"
	"github.com/stergiotis/boxer/public/dev"
	"github.com/stergiotis/boxer/public/fffi/compiletime"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/ph"
	"github.com/stergiotis/boxer/public/observability/profiling"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/semistructured/cbor"
	et7 "github.com/stergiotis/boxer/public/semistructured/et7/cli"
	"github.com/urfave/cli/v2"
)

func main() {
	exitCode := 0
	logging.SetupZeroLog()
	var _ = exitCode
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
			dev.DebuggerFlags,
			dev.IoOverrideFlags),
		Commands: []*cli.Command{
			dsl.NewCommand(),
			cbor.NewCommand(),
			compiletime.NewCommand(nil, nil),
			et7.NewCommand(),
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
	os.Exit(exitCode)
}

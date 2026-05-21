package main

import (
	"os"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/code"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/config/env/envdoc"
	"github.com/stergiotis/boxer/public/dev"
	"github.com/stergiotis/boxer/public/docgen"
	"github.com/stergiotis/boxer/public/gov"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/math/numerical/finddivisions"
	"github.com/stergiotis/boxer/public/observability"
	"github.com/stergiotis/boxer/public/observability/coverage"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/ph"
	"github.com/stergiotis/boxer/public/observability/profiling"
	"github.com/stergiotis/boxer/public/observability/tracing"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/semistructured/cbor"
	lw "github.com/stergiotis/boxer/public/semistructured/leeway/cli"
	"github.com/urfave/cli/v2"

	// Side-effect imports: load env-var Specs from packages that are not
	// otherwise referenced by this binary, so `boxer env list` and the
	// envdoc generator see the full registered set (ADR-0009 §4).
	_ "github.com/stergiotis/boxer/public/llm/openaichat"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
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
		Flags: cli2.FlagsNilRemoved(
			logging.LoggingFlags,
			profiling.ProfilingFlags,
			tracing.TracingFlags,
			docgen.DocFlags,
			dev.DebuggerFlags,
			dev.IoOverrideFlags,
			coverage.CoverageFlags),
		Commands: cli2.CommandsNilRemoved(
			eb.NewCliCommand(),
			cbor.NewCommand(),
			lw.NewCliCommand(),
			observability.NewCliCommand(),
			docgen.NewDocCli(),
			dev.NewCliCommand(),
			env.NewCliCommand(envdoc.NewGenDocsCommand()),
			gov.NewCliCommand(),
			finddivisions.NewCliCommand(),
			code.NewCliCommand(),
		),
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

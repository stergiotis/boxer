package main

import (
	"os"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/app/commands/adr"
	"github.com/stergiotis/boxer/public/app/commands/capslock"
	"github.com/stergiotis/boxer/public/app/commands/codedriven"
	"github.com/stergiotis/boxer/public/app/commands/compression"
	"github.com/stergiotis/boxer/public/app/commands/datasource"
	"github.com/stergiotis/boxer/public/app/commands/designsystem"
	"github.com/stergiotis/boxer/public/app/commands/egui2gen"
	"github.com/stergiotis/boxer/public/app/commands/findAnchor"
	"github.com/stergiotis/boxer/public/app/commands/http"
	"github.com/stergiotis/boxer/public/app/commands/iconsgen"
	"github.com/stergiotis/boxer/public/app/commands/keelsoncodec"
	"github.com/stergiotis/boxer/public/app/commands/key"
	"github.com/stergiotis/boxer/public/app/commands/runtimecodegen"
	"github.com/stergiotis/boxer/public/app/commands/sample"
	"github.com/stergiotis/boxer/public/app/commands/swisstopo"
	"github.com/stergiotis/boxer/public/app/commands/sysmetricsd"
	"github.com/stergiotis/boxer/public/app/commands/watch"
	"github.com/stergiotis/boxer/public/code"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/config/env/envdoc"
	badgercli "github.com/stergiotis/boxer/public/db/badger/cli"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/genbuildertest"
	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql"
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
	_ "github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspecthost"
	_ "github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspecthttp"
	_ "github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	_ "github.com/stergiotis/boxer/public/llm/openaichat"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
)

func mainC() (exitCode int) {
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
			code.NewCliCommand(genbuildertest.NewCliCommand()),
			text2sql.NewCliCommand(),
			badgercli.NewCliCommandBadger(),
			// Ported from pebble2impl app/commands (P9). cbor/leeway/observability/
			// dev/env(=envgen)/gov are intentionally omitted as boxer wires them
			// from their home packages above; adversarialreview/clarityrate are
			// dropped (depended on the absent cmd/adversarial-review tree).
			adr.NewCliCommand(),
			capslock.NewCliCommand(),
			codedriven.NewCliCommand(),
			compression.NewCliCommand(),
			datasource.NewCliCommand(),
			designsystem.NewCliCommand(),
			findAnchor.NewCliCommand(),
			http.NewCliCommand(),
			key.NewCliCommand(),
			runtimecodegen.NewCliCommand(),
			sample.NewCliCommand(),
			swisstopo.NewCliCommand(),
			sysmetricsd.NewCliCommand(),
			watch.NewCliCommand(),
			// Codegen tools folded from cmd/* mains (entry-point standard).
			egui2gen.NewCliCommand(),
			iconsgen.NewCliCommand(),
			keelsoncodec.NewCliCommand(),
		),
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

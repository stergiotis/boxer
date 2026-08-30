package demo

import (
	gocontext "context"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/config"

	"github.com/stergiotis/boxer/apps/capinspector"
	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/audit"
	"github.com/stergiotis/boxer/public/keelson/runtime/hostboot"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providersgodep"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	"github.com/stergiotis/boxer/public/thestack/imzero2/application"
)

// signalShutdownGrace bounds how long a signal-driven shutdown waits for the
// render loop to reach a frame boundary before forcing the process down.
const signalShutdownGrace = 10 * time.Second

func NewCommand() *cli.Command {
	cfg := &application.Config{
		MainFontTTF:            "",
		ImZeroSkiaClientConfig: &application.ImZeroClientConfig{},
	}
	return &cli.Command{
		Name: "demo",
		Description: "Launch the imzero2 demo carousel.\n\n" +
			"Discovery:\n" +
			"   imzero2 demo --list                  # PrettyCompact table of every registered app\n" +
			"   imzero2 demo --list --list-format Markdown\n" +
			"   imzero2 demo --list --list-output apps.arrow   # also dump Arrow IPC stream\n" +
			"\n" +
			"Non-interactive launch — bare identifier shorthand:\n" +
			"   imzero2 demo --launch play           # shorthand for: subject_alias = 'play'\n" +
			"   imzero2 demo --launch regex_explorer\n" +
			"\n" +
			"For richer predicates, --launch is a SQL WHERE clause over the\n" +
			"registered-applications table from --list; the runtime wraps it as\n" +
			"`SELECT id FROM table WHERE <expr>` and evaluates via clickhouse-local:\n" +
			"   imzero2 demo --launch \"subject_alias IN ('play','widgets')\"\n" +
			"   imzero2 demo --launch \"legacy_code = 5\"\n" +
			"   imzero2 demo --launch \"has(topics, 'observability') AND kind = 'app'\"\n" +
			"\n" +
			"See doc/howto/launch-apps-non-interactively.md for the full recipe.",
		Flags: slices.Concat(cfg.ToCliFlags(config.IdentityNameTransf, config.IdentityNameTransf),
			[]cli.Flag{
				&cli.StringFlag{
					Name:  "launch",
					Usage: "bare identifier `play` (shorthand for `subject_alias = 'play'`) or a SQL WHERE clause over the registered-applications table; the runtime wraps it as `SELECT id FROM table WHERE <expr>` and evaluates via clickhouse-local (run --list to see the column set)",
					Value: "",
				},
				&cli.StringSliceFlag{
					Name:  "launch-config",
					Usage: "seed one window with a launch config: `<alias>=<path>` where the file holds the config encoded for the app's LaunchKind (repeatable; see --list for aliases)",
				},
				&cli.BoolFlag{
					Name:  "list",
					Usage: "print the registered applications as a table and exit (no client launch)",
				},
				&cli.StringFlag{
					Name:  "list-output",
					Usage: "with --list: also write the raw Arrow IPC stream to this file (for downstream clickhouse-local queries)",
					Value: "",
				},
				&cli.StringFlag{
					Name:  "list-format",
					Usage: "with --list: clickhouse-local --output-format name (PrettyCompact, Pretty, Vertical, Markdown, JSONEachRow, TSV, ...)",
					Value: "PrettyCompact",
				},
			},
		),
		Action: func(context *cli.Context) error {
			if context.Bool("list") {
				return renderManifestList(
					runtimeapp.AllManifests(),
					context.String("list-output"),
					context.String("list-format"),
					os.Stdout,
				)
			}
			nMessages := cfg.FromContext(config.IdentityNameTransf, context)
			if nMessages > 0 {
				return eb.Build().Int("nMessages", nMessages).Errorf("unable to create config")
			}
			return Run(context.Context, cfg, RunOptions{
				Launch:        context.String("launch"),
				LaunchConfigs: context.StringSlice("launch-config"),
			})
		},
	}
}

// RunOptions are the carousel's own inputs beside the application config:
// the --launch expression and the --launch-config specs.
type RunOptions struct {
	// Launch is a bare alias or a SQL WHERE clause over the registered
	// applications (see NewCommand's description); empty seeds no window.
	Launch string
	// LaunchConfigs are `<alias>=<path>` seeds; see parseLaunchConfigs.
	LaunchConfigs []string
}

// Run is the carousel harness behind `imzero2 demo`, callable from another
// command: an adopter that mounts this package's apps in its own binary
// seeds whatever its app reads (env variables, a config file) and runs the
// same host — every service on, the carousel's registrations — without
// re-hosting the *cli.Command. cfg is the parsed application config.
func Run(ctx gocontext.Context, cfg *application.Config, opts RunOptions) (err error) {
	// Applet manifests are minted before launch resolution so an applet
	// alias resolves like any registered app.
	appletCount, appletErrs := sqlapplet.MintManifests(log.Logger)
	for _, mintErr := range appletErrs {
		log.Warn().Err(mintErr).Msg("sqlapplet: mint")
	}
	if appletCount > 0 {
		log.Info().Int("applets", appletCount).Msg("sqlapplet: manifests minted")
	}
	launchApps, resolveErr := resolveLaunchSql(opts.Launch)
	if resolveErr != nil {
		err = eb.Build().Str("launch", opts.Launch).Errorf("--launch: %w", resolveErr)
		return
	}
	seeds, seedErr := parseLaunchConfigs(opts.LaunchConfigs)
	if seedErr != nil {
		err = eh.Errorf("--launch-config: %w", seedErr)
		return
	}
	// The runtime bootstrap is hostboot's (ADR-0211); the carousel is its
	// every-service caller and adds what is carousel-specific: the
	// capability inspector's audit counters and backend labels, the play
	// host's passes and vocabularies, the applet store.
	rt, bootErr := hostboot.Boot(ctx, hostboot.Options{
		Log:              log.Logger,
		Services:         hostboot.AllServices(),
		LaunchApps:       launchApps,
		SeedWindows:      seeds,
		ExtraAuditSinks:  []audit.AuditSinkI{capinspector.Tally},
		VideoOutput:      true,
		HelpHost:         true,
		BeforeFirstFrame: widgets.BeforeFirstFrameInitHandler,
		AfterHost:        registerHostExtras,
		ShutdownGrace:    signalShutdownGrace,
	})
	if bootErr != nil {
		err = eh.Errorf("carousel: boot: %w", bootErr)
		return
	}
	labelBackends(rt)
	err = rt.Run(cfg)
	return
}

// registerHostExtras is the carousel's hostboot.Options.AfterHost hook: the
// applet store on the bus, the play host's passes, components and SQL
// vocabulary, and the Go-dependency introspection tables.
func registerHostExtras(rt *hostboot.Runtime) (err error) {
	appletStore, storeErr := sqlapplet.StartStore(rt.Bus, log.Logger)
	if storeErr != nil {
		log.Warn().Err(storeErr).Msg("sqlapplet: applet store unavailable")
	} else {
		rt.OnClose(appletStore.Stop)
	}
	if passErr := passregdefaults.RegisterDefaults(); passErr != nil {
		log.Warn().Err(passErr).Msg("passreg: standard pass registration failed")
	}
	if passErr := play.RegisterPasses(passreg.Default); passErr != nil {
		log.Warn().Err(passErr).Msg("passreg: play host pass registration failed")
	}
	if compErr := play.RegisterComponents(componentsql.Default); compErr != nil {
		log.Warn().Err(compErr).Msg("componentsql: play component registration failed")
	}
	if vocabErr := play.RegisterVocabulary(sqlvocab.Default); vocabErr != nil {
		log.Warn().Err(vocabErr).Msg("sqlvocab: play vocabulary registration failed")
	}
	if godepErr := providersgodep.Register(rt.Introspect, providersgodep.Config{Log: log.Logger}); godepErr != nil {
		log.Warn().Err(godepErr).Msg("introspect: godep table registration failed")
	}
	return
}

// labelBackends tells the capability inspector which backend the runtime
// resolved for each cap, so its schematic highlights the effective impl.
func labelBackends(rt *hostboot.Runtime) {
	capinspector.SetActiveBackend(capinspector.CapRun, "runinfo")
	if rt.IsChStore {
		capinspector.SetActiveBackend(capinspector.CapFacts, "chstore")
	} else {
		capinspector.SetActiveBackend(capinspector.CapFacts, "inmem")
	}
	capinspector.SetActiveBackend(capinspector.CapBus, "inprocbus")
	if rt.Fs != nil {
		capinspector.SetActiveBackend(capinspector.CapFs, "fsbroker")
	}
	if rt.Persist != nil {
		capinspector.SetActiveBackend(capinspector.CapPersist, rt.PersistBackend)
	}
	if rt.Tasks != nil {
		capinspector.SetActiveBackend(capinspector.CapTask, "supervisor")
	} else {
		capinspector.SetActiveBackend(capinspector.CapTask, "task")
	}
}

// parseLaunchConfigs turns each `<alias>=<path>` of --launch-config into a
// seeded window: the alias resolves through the registry, the kind is the
// manifest's LaunchKind, and the file holds the encoded config the kind's
// probe validates at open.
func parseLaunchConfigs(specs []string) (seeds []hostboot.SeedWindow, err error) {
	for _, spec := range specs {
		alias, path, ok := strings.Cut(spec, "=")
		alias = strings.TrimSpace(alias)
		if !ok || alias == "" || strings.TrimSpace(path) == "" {
			err = eb.Build().Str("spec", spec).Errorf("expected <alias>=<path>")
			return
		}
		var m runtimeapp.Manifest
		found := false
		for _, cand := range runtimeapp.AllManifests() {
			if cand.Id.SubjectAlias() == alias {
				m, found = cand, true
				break
			}
		}
		if !found {
			err = eb.Build().Str("alias", alias).Errorf("no registered app has this alias (see --list)")
			return
		}
		if m.LaunchKind == "" {
			err = eb.Build().Str("alias", alias).Errorf("app accepts no launch config")
			return
		}
		raw, readErr := os.ReadFile(strings.TrimSpace(path))
		if readErr != nil {
			err = eb.Build().Str("alias", alias).Str("path", path).Errorf("read launch config: %w", readErr)
			return
		}
		seeds = append(seeds, hostboot.SeedWindow{AppId: m.Id, Kind: m.LaunchKind, Config: raw})
	}
	return
}

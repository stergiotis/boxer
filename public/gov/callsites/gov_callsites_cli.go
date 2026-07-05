package callsites

import (
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

// failOnRules maps --fail-on rule names to their site predicate
// (ADR-0107 §SD7).
var failOnRules = map[string]func(CallSite) bool{
	// A scanned call site passing an interface as a type argument — the
	// unambiguous DON'T from the sources in EXPLANATION.md.
	"interface-type-arg": siteHasInterfaceTypeArg,
}

func siteHasInterfaceTypeArg(site CallSite) bool {
	for _, args := range [2][]TypeArgInfo{site.TypeArgs, site.RecvTypeArgs} {
		for _, arg := range args {
			if arg.Shape == ShapeClassInterface {
				return true
			}
		}
	}
	return false
}

// NewCliCommand is the `gov callsites` CLI (ADR-0107 §SD7).
func NewCliCommand() *cli.Command {
	f, err := cli2.NewUniversalCliFormatter(config.IdentityNameTransf)
	if err != nil {
		log.Panic().Err(err).Msg("unable to create universal cli formatter")
	}
	return &cli.Command{
		Name:      "callsites",
		Usage:     "Survey call-site dispatch (mono / static-poly / dynamic-poly), adjudicated against compiler -m decisions (ADR-0107)",
		ArgsUsage: "[pattern ...]   package patterns to load; defaults to './...'",
		Flags: slices.Concat([]cli.Flag{
			&cli.StringFlag{
				Name:  "tags",
				Usage: "comma-separated build tags (passed to package loading and the adjudication build)",
			},
			&cli.BoolFlag{
				Name:  "adjudicate",
				Value: true,
				Usage: "join compiler devirtualization/inlining decisions from -gcflags=-m (ADR-0107 §SD1)",
			},
			&cli.BoolFlag{
				Name:  "include-tests",
				Usage: "also scan test files (their sites stay compiler-unchecked)",
			},
			&cli.StringSliceFlag{
				Name:  "fail-on",
				Usage: "exit non-zero when a rule matches a site; rules: interface-type-arg",
			},
		},
			f.ToCliFlags(),
		),
		Action: func(cctx *cli.Context) error {
			return callsitesAction(cctx, f)
		},
	}
}

func callsitesAction(cctx *cli.Context, f *cli2.UniversalCliFormatter) (err error) {
	var tags []string
	if t := cctx.String("tags"); t != "" {
		for x := range strings.SplitSeq(t, ",") {
			x = strings.TrimSpace(x)
			if x != "" {
				tags = append(tags, x)
			}
		}
	}
	failPredicates := make(map[string]func(CallSite) bool, 4)
	for _, name := range cctx.StringSlice("fail-on") {
		pred, known := failOnRules[name]
		if !known {
			err = eb.Build().Str("rule", name).Errorf("callsites: unknown --fail-on rule")
			return
		}
		failPredicates[name] = pred
	}

	patterns := cctx.Args().Slice()
	var stats LoadStats
	svc := &AnalyzerService{
		Patterns:     patterns,
		BuildTags:    tags,
		Adjudicate:   cctx.Bool("adjudicate"),
		IncludeTests: cctx.Bool("include-tests"),
		OnLoadStats:  func(s LoadStats) { stats = s },
	}

	var summary Summary
	violations := make(map[string]uint64, len(failPredicates))
	for site, runErr := range svc.All(cctx.Context) {
		if runErr != nil {
			err = eh.Errorf("callsites: %w", runErr)
			return
		}
		summary.Add(site)
		for name, pred := range failPredicates {
			if pred(site) {
				violations[name]++
			}
		}
		err = f.FormatValue(cctx, site)
		if err != nil {
			err = eh.Errorf("callsites: unable to format value: %w", err)
			return
		}
	}

	if stats.IgnoredFiles > 0 {
		log.Warn().
			Int("ignoredFiles", stats.IgnoredFiles).
			Msg("callsites: build constraints excluded Go files from the survey — verify --tags")
	}
	log.Info().
		Int("packages", stats.Packages).
		Int("ignoredFiles", stats.IgnoredFiles).
		Uint64("total", summary.Total).
		Uint64("mono", summary.Mono).
		Uint64("staticPoly", summary.StaticPoly).
		Uint64("dynPoly", summary.DynPoly).
		Uint64("conversions", summary.Conversions).
		Uint64("builtins", summary.Builtins).
		Uint64("unknown", summary.Unknown).
		Uint64("stenciledArgs", summary.StenciledArgs).
		Uint64("pointerArgs", summary.PointerArgs).
		Uint64("interfaceArgs", summary.InterfaceArgs).
		Uint64("typeParamArgs", summary.TypeParamArgs).
		Uint64("checked", summary.Checked).
		Uint64("devirtualized", summary.Devirtualized).
		Uint64("inlinedCalls", summary.InlinedCalls).
		Strs("patterns", patterns).
		Msg("callsites survey complete")

	if len(violations) > 0 {
		b := eb.Build()
		for name, count := range violations {
			b = b.Uint64(name, count)
		}
		err = b.Errorf("callsites: fail-on rule matched")
	}
	return
}

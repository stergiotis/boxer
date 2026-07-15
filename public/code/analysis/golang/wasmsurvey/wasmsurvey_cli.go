package wasmsurvey

import (
	"os"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	cli "github.com/urfave/cli/v2"
)

// NewCliCommand returns the `wasmsurvey` subcommand (registered under
// `golang`, sibling to llmuse/stubber). Bare, it surveys which packages are
// amenable to TinyGo/wasm compilation and prints a per-package, per-target
// verdict with transitive blame (ADR-0078). The `props` subcommand group
// (ADR-0080) seeds/harvests/verifies co-located PackageProps declarations.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "wasmsurvey",
		Usage: "Survey which Go packages are amenable to TinyGo/wasm compilation (ADR-0078)",
		Description: "For each internal package, classify its transitive closure as green/yellow/red\n" +
			"per wasm target (wasi, js, wasm-unknown). `static` mode triages from a curated\n" +
			"TinyGo-support set over the import graph; `empirical` mode confirms survivors by\n" +
			"actually running `tinygo build`; `both` (default) does triage then confirm.\n" +
			"The empirical stage needs `tinygo` on PATH (supporting the repo's Go version);\n" +
			"without it the survey reports the static verdicts and says so. Verdict is\n" +
			"package-level (the exported API compiles), not per-function.",
		Flags: append(computeFlags(),
			&cli.BoolFlag{Name: "show-green", Usage: "list green packages individually instead of summarizing their count"},
			&cli.StringFlag{Name: "json", Usage: "write machine-readable JSON here (\"-\" for stdout, suppresses the text report)"},
		),
		Action:      runWasmSurvey,
		Subcommands: []*cli.Command{newPropsCommand()},
	}
}

// computeFlags are the flags that parameterize a survey Run — shared by the
// bare survey command and the props generate/verify subcommands.
func computeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "dir", Usage: "module dir to survey; empty resolves the nearest go.mod above the working dir"},
		&cli.StringSliceFlag{Name: "patterns", Usage: "go list patterns", Value: cli.NewStringSlice("./...")},
		&cli.StringFlag{Name: "tags", Usage: "comma-separated build tags; empty falls back to <root>/tags then GOFLAGS"},
		&cli.StringFlag{Name: "target", Usage: "comma-separated targets: wasi,js,wasm-unknown", Value: "wasi,js,wasm-unknown"},
		&cli.StringFlag{Name: "mode", Usage: "static | empirical | both", Value: "both"},
		&cli.IntFlag{Name: "jobs", Usage: "empirical probe parallelism (0 = GOMAXPROCS)"},
		&cli.DurationFlag{Name: "timeout", Usage: "per-package tinygo build timeout", Value: 180 * time.Second},
		&cli.BoolFlag{Name: "include-external", Usage: "also verdict external (non-stdlib) packages, not just internal"},
		&cli.StringFlag{Name: "assume-clean", Usage: "counterfactual: comma-separated import-path prefixes treated as wasm-clean (static-only hypothesis)"},
	}
}

// wasmSurveyOptions builds survey Options from the compute flags, resolving the
// module dir to a concrete path (so callers like props harvest have a root).
func wasmSurveyOptions(c *cli.Context) (opts Options, err error) {
	dir := c.String("dir")
	if dir == "" {
		if wd, e := os.Getwd(); e == nil {
			if root, ok := godepcollect.ModuleRoot(wd); ok {
				dir = root
			} else {
				dir = wd
			}
		}
	}

	var targets []TargetID
	for name := range strings.SplitSeq(c.String("target"), ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		t, ok := ParseTargetE(name)
		if !ok {
			return Options{}, eb.Build().Str("target", name).Errorf("unknown target (want wasi|js|wasm-unknown)")
		}
		targets = append(targets, t)
	}

	mode, ok := ParseModeE(c.String("mode"))
	if !ok {
		return Options{}, eb.Build().Str("mode", c.String("mode")).Errorf("unknown mode (want static|empirical|both)")
	}

	opts = Options{
		Dir:             dir,
		Patterns:        c.StringSlice("patterns"),
		Tags:            resolveTags(c.String("tags"), dir),
		Targets:         targets,
		Mode:            mode,
		IncludeExternal: c.Bool("include-external"),
		Jobs:            c.Int("jobs"),
		ProbeTimeout:    c.Duration("timeout"),
		AssumeClean:     godepcollect.SplitTags(c.String("assume-clean")),
	}
	return opts, nil
}

func runWasmSurvey(c *cli.Context) (err error) {
	var opts Options
	opts, err = wasmSurveyOptions(c)
	if err != nil {
		return err
	}

	var survey Survey
	survey, err = Run(c.Context, opts)
	if err != nil {
		return err
	}

	if jsonPath := c.String("json"); jsonPath != "" {
		if jsonPath == "-" {
			return RenderJSON(survey, os.Stdout)
		}
		f, e := os.Create(jsonPath)
		if e != nil {
			return eb.Build().Str("path", jsonPath).Errorf("create json output: %w", e)
		}
		defer func() { _ = f.Close() }()
		if e = RenderJSON(survey, f); e != nil {
			return e
		}
	}

	return RenderText(survey, os.Stdout, c.Bool("show-green"))
}

// resolveTags resolves the build-tag list for root. The resolution itself lives
// in godepcollect, beside ModuleRoot, so that every package-loading tool in the
// tree agrees on which files it is looking at.
func resolveTags(flagVal string, root string) (tags []string) {
	return godepcollect.ResolveTags(flagVal, root)
}

//go:build llm_generated_opus47

package llmtag

import (
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/gov/repo"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

// NewCliCommand returns the `gov llmtag` subcommand that annotates Go
// source files with //go:build llm_generated_<model> based on git blame
// attribution of Co-Authored-By trailers.
func NewCliCommand() *cli.Command {
	f, err := cli2.NewUniversalCliFormatter(config.IdentityNameTransf)
	if err != nil {
		log.Panic().Err(err).Msg("unable to create universal cli formatter")
	}
	fmtFlags := f.ToCliFlags()

	baseFlags := []cli.Flag{
		&cli.StringFlag{
			Name:  "repo",
			Value: ".",
			Usage: "Path to git repository",
		},
		&cli.StringFlag{
			Name:  "root",
			Value: ".",
			Usage: "Root directory to scan for .go files",
		},
		&cli.Float64Flag{
			Name:  "threshold",
			Value: 0.5,
			Usage: "Minimum fraction of LLM-attributed lines required to classify a file as LLM-generated",
		},
		&cli.IntFlag{
			Name:  "min-lines",
			Value: 0,
			Usage: "Absolute minimum LLM-attributed lines required (combined with --threshold via AND)",
		},
		&cli.StringFlag{
			Name:  "trailer-cutoff",
			Value: "",
			Usage: "RFC3339 date before which trailerless commits on a tagged file are attributed to its existing tag (overrides auto-detect from earliest LLM-trailered commit)",
		},
		&cli.BoolFlag{
			Name:  "apply",
			Usage: "Write tag changes (add, update, or remove) into matching files (default: dry run)",
		},
		&cli.BoolFlag{
			Name:  "diff",
			Usage: "Lint mode: emit one terse line per file whose tag is missing, stale, conflicting, or no longer warranted; exits non-zero if any were found",
		},
	}

	return &cli.Command{
		Name:  "llmtag",
		Usage: "Apply //go:build llm_generated_<model> build tags based on git Co-Authored-By trailers",
		Flags: slices.Concat(baseFlags, fmtFlags),
		Action: func(c *cli.Context) (err error) {
			if c.Bool("diff") && c.Bool("apply") {
				err = eh.Errorf("llmtag: --diff and --apply are mutually exclusive")
				return
			}
			applier := &Applier{
				ApplyOp:     ApplyOpDryRun,
				Threshold:   c.Float64("threshold"),
				MinLLMLines: int32(c.Int("min-lines")),
			}
			if c.Bool("apply") {
				applier.ApplyOp = ApplyOpApply
			}
			git := &repo.GitRunner{RepoPath: c.String("repo")}

			if cutoffStr := c.String("trailer-cutoff"); cutoffStr != "" {
				var t time.Time
				t, err = time.Parse(time.RFC3339, cutoffStr)
				if err != nil {
					err = eb.Build().Str("value", cutoffStr).Errorf("unable to parse --trailer-cutoff as RFC3339: %w", err)
					return
				}
				applier.TrailerCutoff = t
			} else {
				var t time.Time
				t, err = applier.AutoDetectCutoff(c.Context, git)
				if err != nil {
					err = eh.Errorf("trailer-cutoff auto-detect failed: %w", err)
					return
				}
				applier.TrailerCutoff = t
			}
			if applier.TrailerCutoff.IsZero() {
				log.Info().Msg("llmtag: no trailer cutoff (no LLM-trailered commits found and none specified)")
			} else {
				log.Info().Time("trailerCutoff", applier.TrailerCutoff).Msg("llmtag: pre-cutoff trailerless commits will be attributed to existing simple tag")
			}

			if c.Bool("diff") {
				err = runDiff(c, applier, git)
				return
			}

			for rec, iterErr := range applier.Run(c.Context, git, c.String("root")) {
				if iterErr != nil {
					err = eh.Errorf("llmtag run failed: %w", iterErr)
					return
				}
				err = f.FormatValue(c, rec)
				if err != nil {
					err = eh.Errorf("unable to format record: %w", err)
					return
				}
			}
			return
		},
	}
}

// runDiff prints one terse line per file that needs a build-tag change and
// returns a non-nil error (so the CLI exits non-zero) when any are found.
// Silently ignores skips that are not actionable (already tagged, below
// threshold, uncommitted, complex llm directives).
func runDiff(c *cli.Context, applier *Applier, git *repo.GitRunner) (err error) {
	var missing uint64
	var stale uint64
	var orphan uint64
	var conflict uint64
	for rec, iterErr := range applier.Run(c.Context, git, c.String("root")) {
		if iterErr != nil {
			err = eh.Errorf("llmtag run failed: %w", iterErr)
			return
		}
		switch rec.Decision {
		case DecisionWouldApply:
			missing++
			fmt.Fprintf(os.Stdout, "%s: missing //go:build llm_generated_%s (%d LLM / %d non-LLM lines)\n",
				rec.Path, rec.DominantTag, rec.LLMLines, rec.NonLLMLines)
		case DecisionWouldUpdate:
			stale++
			fmt.Fprintf(os.Stdout, "%s: stale tag llm_generated_%s, dominant is now llm_generated_%s (%d LLM / %d non-LLM lines)\n",
				rec.Path, rec.ExistingLLMTag, rec.DominantTag, rec.LLMLines, rec.NonLLMLines)
		case DecisionWouldRemove:
			orphan++
			fmt.Fprintf(os.Stdout, "%s: stale tag llm_generated_%s, file no longer above threshold (%d LLM / %d non-LLM lines)\n",
				rec.Path, rec.ExistingLLMTag, rec.LLMLines, rec.NonLLMLines)
		case DecisionSkipHasBuildTag:
			conflict++
			fmt.Fprintf(os.Stdout, "%s: existing build directive %q blocks llm_generated_%s (%d LLM / %d non-LLM lines)\n",
				rec.Path, rec.ExistingBuildDirective, rec.DominantTag, rec.LLMLines, rec.NonLLMLines)
		}
	}
	if missing+stale+orphan+conflict > 0 {
		err = eb.Build().
			Uint64("missing", missing).
			Uint64("stale", stale).
			Uint64("orphan", orphan).
			Uint64("conflict", conflict).
			Errorf("llmtag: build-tag findings present")
	}
	return
}

//go:build llm_generated_opus47

package llmtag

import (
	"fmt"
	"os"
	"slices"

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
			Usage: "Minimum fraction of LLM-attributed lines required to apply a tag",
		},
		&cli.BoolFlag{
			Name:  "apply",
			Usage: "Write the build tag into matching files (default: dry run)",
		},
		&cli.BoolFlag{
			Name:  "diff",
			Usage: "Lint mode: emit one terse line per missing or conflicting tag and exit non-zero if any were found",
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
				ApplyOp:   ApplyOpDryRun,
				Threshold: c.Float64("threshold"),
			}
			if c.Bool("apply") {
				applier.ApplyOp = ApplyOpApply
			}
			git := &repo.GitRunner{RepoPath: c.String("repo")}

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
// threshold, uncommitted).
func runDiff(c *cli.Context, applier *Applier, git *repo.GitRunner) (err error) {
	var missing uint64
	var conflict uint64
	for rec, iterErr := range applier.Run(c.Context, git, c.String("root")) {
		if iterErr != nil {
			err = eh.Errorf("llmtag run failed: %w", iterErr)
			return
		}
		switch rec.Decision {
		case DecisionWouldApply:
			missing++
			fmt.Fprintf(os.Stdout, "%s: missing //go:build llm_generated_%s (%d/%d lines)\n",
				rec.Path, rec.DominantTag, rec.LLMLines, rec.TotalLines)
		case DecisionSkipHasBuildTag:
			conflict++
			fmt.Fprintf(os.Stdout, "%s: existing build directive %q blocks llm_generated_%s (%d/%d lines)\n",
				rec.Path, rec.ExistingBuildDirective, rec.DominantTag, rec.LLMLines, rec.TotalLines)
		}
	}
	if missing+conflict > 0 {
		err = eb.Build().
			Uint64("missing", missing).
			Uint64("conflict", conflict).
			Errorf("llmtag: build-tag findings present")
	}
	return
}

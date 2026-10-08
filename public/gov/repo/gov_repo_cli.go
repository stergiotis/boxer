package repo

import (
	"context"
	"os"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v3"
)

func sharedFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "repo",
			Value: ".",
			Usage: "Path to git repository",
		},
		&cli.StringFlag{
			Name:  "since",
			Value: "12 months ago",
			Usage: "Start of time range (git date string)",
		},
		&cli.StringFlag{
			Name:  "until",
			Value: "",
			Usage: "End of time range (git date string, empty = now)",
		},
		&cli.IntFlag{
			Name:  "top",
			Value: 20,
			Usage: "Limit output to top N entries",
		},
	}
}

func gitFromContext(ctx context.Context, cmd *cli.Command) (git GitRunner) {
	git = GitRunner{RepoPath: cmd.String("repo")}
	return
}

func NewCliCommand() *cli.Command {
	f, err := cli2.NewUniversalCliFormatter(config.IdentityNameTransf)
	if err != nil {
		log.Panic().Err(err).Msg("unable to create universal cli formatter")
	}
	fmtFlags := f.ToCliFlags()

	return &cli.Command{
		Name:  "repo",
		Usage: "Git repository health diagnostics",
		Commands: []*cli.Command{
			{
				Name:  "report",
				Usage: "Generate a Unicode health report for embedding in project overviews",
				Flags: sharedFlags(),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					git := gitFromContext(ctx, cmd)
					rpt := &ReportGenerator{
						Since: cmd.String("since"),
						Until: cmd.String("until"),
						TopN:  cmd.Int("top"),
					}
					err = rpt.Generate(ctx, &git, os.Stdout)
					if err != nil {
						return eh.Errorf("report generation failed: %w", err)
					}
					return nil
				},
			},
			{
				Name:  "churn",
				Usage: "Show most frequently changed files",
				Flags: slices.Concat(sharedFlags(), fmtFlags),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					git := gitFromContext(ctx, cmd)
					analyzer := &ChurnAnalyzer{
						TopN:  cmd.Int("top"),
						Since: cmd.String("since"),
						Until: cmd.String("until"),
					}
					for rec, iterErr := range analyzer.Run(ctx, &git) {
						if iterErr != nil {
							return eh.Errorf("churn analysis failed: %w", iterErr)
						}
						err = f.FormatValue(ctx, cmd, rec)
						if err != nil {
							return eh.Errorf("unable to format value: %w", err)
						}
					}
					return nil
				},
			},
			{
				Name:  "velocity",
				Usage: "Show commit frequency by month",
				Flags: slices.Concat(sharedFlags(), fmtFlags),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					git := gitFromContext(ctx, cmd)
					analyzer := &VelocityAnalyzer{
						Since: cmd.String("since"),
						Until: cmd.String("until"),
					}
					for rec, iterErr := range analyzer.Run(ctx, &git) {
						if iterErr != nil {
							return eh.Errorf("velocity analysis failed: %w", iterErr)
						}
						err = f.FormatValue(ctx, cmd, rec)
						if err != nil {
							return eh.Errorf("unable to format value: %w", err)
						}
					}
					return nil
				},
			},
			{
				Name:  "bughotspots",
				Usage: "Show files most associated with bug-fix commits",
				Flags: slices.Concat(sharedFlags(), fmtFlags, []cli.Flag{
					&cli.StringFlag{
						Name:  "pattern",
						Value: "",
						Usage: "Regex pattern for bug-related commit messages (default: ^(fix|hotfix):",
					},
				}),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					git := gitFromContext(ctx, cmd)
					analyzer := &BugHotspotAnalyzer{
						Since:   cmd.String("since"),
						Until:   cmd.String("until"),
						TopN:    cmd.Int("top"),
						Pattern: cmd.String("pattern"),
					}
					for rec, iterErr := range analyzer.Run(ctx, &git) {
						if iterErr != nil {
							return eh.Errorf("bug hotspot analysis failed: %w", iterErr)
						}
						err = f.FormatValue(ctx, cmd, rec)
						if err != nil {
							return eh.Errorf("unable to format value: %w", err)
						}
					}
					return nil
				},
			},
			{
				Name:  "contributors",
				Usage: "Show contributor ranking and bus factor",
				Flags: slices.Concat(sharedFlags(), fmtFlags, []cli.Flag{
					&cli.BoolFlag{
						Name:  "bus-factor",
						Usage: "Show bus factor summary instead of individual contributors",
					},
				}),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					git := gitFromContext(ctx, cmd)
					mailmap, mmErr := LoadMailmap(ctx, &git)
					if mmErr != nil {
						return eh.Errorf("unable to load mailmap: %w", mmErr)
					}
					analyzer := &ContributorAnalyzer{
						Since:   cmd.String("since"),
						Until:   cmd.String("until"),
						TopN:    cmd.Int("top"),
						Mailmap: mailmap,
					}
					if cmd.Bool("bus-factor") {
						var result BusFactorResult
						result, err = analyzer.RunSummary(ctx, &git)
						if err != nil {
							return eh.Errorf("contributor analysis failed: %w", err)
						}
						err = f.FormatValue(ctx, cmd, result)
						if err != nil {
							return eh.Errorf("unable to format value: %w", err)
						}
						return nil
					}
					for rec, iterErr := range analyzer.Run(ctx, &git) {
						if iterErr != nil {
							return eh.Errorf("contributor analysis failed: %w", iterErr)
						}
						err = f.FormatValue(ctx, cmd, rec)
						if err != nil {
							return eh.Errorf("unable to format value: %w", err)
						}
					}
					return nil
				},
			},
			{
				Name:  "authorship",
				Usage: "Show human vs LLM-generated code over time",
				Flags: slices.Concat(sharedFlags(), fmtFlags),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					git := gitFromContext(ctx, cmd)
					analyzer := &AuthorshipAnalyzer{}
					for rec, iterErr := range analyzer.Run(ctx, &git) {
						if iterErr != nil {
							return eh.Errorf("authorship analysis failed: %w", iterErr)
						}
						err = f.FormatValue(ctx, cmd, rec)
						if err != nil {
							return eh.Errorf("unable to format value: %w", err)
						}
					}
					return nil
				},
			},
			{
				Name:  "ownership",
				Usage: "Show surviving-line ownership per file (git blame × Co-Authored-By provenance)",
				Flags: slices.Concat([]cli.Flag{
					&cli.StringFlag{
						Name:  "repo",
						Value: ".",
						Usage: "Path to git repository",
					},
					&cli.IntFlag{
						Name:  "parallelism",
						Value: 0,
						Usage: "Concurrent git blame processes (0 = auto)",
					},
					&cli.BoolFlag{
						Name:  "summary",
						Usage: "Show aggregated owner totals and model sponsorship instead of per-file records",
					},
					&cli.BoolFlag{
						Name:  "commits",
						Usage: "Show the provenance-classified commit log instead of per-file records",
					},
				}, fmtFlags),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					git := gitFromContext(ctx, cmd)
					mailmap, mmErr := LoadMailmap(ctx, &git)
					if mmErr != nil {
						return eh.Errorf("unable to load mailmap: %w", mmErr)
					}
					analyzer := &OwnershipAnalyzer{
						Parallelism: cmd.Int("parallelism"),
						Mailmap:     mailmap,
					}
					if cmd.Bool("commits") {
						for rec, iterErr := range analyzer.RunCommits(ctx, &git) {
							if iterErr != nil {
								return eh.Errorf("commit scan failed: %w", iterErr)
							}
							err = f.FormatValue(ctx, cmd, rec)
							if err != nil {
								return eh.Errorf("unable to format value: %w", err)
							}
						}
						return nil
					}
					if cmd.Bool("summary") {
						summary, sumErr := analyzer.RunSummary(ctx, &git)
						if sumErr != nil {
							return eh.Errorf("ownership analysis failed: %w", sumErr)
						}
						for _, owner := range summary.Owners {
							err = f.FormatValue(ctx, cmd, owner)
							if err != nil {
								return eh.Errorf("unable to format value: %w", err)
							}
						}
						for _, sponsor := range summary.Sponsors {
							err = f.FormatValue(ctx, cmd, sponsor)
							if err != nil {
								return eh.Errorf("unable to format value: %w", err)
							}
						}
						return nil
					}
					for rec, iterErr := range analyzer.Run(ctx, &git) {
						if iterErr != nil {
							return eh.Errorf("ownership analysis failed: %w", iterErr)
						}
						err = f.FormatValue(ctx, cmd, rec)
						if err != nil {
							return eh.Errorf("unable to format value: %w", err)
						}
					}
					return nil
				},
			},
			{
				Name:  "firefighting",
				Usage: "Show reverts, hotfixes, and emergency commits",
				Flags: slices.Concat(sharedFlags(), fmtFlags, []cli.Flag{
					&cli.StringFlag{
						Name:  "revert-pattern",
						Value: "",
						Usage: "Regex for revert commits",
					},
					&cli.StringFlag{
						Name:  "hotfix-pattern",
						Value: "",
						Usage: "Regex for hotfix commits",
					},
					&cli.StringFlag{
						Name:  "emergency-pattern",
						Value: "",
						Usage: "Regex for emergency commits",
					},
				}),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					git := gitFromContext(ctx, cmd)
					analyzer := &FirefightAnalyzer{
						Since:            cmd.String("since"),
						Until:            cmd.String("until"),
						RevertPattern:    cmd.String("revert-pattern"),
						HotfixPattern:    cmd.String("hotfix-pattern"),
						EmergencyPattern: cmd.String("emergency-pattern"),
					}
					for rec, iterErr := range analyzer.Run(ctx, &git) {
						if iterErr != nil {
							return eh.Errorf("firefighting analysis failed: %w", iterErr)
						}
						err = f.FormatValue(ctx, cmd, rec)
						if err != nil {
							return eh.Errorf("unable to format value: %w", err)
						}
					}
					return nil
				},
			},
		},
	}
}

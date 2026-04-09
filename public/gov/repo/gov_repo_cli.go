//go:build llm_generated_opus46

package repo

import (
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
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

func gitFromContext(c *cli.Context) (git GitRunner) {
	git = GitRunner{RepoPath: c.String("repo")}
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
		Subcommands: []*cli.Command{
			{
				Name:  "churn",
				Usage: "Show most frequently changed files",
				Flags: slices.Concat(sharedFlags(), fmtFlags),
				Action: func(c *cli.Context) error {
					git := gitFromContext(c)
					analyzer := &ChurnAnalyzer{
						TopN:  c.Int("top"),
						Since: c.String("since"),
						Until: c.String("until"),
					}
					for rec, iterErr := range analyzer.Run(c.Context, &git) {
						if iterErr != nil {
							return eh.Errorf("churn analysis failed: %w", iterErr)
						}
						err = f.FormatValue(c, rec)
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
				Action: func(c *cli.Context) error {
					git := gitFromContext(c)
					analyzer := &VelocityAnalyzer{
						Since: c.String("since"),
						Until: c.String("until"),
					}
					for rec, iterErr := range analyzer.Run(c.Context, &git) {
						if iterErr != nil {
							return eh.Errorf("velocity analysis failed: %w", iterErr)
						}
						err = f.FormatValue(c, rec)
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
				Action: func(c *cli.Context) error {
					git := gitFromContext(c)
					analyzer := &BugHotspotAnalyzer{
						Since:   c.String("since"),
						Until:   c.String("until"),
						TopN:    c.Int("top"),
						Pattern: c.String("pattern"),
					}
					for rec, iterErr := range analyzer.Run(c.Context, &git) {
						if iterErr != nil {
							return eh.Errorf("bug hotspot analysis failed: %w", iterErr)
						}
						err = f.FormatValue(c, rec)
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
				Action: func(c *cli.Context) error {
					git := gitFromContext(c)
					analyzer := &ContributorAnalyzer{
						Since: c.String("since"),
						Until: c.String("until"),
						TopN:  c.Int("top"),
					}
					if c.Bool("bus-factor") {
						var result BusFactorResult
						result, err = analyzer.RunSummary(c.Context, &git)
						if err != nil {
							return eh.Errorf("contributor analysis failed: %w", err)
						}
						err = f.FormatValue(c, result)
						if err != nil {
							return eh.Errorf("unable to format value: %w", err)
						}
						return nil
					}
					for rec, iterErr := range analyzer.Run(c.Context, &git) {
						if iterErr != nil {
							return eh.Errorf("contributor analysis failed: %w", iterErr)
						}
						err = f.FormatValue(c, rec)
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
				Action: func(c *cli.Context) error {
					git := gitFromContext(c)
					analyzer := &FirefightAnalyzer{
						Since:            c.String("since"),
						Until:            c.String("until"),
						RevertPattern:    c.String("revert-pattern"),
						HotfixPattern:    c.String("hotfix-pattern"),
						EmergencyPattern: c.String("emergency-pattern"),
					}
					for rec, iterErr := range analyzer.Run(c.Context, &git) {
						if iterErr != nil {
							return eh.Errorf("firefighting analysis failed: %w", iterErr)
						}
						err = f.FormatValue(c, rec)
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

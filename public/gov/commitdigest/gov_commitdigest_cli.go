//go:build llm_generated_opus46

package commitdigest

import (
	"errors"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:      "commitdigest",
		Usage:     "Prepare recent commits from multiple repos for LLM summarization",
		ArgsUsage: "REPO [REPO...]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "since",
				Aliases: []string{"s"},
				Value:   "1 day ago",
				Usage:   "Show commits since this time (git date string, e.g. '4h ago', '1 day ago', '2025-01-01')",
			},
			&cli.StringFlag{
				Name:  "author",
				Usage: "Filter commits by author (substring match)",
			},
			&cli.BoolFlag{
				Name:  "no-stat",
				Usage: "Omit changed-file statistics from output",
			},
		},
		Action: func(c *cli.Context) error {
			repos := c.Args().Slice()
			if len(repos) == 0 {
				repos = []string{"."}
			}
			since := c.String("since")
			author := c.String("author")
			noStat := c.Bool("no-stat")

			digests := make([]RepoDigest, 0, len(repos))
			for _, repo := range repos {
				d, err := CollectDigest(c.Context, repo, since, author, noStat)
				if err != nil {
					if errors.Is(err, ErrNotAGitRepo) {
						log.Warn().Str("path", repo).Msg("skipping directory: not a git repository")
						continue
					}
					if errors.Is(err, ErrNoCommits) {
						log.Warn().Str("path", repo).Msg("skipping repository: no commits")
						continue
					}
					return eh.Errorf("failed to collect digest for %q: %w", repo, err)
				}
				digests = append(digests, d)
			}

			err := WriteDigest(os.Stdout, digests)
			if err != nil {
				return eh.Errorf("failed to write digest: %w", err)
			}
			return nil
		},
	}
}

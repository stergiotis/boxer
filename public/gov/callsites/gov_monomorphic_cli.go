//go:build llm_generated_gemini3pro

package callsites

import (
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	f, err := cli2.NewUniversalCliFormatter(config.IdentityNameTransf)
	if err != nil {
		log.Panic().Err(err).Msg("unable to create universal cli formatter")
	}
	return &cli.Command{
		Name: "callsites",
		Flags: slices.Concat([]cli.Flag{
			&cli.StringFlag{
				Name:  "pattern",
				Value: "./...",
			},
		},
			f.ToCliFlags(),
		),
		Action: func(context *cli.Context) error {
			analyzer := &AnalyzerService{
				Pattern:    "",
				BuildTags:  nil,
				BuildFlags: nil,
			}
			var cs CallSite
			for cs, err = range analyzer.Run(context.Context) {
				if err != nil {
					err = eh.Errorf("error while analyzing code: %w", err)
					return err
				}
				err = f.FormatValue(context, cs)
				if err != nil {
					err = eh.Errorf("unable to format value: %w", err)
					return err
				}
			}
			return nil
		},
	}
}

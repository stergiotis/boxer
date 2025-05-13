package cli

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/et7/aspects"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/urfave/cli/v2"
)

func NewCommand() *cli.Command {
	f, err := cli2.NewUniversalCliFormatter(config.IdentityNameTransf)
	if err != nil {
		log.Panic().Err(err).Msg("unable to create cli universal formatter")
	}
	return &cli.Command{
		Name: "et7",
		Subcommands: []*cli.Command{
			{
				Name: "aspects",
				Subcommands: []*cli.Command{
					{
						Name:  "list",
						Flags: f.ToCliFlags(),
						Action: func(context *cli.Context) error {
							strs := make([]string, 0, len(aspects.AllDataAspects))
							for _, a := range aspects.AllDataAspects {
								strs = append(strs, a.String())
							}
							return f.FormatValue(context, strs)
						},
					},
				},
			},
		},
	}
}

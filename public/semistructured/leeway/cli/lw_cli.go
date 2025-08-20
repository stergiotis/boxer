package cli

import (
	"os"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes/codegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	f, err := cli2.NewUniversalCliFormatter(config.IdentityNameTransf)
	if err != nil {
		log.Panic().Err(err).Msg("unable to create cli universal formatter")
	}
	return &cli.Command{
		Name: "leeway",
		Subcommands: []*cli.Command{
			{
				Name: "ct",
				Subcommands: []*cli.Command{
					{
						Name: "abbrevs",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:  "packageName",
								Value: "canonicalTypes",
							},
							&cli.StringFlag{
								Name:  "import",
								Value: "",
							},
							&cli.StringFlag{
								Name:  "astPackage",
								Value: "",
							},
						},
						Action: func(context *cli.Context) error {
							return codegen.GenerateGoAbbrev(context.String("packageName"),
								context.String("import"),
								context.String("astPackage"),
								os.Stdout, nil)
						},
					},
				},
			},
			{
				Name: "useaspects",
				Subcommands: []*cli.Command{
					{
						Name:  "list",
						Flags: f.ToCliFlags(),
						Action: func(context *cli.Context) error {
							strs := make([]string, 0, len(useaspects.AllAspects))
							for _, a := range useaspects.AllAspects {
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

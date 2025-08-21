package cli

import (
	"os"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes/codegen"
	"github.com/urfave/cli/v2"
)

func NewCliCommandCanonicalTypes() *cli.Command {
	return &cli.Command{
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
	}
}

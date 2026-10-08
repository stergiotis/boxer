package cli

import (
	"context"
	"os"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/codegen"
	"github.com/urfave/cli/v3"
)

func NewCliCommandCanonicalTypes() *cli.Command {
	return &cli.Command{
		Name: "ct",
		Commands: []*cli.Command{
			{
				Name: "abbrevs",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "packageName",
						Value: "canonicaltypes",
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
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return codegen.GenerateGoAbbrev(cmd.String("packageName"),
						cmd.String("import"),
						cmd.String("astPackage"),
						os.Stdout, nil)
				},
			},
		},
	}
}

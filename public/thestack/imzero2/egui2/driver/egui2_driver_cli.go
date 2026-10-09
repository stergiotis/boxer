package driver

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v3"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "generate",
		Commands: []*cli.Command{
			{
				Name: "go",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "goOutputBasePath",
						Required: true,
						Usage:    "go code output directory",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					goOutputBasePath := cmd.String("goOutputBasePath")
					goOutputBasePathAbs, err := filepath.Abs(goOutputBasePath)
					if err != nil {
						return eh.Errorf("unable to resolve absolute path of supplied goOutputBasePath")
					}
					err = os.MkdirAll(goOutputBasePathAbs, os.ModePerm)
					if err != nil && !errors.Is(err, os.ErrExist) {
						return eh.Errorf("unable to create goOutputBasePath")
					}
					packageName := filepath.Base(goOutputBasePathAbs) //s[len(s)-1]
					return GenerateGoFiles(packageName, goOutputBasePathAbs)
				},
			},
			{
				Name: "rust",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "rustOutputBasePath",
						Required: true,
						Usage:    "rust code output directory",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					rustOutputBasePath := cmd.String("rustOutputBasePath")
					return GenerateRustFiles(rustOutputBasePath)
				},
			},
			{
				Name: "doc",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "docOutputPath",
						Required: true,
						Usage:    "path to the output markdown file",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					docOutputPath := cmd.String("docOutputPath")
					docOutputPathAbs, err := filepath.Abs(docOutputPath)
					if err != nil {
						return eh.Errorf("unable to resolve absolute path of supplied docOutputPath")
					}
					dir := filepath.Dir(docOutputPathAbs)
					err = os.MkdirAll(dir, os.ModePerm)
					if err != nil && !errors.Is(err, os.ErrExist) {
						return eh.Errorf("unable to create output directory")
					}
					return GenerateDocFile(docOutputPathAbs)
				},
			},
		},
	}
}

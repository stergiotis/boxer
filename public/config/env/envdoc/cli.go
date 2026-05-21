//go:build llm_generated_opus47

package envdoc

import (
	"os"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	cli "github.com/urfave/cli/v2"
)

// NewGenDocsCommand returns the `gen-docs` subcommand. It is mounted
// under boxer's `env` parent by public/app/main.go (env.NewCliCommand
// accepts cross-package subcommands precisely so this can live here
// without forming an env → envdoc → env import cycle).
func NewGenDocsCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "gen-docs",
		Usage: "render the env-var registry to a Diátaxis reference markdown file",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "out",
				Usage:    "output markdown file ('-' for stdout)",
				Required: true,
			},
		},
		Action: runGenDocs,
	}
	return
}

func runGenDocs(ctx *cli.Context) (err error) {
	outPath := ctx.String("out")
	body := Render(Options{
		GeneratorPath:  "public/app env gen-docs",
		RegenerateHint: "go generate ./public/config/env/...",
	})
	if outPath == "-" {
		_, err = os.Stdout.WriteString(body)
		if err != nil {
			err = eb.Build().Errorf("envgen: write stdout: %w", err)
		}
		return
	}
	err = os.WriteFile(outPath, []byte(body), 0o644)
	if err != nil {
		err = eb.Build().Str("outPath", outPath).Errorf("envgen: write %s: %w", outPath, err)
	}
	return
}

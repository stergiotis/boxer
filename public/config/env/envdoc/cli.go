package envdoc

import (
	"context"
	"os"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	cli "github.com/urfave/cli/v3"
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
			&cli.StringFlag{
				Name:  "json",
				Usage: "also write the machine-readable snapshot (RenderJSON) to this file ('-' for stdout)",
			},
		},
		Action: runGenDocs,
	}
	return
}

func runGenDocs(ctx context.Context, cmd *cli.Command) (err error) {
	outPath := cmd.String("out")
	body := Render(Options{
		GeneratorPath:  "public/app env gen-docs",
		RegenerateHint: "go generate ./public/config/env/...",
	})
	err = writeOutput(outPath, []byte(body))
	if err != nil {
		return
	}
	jsonPath := cmd.String("json")
	if jsonPath == "" {
		return
	}
	var snapshot []byte
	snapshot, err = RenderJSON()
	if err != nil {
		return
	}
	err = writeOutput(jsonPath, snapshot)
	return
}

func writeOutput(path string, body []byte) (err error) {
	if path == "-" {
		_, err = os.Stdout.Write(body)
		if err != nil {
			err = eb.Build().Errorf("write stdout: %w", err)
		}
		return
	}
	err = os.WriteFile(path, body, 0o644)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("write: %w", err)
	}
	return
}

package envdoc

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"github.com/stergiotis/boxer/public/config/env"
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
			&cli.StringFlag{
				Name:  "known",
				Usage: "also write the env package's generated name list (RenderKnownNames) to this file",
			},
			&cli.StringFlag{
				Name:  "generator",
				Usage: "how the markdown names the command that rendered it; a downstream host names its own",
				Value: "public/app env gen-docs",
			},
			&cli.StringFlag{
				Name:  "regenerate-hint",
				Usage: "the command the markdown tells a reader to re-run after a spec changes",
				Value: "go generate ./public/config/env/...",
			},
			&cli.StringFlag{
				Name:  "envfile-command",
				Usage: "the subcommand the markdown names for a commented environment file; empty omits it, for a host that does not mount gen-envfile",
				Value: "env gen-envfile",
			},
		},
		Action: runGenDocs,
	}
	return
}

func runGenDocs(ctx context.Context, cmd *cli.Command) (err error) {
	outPath := cmd.String("out")
	body := Render(Options{
		GeneratorPath:  cmd.String("generator"),
		RegenerateHint: cmd.String("regenerate-hint"),
		EnvFileCommand: cmd.String("envfile-command"),
	})
	err = writeOutput(outPath, []byte(body))
	if err != nil {
		return
	}
	if knownPath := cmd.String("known"); knownPath != "" {
		err = writeOutput(knownPath, RenderKnownNames())
		if err != nil {
			return
		}
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

// NewGenEnvFileCommand returns the `gen-envfile` subcommand, mounted
// beside `gen-docs` under boxer's `env` parent. It renders the linked
// registry as a commented environment file (RenderEnvFile).
func NewGenEnvFileCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "gen-envfile",
		Usage: "render the env-var registry as a commented environment file",
		Description: "Writes one assignment per registered variable, commented out and " +
			"carrying its default, under comments giving its description, type, " +
			"allowed values, CLI flag and declaring package. Uncomment a line to set " +
			"the variable. With --current, a variable set in this process's " +
			"environment is written as an active assignment of its value; a " +
			"sensitive variable's value is never written. Values are quoted for a " +
			"POSIX shell. Only the variables this binary links appear.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "out",
				Usage: "output file ('-' for stdout); an existing file is kept unless --force",
				Value: "-",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "overwrite an existing --out file",
			},
			&cli.BoolFlag{
				Name:  "export",
				Usage: "prefix every assignment with 'export ' so the file can be sourced without set -a",
			},
			&cli.BoolFlag{
				Name:  "current",
				Usage: "write the variables set in the environment as active assignments of their values",
			},
			&cli.StringFlag{
				Name:  "category",
				Usage: "include only specs whose Category equals this value",
			},
			&cli.StringFlag{
				Name:  "origin",
				Usage: "include only specs whose Origin.Module equals this module path",
			},
			&cli.StringFlag{
				Name:  "prefix",
				Usage: "include only specs whose Name starts with this prefix",
			},
			&cli.StringFlag{
				Name:  "generator",
				Usage: "the command the file's header names as the one that rendered it; empty names none",
			},
		},
		Action: runGenEnvFile,
	}
	return
}

func runGenEnvFile(ctx context.Context, cmd *cli.Command) (err error) {
	specs := env.SpecFilter{
		Category: env.CategoryE(cmd.String("category")),
		Origin:   cmd.String("origin"),
		Prefix:   cmd.String("prefix"),
	}.Apply(env.Snapshot())
	opts := EnvFileOptions{
		GeneratorPath: cmd.String("generator"),
		Export:        cmd.Bool("export"),
	}
	if cmd.Bool("current") {
		opts.Lookup = os.LookupEnv
	}
	body := []byte(RenderEnvFile(specs, opts))
	outPath := cmd.String("out")
	if outPath == "-" || cmd.Bool("force") {
		err = writeOutput(outPath, body)
		return
	}
	var f *os.File
	f, err = os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			err = eb.Build().Str("path", outPath).Errorf("output file exists; pass --force to overwrite it: %w", err)
			return
		}
		err = eb.Build().Str("path", outPath).Errorf("create: %w", err)
		return
	}
	_, err = f.Write(body)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		err = eb.Build().Str("path", outPath).Errorf("write: %w", err)
	}
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

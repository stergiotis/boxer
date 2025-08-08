package dev

import (
	"os"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "dev",
		Subcommands: []*cli.Command{
			{
				Name: "panic",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "message",
						Value: "default panic message",
					},
				},
				Action: func(context *cli.Context) error {
					log.Panic().Str("str", "strval").Uint64("uint64", 0xdeadbeef).Msg(context.String("message"))
					return nil
				},
			},
		},
	}
}

var IoOverrideFlags = []cli.Flag{
	&cli.StringFlag{
		Category: "override",
		Name:     "stdinFromFile",
		Value:    "",
		Action: func(context *cli.Context, s string) error {
			if s != "" {
				f, err := os.OpenFile(s, os.O_RDONLY, os.ModePerm)
				if err != nil {
					return eb.Build().Str("stdinFromFile", s).Errorf("unable to replace os.Stdin with input from file: %w", err)
				}
				os.Stdin = f
				log.Info().Str("stdinFromFile", s).Msg("attaching os.Stdin to file")
			}
			return nil
		},
	},
	&cli.StringFlag{
		Category: "override",
		Name:     "stdoutToFile",
		Value:    "",
		Action: func(context *cli.Context, s string) error {
			if s != "" {
				f, err := os.OpenFile(s, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, os.ModePerm)
				if err != nil {
					return eb.Build().Str("stdoutToFile", s).Errorf("unable to replace os.Stdout with output to file: %w", err)
				}
				os.Stdout = f
				log.Info().Str("stdoutToFile", s).Msg("attaching os.Stdout to file")
			}
			return nil
		},
	},
	&cli.StringFlag{
		Category: "override",
		Name:     "stderrToFile",
		Value:    "",
		Action: func(context *cli.Context, s string) error {
			if s != "" {
				f, err := os.OpenFile(s, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, os.ModePerm)
				if err != nil {
					return eb.Build().Str("stderrToFile", s).Errorf("unable to replace os.Stderr with output to file: %w", err)
				}
				os.Stderr = f
				log.Info().Str("stderrToFile", s).Msg("attaching os.Stderr to file")
			}
			return nil
		},
	},
}

package adrindex

import (
	"context"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "adrindex",
		Usage: "generate doc/adr/INDEX.md, the table of every ADR with its status and date",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "dir",
				Value: "doc/adr",
				Usage: "directory holding the ADRs",
			},
			&cli.StringFlag{
				Name:  "out",
				Usage: "output file (default: <dir>/INDEX.md)",
			},
			&cli.BoolFlag{
				Name:  "check",
				Usage: "verify the committed index matches the ADRs instead of writing; exit non-zero on drift",
			},
		},
		Action: adrIndexAction,
	}
}

func adrIndexAction(ctx context.Context, cmd *cli.Command) (err error) {
	dir := cmd.String("dir")
	out := cmd.String("out")
	if out == "" {
		out = filepath.Join(dir, "INDEX.md")
	}
	if cmd.Bool("check") {
		err = Check(dir, out)
		if err == nil {
			log.Info().Str("out", out).Msg("adrindex check passed")
		}
		return
	}
	err = Generate(dir, out)
	if err == nil {
		log.Info().Str("out", out).Msg("adrindex written")
	}
	return
}

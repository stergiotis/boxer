package changelogindex

import (
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "changelogindex",
		Usage: "Generate doc/changelog/INDEX.md from the window-bounded entries",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "dir",
				Value: "doc/changelog",
				Usage: "directory holding the window-bounded entries",
			},
			&cli.StringFlag{
				Name:  "out",
				Usage: "output file (default: <dir>/INDEX.md)",
			},
			&cli.BoolFlag{
				Name:  "check",
				Usage: "verify the committed index matches the entries instead of writing; exit non-zero on drift",
			},
		},
		Action: changelogIndexAction,
	}
}

func changelogIndexAction(ctx *cli.Context) (err error) {
	dir := ctx.String("dir")
	out := ctx.String("out")
	if out == "" {
		out = filepath.Join(dir, "INDEX.md")
	}
	if ctx.Bool("check") {
		err = Check(dir, out)
		if err == nil {
			log.Info().Str("out", out).Msg("changelogindex check passed")
		}
		return
	}
	err = Generate(dir, out)
	if err == nil {
		log.Info().Str("out", out).Msg("changelogindex written")
	}
	return
}

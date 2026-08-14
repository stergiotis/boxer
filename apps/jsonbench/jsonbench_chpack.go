package main

import (
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// chpackCommand installs leeway's SQL read surface (ADR-0171 §SD2), so the
// trial's queries can be written in the leeway query vocabulary instead of
// open-coding the lane arithmetic. The families are sets of CREATE OR REPLACE
// FUNCTION macros: they inline at analysis time, so installing them changes
// how queries read, not what they cost.
//
// The `chpack` alias is kept because the trial's README documents that
// spelling. It installs more than the pack now — which is the fix for one of
// the trial's own findings, not a surprise to it.
func chpackCommand() *cli.Command {
	return &cli.Command{
		Name:    "sqlsurface",
		Aliases: []string{"chpack"},
		Usage:   "install leeway's SQL read surface (ADR-0171) on the target server",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Value: "http://localhost:8123/"},
			&cli.StringFlag{Name: "user", Value: "default"},
			&cli.StringFlag{Name: "password"},
		},
		Action: func(cCtx *cli.Context) (err error) {
			client := chclient.New(chclient.Config{
				URL:      cCtx.String("url"),
				User:     cCtx.String("user"),
				Password: cCtx.String("password"),
			}, nil)
			err = lwsqlsurface.Install(cCtx.Context, client)
			if err != nil {
				return
			}
			log.Info().Int("version", lwsqlsurface.Version).
				Int("functions", len(lwsqlsurface.DeclaredFunctions())).
				Msg("leeway SQL read surface installed")
			return
		},
	}
}

package main

import (
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
)

// chpackCommand installs the ADR-0162 co/ragged SQL-UDF pack, so the trial's
// queries can be written in the leeway query vocabulary instead of open-coding
// the lane arithmetic. The pack is a set of CREATE OR REPLACE FUNCTION macros:
// they inline at analysis time, so installing it changes how queries read, not
// what they cost.
func chpackCommand() *cli.Command {
	return &cli.Command{
		Name:  "chpack",
		Usage: "install the leeway co/ragged SQL-UDF pack (ADR-0162) on the target server",
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
			err = chpack.Install(cCtx.Context, client)
			if err != nil {
				return
			}
			log.Info().Int("version", chpack.Version).Int("functions", len(chpack.Functions())).
				Msg("leeway co/ragged pack installed")
			return
		},
	}
}

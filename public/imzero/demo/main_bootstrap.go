//go:build bootstrap

package demo

import (
	"github.com/rs/zerolog/log"
	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/imzero/application"
)

func NewCommand() *cli.Command {
	return &cli.Command{}
}

func mainE(app *application.Application) error {
	log.Fatal().Msg("boostrap build tag set, should never get here")
	return nil
}

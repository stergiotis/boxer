//go:build bootstrap

package demo

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/imzero/application"
	"github.com/urfave/cli/v2"
)

func NewCommand() *cli.Command {
	return &cli.Command{}
}
func mainE(app *application.Application) error {
	log.Fatal().Msg("boostrap build tag set, should never get here")
	return nil
}

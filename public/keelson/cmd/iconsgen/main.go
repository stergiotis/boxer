//go:build llm_generated_opus47

package main

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons/generator"
	"github.com/urfave/cli/v2"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	app := &cli.App{
		Name:     "iconsgen",
		Commands: []*cli.Command{generator.NewCommand()},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal().Err(err).Msg("generator failed")
	}
}

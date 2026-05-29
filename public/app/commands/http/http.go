package http

import (
	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/app/commands/http/serve"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "http",
		Subcommands: []*cli.Command{
			serve.NewCommand(),
		},
	}
}

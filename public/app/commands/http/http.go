package http

import (
	cli "github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/app/commands/http/serve"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "http",
		Usage: "HTTP tools",
		Commands: []*cli.Command{
			serve.NewCommand(),
		},
	}
}

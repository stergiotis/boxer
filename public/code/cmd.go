package code

import (
	"github.com/stergiotis/boxer/public/code/analysis"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "code",
		Subcommands: []*cli.Command{
			analysis.NewCliCommand(),
		},
	}
}

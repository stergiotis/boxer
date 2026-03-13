package analysis

import (
	"github.com/stergiotis/boxer/public/code/analysis/golang"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "analysis",
		Subcommands: []*cli.Command{
			golang.NewCliCommand(),
		},
	}
}

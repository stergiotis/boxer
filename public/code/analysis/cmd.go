package analysis

import (
	"github.com/stergiotis/boxer/public/code/analysis/golang"
	"github.com/stergiotis/boxer/public/code/analysis/sccapplet"
	"github.com/urfave/cli/v3"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "analysis",
		Usage: "analyses of the source tree",
		Commands: []*cli.Command{
			golang.NewCliCommand(),
			sccapplet.NewCliCommand(),
		},
	}
}

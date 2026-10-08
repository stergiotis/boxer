package analysis

import (
	"github.com/stergiotis/boxer/public/code/analysis/golang"
	"github.com/urfave/cli/v3"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "analysis",
		Commands: []*cli.Command{
			golang.NewCliCommand(),
		},
	}
}

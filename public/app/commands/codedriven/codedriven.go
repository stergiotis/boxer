package codedriven

import (
	"github.com/stergiotis/boxer/public/code/analysis/golang/llmuse"
	"github.com/stergiotis/boxer/public/code/analysis/golang/stubber"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "codedriven",
		Subcommands: []*cli.Command{
			{
				Name: "go",
				Subcommands: []*cli.Command{
					//apidoc.NewCliCommandV2(),
					stubber.NewCliCommand(),
					llmuse.NewCliCommand(),
				},
			},
		},
	}
}

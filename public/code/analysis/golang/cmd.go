package golang

import (
	"github.com/stergiotis/boxer/public/code/analysis/golang/llmuse"
	"github.com/stergiotis/boxer/public/code/analysis/golang/stubber"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "golang",
		Subcommands: []*cli.Command{
			llmuse.NewCliCommand(),
			stubber.NewCliCommand(),
		},
	}
}

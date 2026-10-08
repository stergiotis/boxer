package golang

import (
	"github.com/stergiotis/boxer/public/code/analysis/golang/wasmsurvey"
	"github.com/urfave/cli/v3"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "golang",
		Commands: []*cli.Command{
			wasmsurvey.NewCliCommand(),
		},
	}
}

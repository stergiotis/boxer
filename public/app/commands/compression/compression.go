package compression

import (
	cli "github.com/urfave/cli/v3"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "compression",
		Usage: "compression experiments",
		Commands: []*cli.Command{
			NewDictCommand(),
		},
	}
}

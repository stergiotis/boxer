package compression

import (
	cli "github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "compression",
		Subcommands: []*cli.Command{
			NewDictCommand(),
		},
	}
}

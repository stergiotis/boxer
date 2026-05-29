package datasource

import (
	cli "github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:        "datasource",
		Subcommands: []*cli.Command{},
	}
}

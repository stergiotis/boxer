package datasource

import (
	cli "github.com/urfave/cli/v3"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:     "datasource",
		Usage:    "placeholder group; it has no subcommands",
		Commands: []*cli.Command{},
	}
}

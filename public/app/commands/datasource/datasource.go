package datasource

import (
	cli "github.com/urfave/cli/v3"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:     "datasource",
		Commands: []*cli.Command{},
	}
}

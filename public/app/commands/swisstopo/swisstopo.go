package swisstopo

import (
	cli "github.com/urfave/cli/v2"
)

func NewCliCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "swisstopo",
		Usage: "swisstopo geodata tools",
		Subcommands: []*cli.Command{
			newMirrorCommand(),
			newLineOfSightCommand(),
		},
	}
	return
}

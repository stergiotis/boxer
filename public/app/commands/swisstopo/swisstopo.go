package swisstopo

import (
	cli "github.com/urfave/cli/v3"
)

func NewCliCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "swisstopo",
		Usage: "swisstopo geodata tools",
		Commands: []*cli.Command{
			newMirrorCommand(),
			newLineOfSightCommand(),
		},
	}
	return
}

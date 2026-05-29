package watch

import (
	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/app/commands/watch/fs"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "watch",
		Subcommands: []*cli.Command{
			fs.NewCommand(),
		},
	}
}

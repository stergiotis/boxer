package watch

import (
	cli "github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/app/commands/watch/fs"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "watch",
		Usage: "watch for changes",
		Commands: []*cli.Command{
			fs.NewCommand(),
		},
	}
}

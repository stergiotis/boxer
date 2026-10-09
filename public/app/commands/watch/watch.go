package watch

import (
	cli "github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/app/commands/watch/fs"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "watch",
		Commands: []*cli.Command{
			fs.NewCommand(),
		},
	}
}

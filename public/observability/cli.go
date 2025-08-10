package observability

import (
	"slices"

	"github.com/stergiotis/boxer/public/observability/tracing"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "observability",
		Subcommands: slices.Concat(
			tracing.NewCliCommands(),
		),
	}
}

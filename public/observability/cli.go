package observability

import (
	"slices"

	"github.com/stergiotis/boxer/public/observability/tracing"
	"github.com/urfave/cli/v3"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "observability",
		Usage: "observability tools",
		Commands: slices.Concat(
			tracing.NewCliCommands(),
		),
	}
}

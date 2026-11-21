// Package errordemo is `boxer error demo`: it returns a nested structured
// error so a person can see how the logger renders one.
package errordemo

import (
	"context"
	"io"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v3"
)

// NewCliCommand returns the `error` command group.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "error",
		Usage: "error-handling helpers",
		Commands: []*cli.Command{
			{Name: "demo",
				Usage: "return a nested structured error, to see how the logger renders one",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					err1 := eb.Build().Uint64("aLargeUint64", math.MaxUint64/2).Str("myString", "this is a string value").Type("myType", struct {
						FieldA string
						FieldB int
					}{}).Errorf("this is the error message: %w", io.ErrClosedPipe)
					err2 := eb.Build().Str("data2", "too").Errorf("error2: %w", err1)
					return err2
				},
			},
		},
	}
}

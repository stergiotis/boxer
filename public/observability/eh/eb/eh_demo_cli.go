package eb

import (
	"context"
	"io"
	"math"

	"github.com/urfave/cli/v3"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "error",
		Commands: []*cli.Command{
			{Name: "demo",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					err1 := Build().Uint64("aLargeUint64", math.MaxUint64/2).Str("myString", "this is a string value").Type("myType", struct {
						FieldA string
						FieldB int
					}{}).Errorf("this is the error message: %w", io.ErrClosedPipe)
					err2 := Build().Str("data2", "too").Errorf("error2: %w", err1)
					return err2
				},
			},
		},
	}
}

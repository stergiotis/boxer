package key

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"os"

	"github.com/stergiotis/boxer/public/observability/eh"
	cli "github.com/urfave/cli/v3"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "key",
		Usage: "cipher key related commands",
		Commands: []*cli.Command{
			{
				Name:  "random",
				Usage: "writes a cryptographically safe random key hex encoded to stdout",
				Flags: []cli.Flag{
					&cli.UintFlag{
						Name:  "length",
						Value: 32,
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					l := cmd.Uint("length")
					key := make([]byte, l)
					var err error
					_, err = cryptorand.Read(key)
					if err != nil {
						return eh.Errorf("unable to generate random number: %w", err)
					}
					_, err = hex.NewEncoder(os.Stdout).Write(key)
					if err != nil {
						return eh.Errorf("unable to write to stdout: %w", err)
					}
					_, err = os.Stdout.WriteString("\n")
					if err != nil {
						return eh.Errorf("unable to write to stdout: %w", err)
					}
					return nil
				},
			},
		},
	}
}

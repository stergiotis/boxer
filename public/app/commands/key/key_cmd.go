package key

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"os"

	"github.com/stergiotis/boxer/public/observability/eh"
	cli "github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "key",
		Usage: "cipher key related commands",
		Subcommands: []*cli.Command{
			{
				Name:  "random",
				Usage: "writes a cryptographically safe random key hex encoded to stdout",
				Flags: []cli.Flag{
					&cli.UintFlag{
						Name:  "length",
						Value: 32,
					},
				},
				Action: func(context *cli.Context) error {
					l := context.Uint("length")
					key := make([]byte, l, l)
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

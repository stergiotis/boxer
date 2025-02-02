package cbor

import (
	"bufio"
	"github.com/fxamacker/cbor/v2"
	"github.com/urfave/cli/v2"
	"io"
	"os"
)

func NewCommand() *cli.Command {
	return &cli.Command{
		Name: "cbor",
		Subcommands: []*cli.Command{
			diagCommand(),
		},
	}
}
func diagCommand() *cli.Command {
	return &cli.Command{
		Name:        "diagnostics",
		Description: "",
		Aliases:     []string{"diag"},
		Flags:       []cli.Flag{},
		Action: func(ctx *cli.Context) error {
			r := bufio.NewReader(os.Stdin)
			w := bufio.NewWriter(os.Stdout)

			b, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			rest := b
			for len(rest) > 0 {
				var diag string
				diag, rest, err = cbor.DiagnoseFirst(rest)
				if err != nil {
					return err
				}
				_, err = w.WriteString(diag)
				if err != nil {
					return err
				}
				_, err = w.WriteString("\n")
				if err != nil {
					return err
				}
			}
			return nil
		},
		Usage: "reads cbor from stdin and emits RFC8949 diagnose output to stdout",
	}
}

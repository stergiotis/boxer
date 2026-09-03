package cbor

import (
	"bufio"
	"io"
	"os"

	"github.com/fxamacker/cbor/v2"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/cbor/diag"
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
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "pretty",
				Usage: "indent nested containers one element per line and label known tags (ADR-0219 §SD6); without it, one item per line in the library's compact notation",
			},
			&cli.IntFlag{
				Name:  "width",
				Usage: "line width a container must fit in to stay on one line under --pretty",
				Value: diag.DefaultWidth,
			},
		},
		Action: func(ctx *cli.Context) error {
			r := bufio.NewReader(os.Stdin)
			w := bufio.NewWriter(os.Stdout)

			b, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			if ctx.Bool("pretty") {
				var s string
				s, err = diag.String(b, diag.Options{
					Width:       ctx.Int("width"),
					TagComments: true,
					Sequence:    true,
				})
				if _, werr := w.WriteString(s + "\n"); werr != nil {
					return werr
				}
				if err != nil {
					_ = w.Flush()
					return eh.Errorf("input is not well-formed cbor: %w", err)
				}
				return w.Flush()
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
			return w.Flush()
		},
		Usage: "reads cbor from stdin and emits RFC8949 diagnose output to stdout",
	}
}

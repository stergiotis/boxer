package dsl

import (
	"encoding/json"
	"fmt"
	chparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/davecgh/go-spew/spew"
	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
	"github.com/yassinebenaid/godump"
	"io"
	"os"
	"reflect"
	"strings"
)

func astCommand() *cli.Command {
	return &cli.Command{
		Name: "ast",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "format",
				Value: "spew",
				Usage: fmt.Sprintf("one of the following values: %v", []string{"sql", "godump", "spew", "cbor", "json"}),
			},
		},
		Action: func(context *cli.Context) (err error) {
			var dsl *Dsl
			tableIdTransf := NewTableIdTransformer()
			dsl, err = NewDsl(tableIdTransf)
			if err != nil {
				return
			}

			var b []byte
			b, err = io.ReadAll(os.Stdin)
			if err != nil {
				return
			}
			sql := string(b)
			err = dsl.Parse(sql)
			if err != nil {
				err = eh.Errorf("unable to parse sql: %w", err)
				return
			}

			format := context.String("format")
			switch format {
			case "highlight", "hl":
				var hl *SyntaxHighlighter
				if true {
					hl = NewSyntaxHighlighter(AnsiHighlightFunc)
				} else {
					hl = NewSyntaxHighlighter(func(expr chparser.Expr) (before string, after string) {
						_, t, _ := strings.Cut(reflect.TypeOf(expr).String(), ".")
						before = "<" + t + ">"
						after = "</" + t + ">"
						return
					})
				}
				var s string
				s, err = hl.Highlite(sql, dsl.Exprs)
				if err != nil {
					return err
				}
				_, err = os.Stdout.WriteString(s)
				if err != nil {
					return err
				}
				break
			case "godump":
				d := godump.Dumper{
					Indentation:             "  ",
					ShowPrimitiveNamedTypes: false,
					HidePrivateFields:       true,
					Theme:                   godump.DefaultTheme,
				}
				for _, expr := range dsl.Exprs {
					err = d.Fprint(os.Stdout, expr)
					if err != nil {
						return err
					}
					_, err = os.Stdout.WriteString("\n\n")
					if err != nil {
						return err
					}
				}
				break
			case "spew":
				s := spew.NewDefaultConfig()
				s.DisableMethods = true
				s.DisablePointerAddresses = true
				s.DisableCapacities = true
				s.SortKeys = true
				for _, expr := range dsl.Exprs {
					s.Fdump(os.Stdout, expr)
					_, err = os.Stdout.WriteString("\n\n")
					if err != nil {
						return err
					}
				}
				break
			case "json":
				jsonenc := json.NewEncoder(os.Stdout)
				jsonenc.SetEscapeHTML(false)
				jsonenc.SetIndent("", "  ")
				for _, expr := range dsl.Exprs {
					err = jsonenc.Encode(expr)
					if err != nil {
						err = eh.Errorf("unable to encode ast to json: %w", err)
						return
					}
					_, err = os.Stdout.WriteString("\n")
					if err != nil {
						return err
					}
				}
				break
			case "cbor":
				var encmode cbor.EncMode
				encmode, err = cbor.CanonicalEncOptions().EncMode()
				if err != nil {
					err = eh.Errorf("unable to create encoding mode: %w", err)
					return err
				}
				enc := encmode.NewEncoder(os.Stdout)
				for _, expr := range dsl.Exprs {
					err = enc.Encode(expr)
					if err != nil {
						err = eh.Errorf("unable to encode ast to json: %w", err)
						return
					}
				}
				break
			case "sql":
				for _, expr := range dsl.Exprs {
					_, err = os.Stdout.WriteString(expr.String())
					if err != nil {
						return err
					}
					_, err = os.Stdout.WriteString(";\n")
					if err != nil {
						return err
					}
				}
			default:
				err = eb.Build().Str("format", format).Errorf("unhandled format")
				return
			}
			return
		},
	}
}
func NewCommand() *cli.Command {
	return &cli.Command{
		Name: "dsl",
		Subcommands: []*cli.Command{
			astCommand(),
		},
	}
}

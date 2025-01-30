package dsl

import (
	"fmt"
	"github.com/antlr4-go/antlr/v4"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
	"github.com/yassinebenaid/godump"
	"os"
	"reflect"
	"strings"
)

func parseCommand() *cli.Command {
	return &cli.Command{
		Name: "parse",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "format",
				Value: "hl",
				Usage: fmt.Sprintf("Output format. One of the following values: %v", []string{"hl"}),
			},
		},
		Action: func(context *cli.Context) (err error) {
			dql := NewParsedDqlQuery()
			err = dql.ParseFromReader(os.Stdin)
			if err != nil {
				err = eh.Errorf("unable to Parse sql: %w", err)
				return
			}

			var pss *ParamSlotSet
			pss, err = dql.GetParamSlotSet()
			if err != nil {
				err = eh.Errorf("unable to get param slot set: %w", err)
				return
			}
			for param, types := range pss.NamesAndTypes() {
				log.Info().Str("param", param).Strs("types", types.Slice()).Msg("found param slot")
			}

			format := context.String("format")
			switch format {
			case "highlight", "hl":
				var hl *SyntaxHighlighter
				if true {
					hl = NewSyntaxHighlighter(AnsiHighlightFunc)
				} else {
					hl = NewSyntaxHighlighter(func(node antlr.Tree) (before string, after string) {
						_, t, _ := strings.Cut(reflect.TypeOf(node).String(), ".")
						before = "<" + t + ">"
						after = "</" + t + ">"
						return
					})
				}
				var s string
				s, err = hl.Highlight(dql.GetInputSql(), dql.GetInputParseTree())
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
				var _ = d
				break
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
			parseCommand(),
		},
	}
}

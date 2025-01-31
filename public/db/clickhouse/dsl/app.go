package dsl

import (
	"fmt"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
	"github.com/yassinebenaid/godump"
	"os"
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
				var a *AnsiHighlighter
				a, err = NewAnsiHighlighter(&godump.DefaultTheme)
				if err != nil {
					return
				}
				var h *HtmlHighlighter
				h = NewHtmlHighlighter()

				_, err = os.Stdout.WriteString(`<html><head><link rel="stylesheet" href="styles.css"/><meta name="referrer" content="no-referrer" /></head><body><style>`)
				if err != nil {
					return err
				}
				{
					var style []byte
					style, err = os.ReadFile("style.css")
					if err != nil {
						return err
					}
					_, err = os.Stdout.Write(style)
					if err != nil {
						return
					}
					_, err = os.Stdout.WriteString("</style>")
					if err != nil {
						return
					}
				}

				var _ = a
				hl := NewSyntaxHighlighter(h)
				var s string
				s, err = hl.Highlight(dql.GetInputSql(), dql.GetInputParseTree())
				if err != nil {
					return err
				}
				_, err = os.Stdout.WriteString(s)
				if err != nil {
					return err
				}
				_, err = os.Stdout.WriteString(`</body></html>
`)
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

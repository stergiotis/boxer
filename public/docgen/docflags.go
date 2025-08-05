package docgen

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"

	md "github.com/nao1215/markdown"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
)

var DocFlags = []cli.Flag{
	&cli.BoolFlag{
		Name:     "markdownEcho",
		Category: "doc",
		Usage:    "echos the current command line (os.Args) as markdown shell code block [EXPERIMENTAL]",
		EnvVars:  []string{"BOXER_MARKDOWN_ECHO"},
		Action: func(context *cli.Context, b bool) error {
			if b {
				args := slices.Clone(os.Args)
				cwd, err := os.Getwd()
				if err == nil && len(args) > 0 {
					var s string
					s, err = filepath.Rel(cwd, args[0])
					if err == nil {
						args[0] = "./" + s
					}
				}
				return md.NewMarkdown(os.Stdout).CodeBlocks(md.SyntaxHighlightShell,
					strings.Join(args, " ")).LF().Build()
			}
			return nil
		},
	},
}

func NewDocCli() *cli.Command {
	syntaxHighlightFlag, syntaxHighlightFunc := cli2.BuildEnumStringFlagStr([]md.SyntaxHighlight{
		md.SyntaxHighlightNone,
		md.SyntaxHighlightText,
		md.SyntaxHighlightAPIBlueprint,
		md.SyntaxHighlightShell,
		md.SyntaxHighlightGo,
		md.SyntaxHighlightJSON,
		md.SyntaxHighlightYAML,
		md.SyntaxHighlightXML,
		md.SyntaxHighlightHTML,
		md.SyntaxHighlightCSS,
		md.SyntaxHighlightJavaScript,
		md.SyntaxHighlightTypeScript,
		md.SyntaxHighlightSQL,
		md.SyntaxHighlightC,
		md.SyntaxHighlightCSharp,
		md.SyntaxHighlightCPlusPlus,
		md.SyntaxHighlightJava,
		md.SyntaxHighlightKotlin,
		md.SyntaxHighlightPHP,
		md.SyntaxHighlightPython,
		md.SyntaxHighlightRuby,
		md.SyntaxHighlightSwift,
		md.SyntaxHighlightScala,
		md.SyntaxHighlightRust,
		md.SyntaxHighlightObjectiveC,
		md.SyntaxHighlightPerl,
		md.SyntaxHighlightLua,
		md.SyntaxHighlightDart,
		md.SyntaxHighlightClojure,
		md.SyntaxHighlightGroovy,
		md.SyntaxHighlightR,
		md.SyntaxHighlightHaskell,
		md.SyntaxHighlightErlang,
		md.SyntaxHighlightElixir,
		md.SyntaxHighlightOCaml,
		md.SyntaxHighlightJulia,
		md.SyntaxHighlightScheme,
		md.SyntaxHighlightFSharp,
		md.SyntaxHighlightCoffeeScript,
		md.SyntaxHighlightVBNet,
		md.SyntaxHighlightTeX,
		md.SyntaxHighlightDiff,
		md.SyntaxHighlightApache,
		md.SyntaxHighlightDockerfile,
		md.SyntaxHighlightMermaid,
	}, md.SyntaxHighlightNone, "syntaxHighlight")
	return &cli.Command{
		Name: "doc",
		Subcommands: []*cli.Command{
			{
				Name: "markdown",
				Subcommands: []*cli.Command{
					{
						Name:  "codeblock",
						Usage: "wraps text from stdin in a markdown code block",
						Flags: []cli.Flag{syntaxHighlightFlag},
						Action: func(context *cli.Context) error {
							buf := bytes.NewBuffer(make([]byte, 0, 4*4096))
							_, err := buf.ReadFrom(os.Stdin)
							if err != nil {
								return eh.Errorf("unable to read from stdin: %w", err)
							}
							return md.NewMarkdown(os.Stdout).CodeBlocks(syntaxHighlightFunc(context), buf.String()).Build()
						},
					},
				},
			},
		},
	}
}

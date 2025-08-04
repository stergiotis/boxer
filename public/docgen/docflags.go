package docgen

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	md "github.com/nao1215/markdown"
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

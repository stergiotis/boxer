package code

import (
	"github.com/stergiotis/boxer/public/code/analysis"
	"github.com/stergiotis/boxer/public/code/rewrite/importmove"
	"github.com/urfave/cli/v3"
)

// NewCliCommand returns the `code` parent command: `code analysis` and
// `code importmove`; extraSubcommands lets sibling packages mount their
// own code-related tools (e.g. clickhouse/dsl/genbuildertest) without
// forming an import edge back to `code`.
func NewCliCommand(extraSubcommands ...*cli.Command) *cli.Command {
	subs := make([]*cli.Command, 0, 2+len(extraSubcommands))
	subs = append(subs, analysis.NewCliCommand(), importmove.NewCliCommand())
	subs = append(subs, extraSubcommands...)
	return &cli.Command{
		Name:     "code",
		Usage:    "code analysis and code-generation checks",
		Commands: subs,
	}
}

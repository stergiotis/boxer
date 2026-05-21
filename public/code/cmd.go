package code

import (
	"github.com/stergiotis/boxer/public/code/analysis"
	"github.com/urfave/cli/v2"
)

// NewCliCommand returns the `code` parent command. Currently exposes
// `code analysis`; extraSubcommands lets sibling packages mount their
// own code-related tools (e.g. clickhouse/dsl/genbuildertest) without
// forming an import edge back to `code`.
func NewCliCommand(extraSubcommands ...*cli.Command) *cli.Command {
	subs := make([]*cli.Command, 0, 1+len(extraSubcommands))
	subs = append(subs, analysis.NewCliCommand())
	subs = append(subs, extraSubcommands...)
	return &cli.Command{
		Name:        "code",
		Subcommands: subs,
	}
}

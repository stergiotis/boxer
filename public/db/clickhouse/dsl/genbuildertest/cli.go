package genbuildertest

import (
	cli "github.com/urfave/cli/v2"
)

// NewCliCommand returns the `gen-builder-tests` subcommand. Mounted
// under boxer's `code` parent (via code.NewCliCommand's variadic
// extras) so the command tree groups all code-generation tools.
//
// The action walks the chsql corpus, runs each entry through the
// canonicalize pipeline + Grammar2 + CST→AST conversion, calls
// ToGoCode() to emit builder calls, and writes a test file that
// reconstructs each query through the builder API and reparses the
// resulting SQL.
func NewCliCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "gen-builder-tests",
		Usage: "generate Go tests that round-trip the chsql corpus through the AST builder",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "out",
				Value: "chsql/builder_generated_test.go",
				Usage: "output file path",
			},
			&cli.StringFlag{
				Name:  "pkg",
				Value: "chsql_test",
				Usage: "package name for the generated file",
			},
		},
		Action: runCli,
	}
	return
}

func runCli(ctx *cli.Context) (err error) {
	err = Run(ctx.String("out"), ctx.String("pkg"))
	return
}

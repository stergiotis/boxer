//go:build !(llm_generated_gemini3pro || llm_generated_opus46)

package gov

import (
	"github.com/stergiotis/boxer/public/gov/doclint"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/urfave/cli/v2"
)

// NewCliCommand exposes the always-on subset of the gov subcommand tree.
//
// LLM-generated subcommands (filename, callsites, repo, commitdigest) live
// behind the llm_generated_* build tags in gov.go; doclint is unconditionally
// available and is the only subcommand registered here.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "gov",
		Subcommands: cli2.CommandsNilRemoved(
			doclint.NewCliCommand(),
		),
	}
}

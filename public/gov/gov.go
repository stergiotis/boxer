//go:build llm_generated_gemini3pro || llm_generated_opus46
package gov

import (
	"github.com/stergiotis/boxer/public/gov/callsites"
	"github.com/stergiotis/boxer/public/gov/commitdigest"
	"github.com/stergiotis/boxer/public/gov/filename"
	"github.com/stergiotis/boxer/public/gov/repo"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "gov",
		Subcommands: cli2.CommandsNilRemoved(
			filename.NewCliCommand(),
			callsites.NewCliCommand(),
			repo.NewCliCommand(),
			commitdigest.NewCliCommand(),
		),
	}
}

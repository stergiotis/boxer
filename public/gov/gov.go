//go:build llm_generated_gemini3pro
package gov

import (
	"github.com/stergiotis/boxer/public/gov/callsites"
	"github.com/stergiotis/boxer/public/gov/filename"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "gov",
		Subcommands: cli2.CommandsNilRemoved(
			filename.NewCliCommand(),
			callsites.NewCliCommand(),
		),
	}
}

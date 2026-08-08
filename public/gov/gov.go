package gov

import (
	"github.com/stergiotis/boxer/public/gov/buildtags"
	"github.com/stergiotis/boxer/public/gov/callsites"
	"github.com/stergiotis/boxer/public/gov/codelint"
	"github.com/stergiotis/boxer/public/gov/commitdigest"
	"github.com/stergiotis/boxer/public/gov/doclint"
	"github.com/stergiotis/boxer/public/gov/filename"
	"github.com/stergiotis/boxer/public/gov/gate"
	"github.com/stergiotis/boxer/public/gov/licensegate"
	"github.com/stergiotis/boxer/public/gov/llmtag"
	"github.com/stergiotis/boxer/public/gov/repo"
	"github.com/stergiotis/boxer/public/gov/skeleton"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "gov",
		Subcommands: cli2.CommandsNilRemoved(
			doclint.NewCliCommand(),
			codelint.NewCliCommand(),
			buildtags.NewCliCommand(),
			gate.NewCliCommand(),
			skeleton.NewCliCommand(),
			filename.NewCliCommand(),
			callsites.NewCliCommand(),
			repo.NewCliCommand(),
			commitdigest.NewCliCommand(),
			llmtag.NewCliCommand(),
			licensegate.NewCliCommand(),
		),
	}
}

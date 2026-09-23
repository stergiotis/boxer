// Package stevedoredemo is the `stevedoredemo` command group: the example
// application of ADR-0252 §SD6, a processor and a lander an operator can run
// end to end under a streaming framework with the pipeline configuration
// beside this file. `process` splits a body into lines through the processor
// host; `land` consumes the items and prints each one, dead-lettering what it
// cannot decode through the facts-bound store or a log line.
//
// It is an example, not a shipped surface: an application writes its own
// handler and sink and links the hosts into a binary of its own.
package stevedoredemo

import (
	"github.com/urfave/cli/v2"
)

// NewCliCommand is the command group.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "stevedoredemo",
		Usage: "the stevedore example: a line-splitting processor and a printing lander (ADR-0252)",
		Subcommands: []*cli.Command{
			newProcessCommand(),
			newLandCommand(),
		},
	}
}

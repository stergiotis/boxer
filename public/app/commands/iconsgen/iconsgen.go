// Package iconsgen exposes the keelson phosphor-icon lookup generator as a boxer
// subcommand, folding the former standalone iconsgen main into public/app per
// the entry-point standard. The generation logic lives in
// keelson/runtime/icons/generator.
package iconsgen

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons/generator"
	"github.com/urfave/cli/v2"
)

// NewCliCommand returns the `iconsgen` subcommand. The generator command is
// itself named "generate"; namespacing it under a tool-named parent avoids a
// clash with other "generate" commands.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:        "iconsgen",
		Usage:       "generate the keelson phosphor-icon lookup table",
		Subcommands: []*cli.Command{generator.NewCommand()},
	}
}

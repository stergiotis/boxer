// Package egui2gen exposes the FFFI2 egui2-widget code generator as a boxer
// subcommand. The generation logic lives in the egui2/driver command; this
// folds the former standalone egui2gen main into public/app per the
// entry-point standard (the old egui2gen.sh launcher is replaced by
// `./boxer.sh egui2gen generate …`).
package egui2gen

import (
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/driver"
	"github.com/urfave/cli/v2"
)

// NewCliCommand returns the `egui2gen` subcommand, namespacing the egui2 driver
// generator (which is itself named "generate", and would otherwise collide with
// other generators) under a tool-named parent.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:        "egui2gen",
		Usage:       "FFFI2 code generator for egui2 widgets",
		Subcommands: []*cli.Command{driver.NewCliCommand()},
	}
}

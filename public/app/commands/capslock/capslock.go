//go:build llm_generated_opus47

// Package capslock is the CLI wiring for the ADR-0026 §SD10 capslock
// cross-checker. Mirrors the pattern established by designsystem and
// envgen: a thin urfave/cli v2 surface that bridges to the existing
// implementation at
// [github.com/stergiotis/boxer/public/keelson/security/capslock]
// without changing the library.
//
// The implementation function [capslock.Run] takes an os.Args-shaped
// slice and returns an exit code. The bridge here constructs that
// slice from the urfave flag context so the same gate is reachable as:
//
//   - `./pebble.sh capslock --in=report.json` (aggregated app)
//   - `./cmd/capslock-check -in=report.json` (standalone CI
//     binary; canonical entry for scripts/ci/capslock-check.sh)
//
// Both paths route through the same Run function — the cross-check
// logic, severity policy, and exit-code semantics are identical.
package capslock

import (
	"github.com/urfave/cli/v2"

	capslocklib "github.com/stergiotis/boxer/public/keelson/security/capslock"
)

// NewCliCommand returns the `capslock` command for the pebble.sh-
// aggregated app. Single `--in` flag mirrors the standalone binary's
// `-in` flag (path to capslock JSON; `-` reads stdin).
func NewCliCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:        "capslock",
		Usage:       "ADR-0026 §SD10 cap-vs-manifest cross-checker (advisory, M2.7)",
		Description: "Reads a capslock JSON report and cross-checks each detected capability against the AppI.Manifest declarations. Advisory in M2.7 — exit 0 even on findings; exit 1 only on I/O or decode failure.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "in",
				Value: "-",
				Usage: "path to capslock JSON file ('-' reads stdin)",
			},
		},
		Action: func(ctx *cli.Context) (err error) {
			args := []string{"capslock-check", "-in", ctx.String("in")}
			exit := capslocklib.Run(args)
			if exit != 0 {
				err = cli.Exit("", exit)
			}
			return
		},
	}
	return
}

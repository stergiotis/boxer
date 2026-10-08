// Package capslock is the CLI wiring for the ADR-0026 §SD10 capslock
// cross-checker. Mirrors the pattern established by designsystem and
// envgen: a thin urfave/cli v2 surface that bridges to the existing
// implementation at
// [github.com/stergiotis/boxer/public/keelson/security/capslock]
// without changing the library.
//
// The implementation function [capslock.Run] takes an os.Args-shaped
// slice and returns an exit code. The bridge here constructs that slice
// from the urfave flag context, so the gate is reachable as
// `./boxer.sh capslock [--root=.]`.
//
// The analysis runs in-process against the source tree — there is no
// capslock binary to install and no JSON report to produce first. The
// same check runs as a plain Go test (TestAnalyse_MatchesBaseline), which
// is what scripts/ci/lint.sh gates on; this command exists for running it
// by hand against an arbitrary root.
package capslock

import (
	"context"
	"github.com/urfave/cli/v3"

	capslocklib "github.com/stergiotis/boxer/public/keelson/security/capslock"
)

// NewCliCommand returns the `capslock` command for the boxer.sh-aggregated
// app.
func NewCliCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "capslock",
		Usage: "ADR-0026 §SD10 cap-vs-manifest cross-checker",
		Description: "Runs capslock over the app packages under --root and cross-checks each " +
			"capability an app's own code exercises against its AppI.Manifest declarations. " +
			"Findings already accepted in the baseline are reported and exit 0; a finding " +
			"outside the baseline, or a baseline entry that no longer reproduces, exits 1.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "root",
				Value: ".",
				Usage: "module root to analyse",
			},
			&cli.StringFlag{
				Name:  "tags",
				Usage: "build tags to load with (default: the root's tags file)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) (err error) {
			args := []string{"capslock-check", "-root", cmd.String("root")}
			if t := cmd.String("tags"); t != "" {
				args = append(args, "-tags", t)
			}
			exit := capslocklib.Run(args)
			if exit != 0 {
				err = cli.Exit("", exit)
			}
			return
		},
	}
	return
}

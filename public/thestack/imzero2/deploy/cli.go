package deploy

import (
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

// NewCommand returns the `imzero2 deploy` subcommand (ADR-0085). It is
// meant to be fired by a systemd timer: it polls the workspace clone's
// tags and, on a newer release tag than `current`, checks out, builds via
// the project scripts, snapshots the artifacts into releases/<tag>/,
// verifies the build actually streams (ws_probe), then atomically swaps the
// `current` symlink and restarts the service. Phase 1: happy path only.
func NewCommand() *cli.Command {
	return &cli.Command{
		Name:  "deploy",
		Usage: "pull a new release tag, build on-box, gate, and atomically deploy (ADR-0085)",
		Description: "On-box pull-build-and-atomic-deploy for the headless demo (ADR-0085).\n\n" +
			"Fired by a systemd timer. Polls the workspace clone's tags; on a newer\n" +
			"release tag than `current` it checks out, builds via the project\n" +
			"scripts, snapshots the artifacts into releases/<tag>/, verifies the\n" +
			"build streams (ws_probe on a scratch port), then atomically swaps the\n" +
			"`current` symlink and restarts the service. Rollback/retention are a\n" +
			"later phase; --dry-run stops after build+gate.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "root", Value: "/opt/imzero2", Usage: "deploy root; workspace/releases/current derive from it unless overridden"},
			&cli.StringFlag{Name: "workspace", Usage: "persistent git clone + build caches (default <root>/workspace)"},
			&cli.StringFlag{Name: "releases-dir", Usage: "immutable release snapshots (default <root>/releases)"},
			&cli.StringFlag{Name: "current", Usage: "the `current` symlink (default <root>/current)"},
			&cli.StringFlag{Name: "remote", Value: "origin", Usage: "git remote name in the workspace clone (read-only)"},
			&cli.StringFlag{Name: "service", Value: "imzero2-demo.service", Usage: "systemd unit restarted on swap"},
			&cli.IntFlag{Name: "scratch-port", Value: 18089, Usage: "loopback port the gate binds the candidate carrier on"},
			&cli.IntFlag{Name: "gate-aus", Value: 30, Usage: "access units ws_probe must receive for the gate to pass"},
			&cli.DurationFlag{Name: "gate-timeout", Value: 120 * time.Second, Usage: "overall gate budget"},
			&cli.IntFlag{Name: "live-port", Value: 8089, Usage: "the demo service's listen port (post-restart health-probe target)"},
			&cli.IntFlag{Name: "keep", Value: 5, Usage: "release dirs to retain for rollback history"},
			&cli.StringFlag{Name: "encoder-args", Value: "-c:v libx264 -preset veryfast -tune zerolatency -bf 0 -g 100000", Usage: "IMZERO2_HEADLESS_ENCODER_ARGS for the gate run"},
			&cli.StringFlag{Name: "main-font", Usage: "main font TTF (default: fc-match 'Noto Sans')"},
			&cli.StringFlag{Name: "phosphor-font", Usage: "phosphor font TTF (default: the release's bundled asset)"},
			&cli.StringFlag{Name: "fallback-font", Usage: "fallback font TTF (default: fc-match 'Noto Sans CJK JP')"},
			&cli.BoolFlag{Name: "dry-run", Usage: "build + gate but skip the swap + restart"},
		},
		Action: func(c *cli.Context) error {
			root := c.String("root")
			cfg := Config{
				Remote:       c.String("remote"),
				Workspace:    orDefault(c.String("workspace"), filepath.Join(root, "workspace")),
				ReleasesDir:  orDefault(c.String("releases-dir"), filepath.Join(root, "releases")),
				CurrentLink:  orDefault(c.String("current"), filepath.Join(root, "current")),
				ServiceName:  c.String("service"),
				ScratchPort:  c.Int("scratch-port"),
				GateAUs:      c.Int("gate-aus"),
				GateTimeout:  c.Duration("gate-timeout"),
				LivePort:     c.Int("live-port"),
				KeepReleases: c.Int("keep"),
				EncoderArgs:  c.String("encoder-args"),
				MainFont:     c.String("main-font"),
				PhosphorFont: c.String("phosphor-font"),
				FallbackFont: c.String("fallback-font"),
				DryRun:       c.Bool("dry-run"),
			}
			_, err := Run(c.Context, log.Logger, cfg)
			return err
		},
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

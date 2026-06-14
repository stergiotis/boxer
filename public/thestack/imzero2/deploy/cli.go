package deploy

import (
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
	"github.com/urfave/cli/v2"
)

const defaultEncoderArgs = "-c:v libx264 -preset veryfast -tune zerolatency -bf 0 -g 100000"

// NewCommand returns the `imzero2 deploy` subcommand (ADR-0085). Flag
// defaults come from the IMZERO2_DEPLOY_* env registry (SD7) — so systemd
// `Environment=` configures the tool and every knob self-documents in
// doc/env-vars.md — and a flag passed on the command line overrides the env.
func NewCommand() *cli.Command {
	return &cli.Command{
		Name:  "deploy",
		Usage: "pull a new release tag, build on-box, gate, and atomically deploy (ADR-0085)",
		Description: "On-box pull-build-and-atomic-deploy for the headless demo (ADR-0085).\n\n" +
			"Fired by a systemd timer. Polls the workspace clone's tags; on a newer\n" +
			"release tag than `current` it checks out, builds via the project\n" +
			"scripts, snapshots the artifacts into releases/<tag>/, verifies the\n" +
			"build streams (ws_probe on a scratch port), then atomically swaps the\n" +
			"`current` symlink and restarts the service. A release that fails to\n" +
			"serve after the swap is rolled back; old releases beyond --keep are\n" +
			"pruned. --dry-run stops after build+gate. Knobs default from the\n" +
			"IMZERO2_DEPLOY_* env vars (see doc/env-vars.md); flags override.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "root", Value: imzero2env.DeployRoot.Get(), Usage: "deploy root; workspace/releases/current derive from it unless overridden"},
			&cli.StringFlag{Name: "workspace", Value: imzero2env.DeployWorkspace.Get(), Usage: "persistent git clone + build caches (default <root>/workspace)"},
			&cli.StringFlag{Name: "releases-dir", Value: imzero2env.DeployReleasesDir.Get(), Usage: "immutable release snapshots (default <root>/releases)"},
			&cli.StringFlag{Name: "current", Value: imzero2env.DeployCurrent.Get(), Usage: "the `current` symlink (default <root>/current)"},
			&cli.StringFlag{Name: "remote", Value: imzero2env.DeployRemote.Get(), Usage: "git remote name in the workspace clone (read-only)"},
			&cli.StringFlag{Name: "service", Value: imzero2env.DeployService.Get(), Usage: "systemd unit restarted on swap"},
			&cli.IntFlag{Name: "scratch-port", Value: int(imzero2env.DeployScratchPort.Get()), Usage: "loopback port the gate binds the candidate carrier on"},
			&cli.IntFlag{Name: "live-port", Value: int(imzero2env.DeployLivePort.Get()), Usage: "the demo service's listen port (post-restart health-probe target)"},
			&cli.IntFlag{Name: "gate-aus", Value: int(imzero2env.DeployGateAUs.Get()), Usage: "access units ws_probe must receive for the gate to pass"},
			&cli.DurationFlag{Name: "gate-timeout", Value: imzero2env.DeployGateTimeout.Get(), Usage: "overall gate / health-probe budget"},
			&cli.IntFlag{Name: "keep", Value: int(imzero2env.DeployKeep.Get()), Usage: "release dirs to retain for rollback history"},
			&cli.StringFlag{Name: "encoder-args", Value: gateEncoderDefault(), Usage: "IMZERO2_HEADLESS_ENCODER_ARGS for the gate run; defaults to the live service's encoder, else x264"},
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
				LivePort:     c.Int("live-port"),
				GateAUs:      c.Int("gate-aus"),
				GateTimeout:  c.Duration("gate-timeout"),
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

// gateEncoderDefault reuses the live service's encoder (IMZERO2_HEADLESS_ENCODER_ARGS)
// for the gate run so the gate exercises the same path, falling back to x264.
func gateEncoderDefault() string {
	if v := imzero2env.HeadlessEncoderArgs.Get(); v != "" {
		return v
	}
	return defaultEncoderArgs
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

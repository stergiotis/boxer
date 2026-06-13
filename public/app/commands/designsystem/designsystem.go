// Package designsystem is the CLI wiring for the IDS toolchain
// (ADR-0029). It exposes the same command tree to both consumers:
//
//   - The standalone binary at cmd/designsystem (legacy entry
//     used by scripts/designcolors.sh and CI workflows) — invokes
//     [Subcommands] so the user types `designsystem colors gen` rather
//     than `designsystem designsystem colors gen`.
//   - The aggregated pebble.sh app (public/app/app.go) — invokes
//     [NewCliCommand] to register `designsystem` alongside the other
//     top-level commands (cbor, key, http, datasource, …).
//
// All implementations live under public/keelson/designsystem/
// (gen, vendor, ssim, tour); this package is purely the urfave/cli
// surface.
package designsystem

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/gen"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/vendor"
	"github.com/stergiotis/boxer/public/keelson/designsystem/review/ssim"
	"github.com/stergiotis/boxer/public/keelson/designsystem/review/tour"
)

// NewCliCommand returns the top-level `designsystem` command for the
// pebble.sh-aggregated app. Use [Subcommands] when wiring the
// standalone binary so the user types the subcommands directly.
func NewCliCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:        "designsystem",
		Usage:       "IDS toolchain — color generation, palette vendoring, screenshot review (ADR-0029)",
		Subcommands: Subcommands(),
	}
	return
}

// Subcommands returns the colors / review subcommand pair, suitable
// for the standalone binary's top-level App.Commands list.
func Subcommands() (cmds []*cli.Command) {
	cmds = []*cli.Command{
		newColorsCommand(),
		newReviewCommand(),
	}
	return
}

// ---- colors -------------------------------------------------------

func newColorsCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "colors",
		Usage: "IDS semantic-palette generator + scientific-palette vendoring",
		Subcommands: []*cli.Command{
			newColorsGenCommand(),
			newColorsVendorCommand(),
		},
	}
	return
}

func newColorsGenCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "gen",
		Usage: "regenerate IDS semantic palette artefacts (ADR-0033)",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "verify",
				Value: false,
				Usage: "re-emit to memory and byte-compare against committed palette_generated.{rs,go}; non-zero exit on drift",
			},
		},
		Action: func(ctx *cli.Context) (err error) {
			res, err := gen.Run(ctx.Context, gen.Config{Verify: ctx.Bool("verify")})
			if err != nil {
				err = eh.Errorf("colors gen failed: %w", err)
				return
			}
			if ctx.Bool("verify") {
				fmt.Printf("designsystem colors gen: verify ok (%d tokens, %d pairs)\n",
					res.TokenCount, res.PairCount)
			} else {
				for _, p := range res.Wrote {
					fmt.Printf("wrote %s\n", p)
				}
				fmt.Printf("designsystem colors gen: %d tokens, %d pairs (APCA gate), %d WCAG warnings, %d collisions, %d CVD warnings\n",
					res.TokenCount, res.PairCount, len(res.WCAGWarnings), res.Collisions, len(res.CVDWarnings))
			}
			if len(res.APCAFailures) > 0 {
				fmt.Fprintln(os.Stderr, "FAIL: APCA Lc threshold violations (primary contrast gate):")
				for _, f := range res.APCAFailures {
					fmt.Fprintln(os.Stderr, "  "+f)
				}
				err = cli.Exit("APCA Lc threshold violations", 2)
				return
			}
			return
		},
	}
	return
}

func newColorsVendorCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "vendor",
		Usage: "re-vendor Crameri / viridis data-encoding LUTs (ADR-0033 §SD4)",
		Action: func(ctx *cli.Context) (err error) {
			res, err := vendor.Run(ctx.Context, vendor.Config{})
			if err != nil {
				err = eh.Errorf("colors vendor failed: %w", err)
				return
			}
			for _, n := range res.Names {
				fmt.Printf("vendored %s\n", n)
			}
			fmt.Printf("designsystem colors vendor: %d palettes\n", res.Total)
			return
		},
	}
	return
}

// ---- review -------------------------------------------------------

func newReviewCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "review",
		Usage: "Tier 2 deterministic pre-filter and (future) LLM-rubric driver (ADR-0029 §SD9)",
		Subcommands: []*cli.Command{
			newReviewSsimCommand(),
			newReviewTourCommand(),
		},
	}
	return
}

func newReviewSsimCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:      "ssim",
		Usage:     "compute SSIM(A, B); exit 1 if below --threshold",
		ArgsUsage: "BASELINE CANDIDATE",
		Flags: []cli.Flag{
			&cli.Float64Flag{
				Name:  "threshold",
				Value: ssim.ImperceptibleThreshold,
				Usage: "SSIM floor — exit 0 if computed SSIM ≥ threshold, else exit 1",
			},
			&cli.IntFlag{
				Name:  "window",
				Value: ssim.DefaultWindow,
				Usage: "SSIM window size (K × K); 0 uses package default",
			},
			&cli.BoolFlag{
				Name:  "quiet",
				Value: false,
				Usage: "suppress all stdout output",
			},
		},
		Action: func(ctx *cli.Context) (err error) {
			if ctx.NArg() != 2 {
				err = cli.Exit("usage: designsystem review ssim BASELINE CANDIDATE [flags]", 2)
				return
			}
			a, err := loadImage(ctx.Args().Get(0))
			if err != nil {
				err = cli.Exit(err.Error(), 2)
				return
			}
			b, err := loadImage(ctx.Args().Get(1))
			if err != nil {
				err = cli.Exit(err.Error(), 2)
				return
			}
			window := ctx.Int("window")
			threshold := ctx.Float64("threshold")
			s, err := ssim.Compute(a, b, window)
			if err != nil {
				err = cli.Exit(err.Error(), 2)
				return
			}
			d := (1.0 - s) / 2.0
			verdict := "skip-llm"
			exitCode := 0
			if s < threshold {
				verdict = "llm-grade-warranted"
				exitCode = 1
			}
			if !ctx.Bool("quiet") {
				fmt.Printf("ssim=%.6f dssim=%.6f threshold=%.6f verdict=%s\n",
					s, d, threshold, verdict)
			}
			if exitCode != 0 {
				err = cli.Exit("", exitCode)
			}
			return
		},
	}
	return
}

func newReviewTourCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:      "tour",
		Usage:     "walk two tour PNG dirs; per-scene SSIM table + summary",
		ArgsUsage: "BASELINE_DIR CANDIDATE_DIR",
		Description: "Pairs *.png files by basename across BASELINE_DIR and CANDIDATE_DIR,\n" +
			"computes per-pair SSIM, and prints a sorted table + aggregate\n" +
			"summary. SSIM is never itself a build gate (ADR-0029 §SD9); the\n" +
			"default exit is 0. Pass --gate-below=N to opt into catastrophic-\n" +
			"regression detection: exit 1 if any pair's SSIM falls below N.\n" +
			"Missing-candidate / dim-mismatch outcomes always exit 1 because\n" +
			"they signal a broken capture rather than a perceptual drift.",
		Flags: []cli.Flag{
			&cli.Float64Flag{
				Name:  "threshold",
				Value: ssim.ImperceptibleThreshold,
				Usage: "SSIM floor for skip-llm vs llm-grade-warranted bucketing",
			},
			&cli.Float64Flag{
				Name:  "gate-below",
				Value: 0.0,
				Usage: "if > 0, exit 1 when any scene SSIM falls below this value (catastrophic-regression gate)",
			},
			&cli.IntFlag{
				Name:  "window",
				Value: ssim.DefaultWindow,
				Usage: "SSIM window size (K × K); 0 uses package default",
			},
			&cli.BoolFlag{
				Name:  "quiet",
				Value: false,
				Usage: "suppress per-scene rows; print only the summary line",
			},
		},
		Action: func(ctx *cli.Context) (err error) {
			if ctx.NArg() != 2 {
				err = cli.Exit("usage: designsystem review tour BASELINE_DIR CANDIDATE_DIR [flags]", 2)
				return
			}
			baselineDir := ctx.Args().Get(0)
			candidateDir := ctx.Args().Get(1)
			threshold := ctx.Float64("threshold")
			gateBelow := ctx.Float64("gate-below")
			window := ctx.Int("window")

			res, err := tour.Compare(ctx.Context, baselineDir, candidateDir, tour.Config{
				Window:    window,
				Threshold: threshold,
			})
			if err != nil {
				err = cli.Exit(err.Error(), 2)
				return
			}

			printTourResult(res, baselineDir, candidateDir, threshold, gateBelow, window, ctx.Bool("quiet"))

			gated := false
			if gateBelow > 0 {
				for _, o := range res.Outcomes {
					if o.Status == tour.StatusOK && o.SSIM < gateBelow {
						gated = true
						break
					}
				}
			}
			missingFail := res.Summary.MissingCandidate > 0 || res.Summary.OpErrors > 0
			if gated || missingFail {
				err = cli.Exit("", 1)
				return
			}
			return
		},
	}
	return
}

// ---- shared helpers -----------------------------------------------

func loadImage(path string) (img image.Image, err error) {
	f, err := os.Open(path)
	if err != nil {
		err = eh.Errorf("open %s: %w", path, err)
		return
	}
	defer f.Close()
	img, _, err = image.Decode(f)
	if err != nil {
		err = eh.Errorf("decode %s: %w", path, err)
	}
	return
}

func printTourResult(res tour.Result, baselineDir, candidateDir string, threshold, gateBelow float64, window int, quiet bool) {
	if !quiet {
		fmt.Printf("%-36s  %-9s  %-9s  %s\n", "SCENE", "SSIM", "DSSIM", "VERDICT")
		for _, o := range res.Outcomes {
			ssimCol := "-"
			dssimCol := "-"
			verdict := o.Status.String()
			if o.Status == tour.StatusOK {
				ssimCol = fmt.Sprintf("%.6f", o.SSIM)
				dssimCol = fmt.Sprintf("%.6f", o.DSSIM)
				if o.SSIM >= threshold {
					verdict = "skip-llm"
				} else {
					verdict = "llm-grade-warranted"
				}
			}
			line := fmt.Sprintf("%-36s  %-9s  %-9s  %s", o.Scene, ssimCol, dssimCol, verdict)
			if o.Err != nil {
				line = line + "  (" + o.Err.Error() + ")"
			}
			fmt.Println(line)
		}
	}

	gateVerdict := "off"
	if gateBelow > 0 {
		gateVerdict = "pass"
		for _, o := range res.Outcomes {
			if o.Status == tour.StatusOK && o.SSIM < gateBelow {
				gateVerdict = "FAIL"
				break
			}
		}
	}

	fmt.Printf("\nreview tour summary:\n")
	fmt.Printf("  baseline=%s candidate=%s threshold=%.3f window=%d\n",
		baselineDir, candidateDir, threshold, window)
	fmt.Printf("  total=%d skip-llm=%d llm-grade-warranted=%d missing-candidate=%d missing-baseline=%d op-errors=%d\n",
		res.Summary.Total, res.Summary.SkipLLM, res.Summary.LLMGradeWarranted,
		res.Summary.MissingCandidate, res.Summary.MissingBaseline, res.Summary.OpErrors)
	if res.Summary.LowestScene != "" {
		fmt.Printf("  lowest-ssim: %s (%.6f)\n", res.Summary.LowestScene, res.Summary.LowestSSIM)
	}
	if gateBelow > 0 {
		fmt.Printf("  gate-below=%.3f → %s\n", gateBelow, gateVerdict)
	} else {
		fmt.Printf("  gate-below=off (triage-only run)\n")
	}
}


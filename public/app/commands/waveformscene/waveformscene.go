// Package waveformscene is the geometry scripts/dev/waveform-scene.sh aims its
// synthetic pointer with — the headless scene ADR-0208's verification plan
// asks for over the waveform player.
//
// The canvas is painter-only and has no accessibility node, so the scene
// locates it indirectly: it dumps the tree, reads the button row above the
// canvas and the readout line below it, and aims between them. Landing a press
// *inside a region* needs one step more, because the demo's regions are tone
// bursts on a fixed cadence and the pointer has to arrive at one.
//
// Both verbs take the coordinates the scene read out of the dump and print the
// point to aim at, so the arithmetic and the demo's cadence live here rather
// than in the trace the scene writes.
package waveformscene

import (
	"fmt"
	"math"

	cli "github.com/urfave/cli/v2"
)

// The synthetic track the demo builds: a 0.35 s tone burst every 0.9 s. The
// press lands a little past the start of a burst rather than on its edge, so
// a pixel of rounding cannot put it in the silence before.
const (
	burstPeriod     = 0.9
	burstEntry      = 0.17
	defaultSecPerPx = 0.010
)

// NewCliCommand returns the `waveform-scene` subcommand. Mounted under boxer's
// `dev` parent by public/app/main.go.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "waveform-scene",
		Usage: "aim the waveform player scene's pointer (ADR-0208 verification plan)",
		Subcommands: []*cli.Command{
			newCanvasPointCommand(),
			newRegionPressCommand(),
		},
	}
}

func newCanvasPointCommand() *cli.Command {
	return &cli.Command{
		Name:  "canvas-point",
		Usage: "print the canvas midpoint between the button row and the first readout line",
		Flags: []cli.Flag{
			&cli.Float64Flag{Name: "button-x", Usage: "centre x of a button in the row above the canvas", Required: true},
			&cli.Float64Flag{Name: "button-y", Usage: "centre y of that button", Required: true},
			&cli.Float64Flag{Name: "readout-y", Usage: "centre y of the first readout line below the canvas", Required: true},
		},
		Action: func(ctx *cli.Context) error {
			// The midpoint is inside the canvas strip whatever the row
			// heights around it turn out to be.
			x := roundHalfEven(ctx.Float64("button-x"))
			y := roundHalfEven((ctx.Float64("button-y") + ctx.Float64("readout-y")) / 2)
			fmt.Printf("%d %d\n", x, y)
			return nil
		},
	}
}

func newRegionPressCommand() *cli.Command {
	return &cli.Command{
		Name:  "region-press",
		Usage: "print the x to press at so the pointer lands inside the next tone burst",
		Flags: []cli.Flag{
			&cli.Float64Flag{Name: "canvas-x", Usage: "x the hover readout was taken at", Required: true},
			&cli.Float64Flag{Name: "hover-min", Usage: "minutes of the time under the pointer", Required: true},
			&cli.Float64Flag{Name: "hover-sec", Usage: "seconds of the time under the pointer", Required: true},
			&cli.Float64Flag{Name: "seconds-per-px", Value: defaultSecPerPx, Usage: "the zoom the scene set before measuring"},
		},
		Action: func(ctx *cli.Context) error {
			at := ctx.Float64("hover-min")*60 + ctx.Float64("hover-sec")
			// The next burst start at or after the pointer, entered a little
			// way in.
			target := math.Ceil(at/burstPeriod)*burstPeriod + burstEntry
			x := roundHalfEven(ctx.Float64("canvas-x") + (target-at)/ctx.Float64("seconds-per-px"))
			fmt.Printf("%d\n", x)
			return nil
		},
	}
}

// roundHalfEven ties to even, because the coordinates it rounds are centres of
// laid-out rows and land on a half pixel often enough for the tie rule to
// decide which pixel the press reaches.
func roundHalfEven(v float64) int { return int(math.RoundToEven(v)) }

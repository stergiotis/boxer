// Package portolancam is the camera oracle for
// scripts/dev/portolan-map-scene.sh, the ADR-0204 §M5 regression net over the
// portolan map widget.
//
// The map reads its input one frame behind the host from the painter lane's
// registers, and the failures worth a scene are the ones a capture looks right
// through: a drag that lands short, a wheel notch that zooms about the wrong
// point, arrows that do nothing because focus was surrendered at the click. So
// the scene asserts the *camera* the demo reads back — centre, zoom, the
// canvas rect, the tile pipeline — rather than pixels.
//
// `read` turns one accessibility-tree dump into a reading on stdout; `at`
// turns a canvas-relative point into the screen point a gesture is aimed at;
// the remaining verbs compare two readings against what a gesture must have
// done, in web-mercator pixels at the zoom of the first reading. Each check
// prints its measurement on stdout whether it passes or fails — the scene logs
// that line either way — and exits non-zero when the gesture missed.
package portolancam

import (
	"encoding/json/v2"
	"fmt"
	"math"

	cli "github.com/urfave/cli/v2"
)

// zoomEqual is the tolerance below which two readings are the same zoom level.
// The demo prints two decimals, and an animated pan settles a few thousandths
// short of its target.
const zoomEqual = 0.006

// The first view the demo opens on, which `baseline` pins before any gesture
// runs: Wrocław at zoom 12, in a canvas the scene's default window size makes
// 720 × 460 px.
const (
	baselineLat  = 51.0992
	baselineLon  = 17.0366
	baselineZoom = 12
	baselineW    = 720
	baselineH    = 460
)

// minTilesLoaded is the tile count below which the pipeline cannot have
// covered the canvas, so a "no errors" verdict would be vacuous.
const minTilesLoaded = 12

// NewCliCommand returns the `portolan-cam` subcommand. Mounted under boxer's
// `dev` parent by public/app/main.go.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:      "portolan-cam",
		Usage:     "read and check the portolan demo's camera (ADR-0204 §M5 scene oracle)",
		ArgsUsage: "see each subcommand",
		Subcommands: []*cli.Command{
			newReadCommand(),
			newAtCommand(),
			newBaselineCommand(),
			newDragCommand(),
			newWheelCommand(),
			newDblClickCommand(),
			newKeyCommand(),
			newPipelineCommand(),
		},
	}
}

func newReadCommand() *cli.Command {
	return &cli.Command{
		Name:      "read",
		Usage:     "parse an accessibility-tree dump into one reading on stdout",
		ArgsUsage: "TREE.jsonl",
		Action: func(ctx *cli.Context) (err error) {
			path, err := oneArg(ctx)
			if err != nil {
				return
			}
			r, seen, err := ReadTree(path)
			if err != nil {
				return
			}
			if missing, ok := seen.Complete(); !ok {
				// A verdict about the demo, not a fault in this command: the
				// scene reports the dump, so say which line was absent and
				// exit rather than raising a stack over it.
				return cli.Exit("the tree dump carries no "+missing+" readout: "+path, 1)
			}
			b, err := json.Marshal(r)
			if err != nil {
				return
			}
			fmt.Printf("%s\n", b)
			return
		},
	}
}

func newAtCommand() *cli.Command {
	return &cli.Command{
		Name:      "at",
		Usage:     "print the screen point of a canvas-relative offset in a reading",
		ArgsUsage: "READING.json",
		Flags: []cli.Flag{
			&cli.Float64Flag{Name: "dx", Usage: "offset from the canvas origin, px", Required: true},
			&cli.Float64Flag{Name: "dy", Usage: "offset from the canvas origin, px", Required: true},
		},
		Action: func(ctx *cli.Context) (err error) {
			path, err := oneArg(ctx)
			if err != nil {
				return
			}
			r, err := LoadReadout(path)
			if err != nil {
				return
			}
			// Truncating rather than rounding: the driver wants a point
			// inside the canvas, and the rect is given in whole pixels.
			fmt.Printf("%d %d\n", int(r.OX+ctx.Float64("dx")), int(r.OY+ctx.Float64("dy")))
			return
		},
	}
}

func newBaselineCommand() *cli.Command {
	return &cli.Command{
		Name:      "baseline",
		Usage:     "check the first view: the demo's opening centre, zoom and canvas, every tile landed",
		ArgsUsage: "READING.json",
		Action: func(ctx *cli.Context) (err error) {
			path, err := oneArg(ctx)
			if err != nil {
				return
			}
			a, err := LoadReadout(path)
			if err != nil {
				return
			}
			ok := math.Abs(a.Lat-baselineLat) < 1e-4 && math.Abs(a.Lon-baselineLon) < 1e-4 &&
				math.Abs(a.Zoom-baselineZoom) < zoomEqual &&
				a.W == baselineW && a.H == baselineH && a.Loading == "false" && a.Errors == 0
			return verdict(ok, "baseline: centre %.5f,%.5f zoom %.2f canvas %dx%d at %.0f,%.0f errors %d",
				a.Lat, a.Lon, a.Zoom, a.W, a.H, a.OX, a.OY, a.Errors)
		},
	}
}

func newDragCommand() *cli.Command {
	return &cli.Command{
		Name:      "drag",
		Usage:     "check that dragging the map by (dx,dy) px moved the centre by exactly the opposite",
		ArgsUsage: "BEFORE.json AFTER.json",
		Flags: []cli.Flag{
			&cli.Float64Flag{Name: "dx", Usage: "pointer travel, px", Required: true},
			&cli.Float64Flag{Name: "dy", Usage: "pointer travel, px", Required: true},
			&cli.Float64Flag{Name: "tol", Usage: "allowed slack, px", Required: true},
		},
		Action: func(ctx *cli.Context) (err error) {
			a, b, err := twoReadings(ctx)
			if err != nil {
				return
			}
			dx, dy, tol := ctx.Float64("dx"), ctx.Float64("dy"), ctx.Float64("tol")
			mx, my := centreShift(a, b)
			// Dragging the map by (+dx,+dy) moves the centre by (-dx,-dy).
			ok := math.Abs(mx+dx) <= tol && math.Abs(my+dy) <= tol && math.Abs(a.Zoom-b.Zoom) < zoomEqual
			return verdict(ok, "drag: centre moved %.2f,%.2f px (expected %.0f,%.0f ± %.0f; inertia on a slow drag is a pixel or two)",
				mx, my, -dx, -dy, tol)
		},
	}
}

func newWheelCommand() *cli.Command {
	return &cli.Command{
		Name:      "wheel",
		Usage:     "check that one wheel notch zoomed into the band, about the canvas centre",
		ArgsUsage: "BEFORE.json AFTER.json",
		Flags: []cli.Flag{
			&cli.Float64Flag{Name: "lo", Usage: "lowest accepted zoom delta, levels", Required: true},
			&cli.Float64Flag{Name: "hi", Usage: "highest accepted zoom delta, levels", Required: true},
			&cli.Float64Flag{Name: "tol", Usage: "allowed centre drift, px", Required: true},
		},
		Action: func(ctx *cli.Context) (err error) {
			a, b, err := twoReadings(ctx)
			if err != nil {
				return
			}
			lo, hi, tol := ctx.Float64("lo"), ctx.Float64("hi"), ctx.Float64("tol")
			dz := b.Zoom - a.Zoom
			mx, my := centreShift(a, b)
			ok := lo <= dz && dz <= hi && math.Abs(mx) <= tol && math.Abs(my) <= tol
			return verdict(ok, "wheel: zoom %+.2f (expected %.2f..%.2f), centre moved %.2f,%.2f px (within %.0f: the notch is about the centre)",
				dz, lo, hi, mx, my, tol)
		},
	}
}

func newDblClickCommand() *cli.Command {
	return &cli.Command{
		Name:      "dblclick",
		Usage:     "check that a double click zoomed one level, anchored at the canvas centre",
		ArgsUsage: "BEFORE.json AFTER.json",
		Flags: []cli.Flag{
			&cli.Float64Flag{Name: "tol", Usage: "allowed centre drift, px", Required: true},
		},
		Action: func(ctx *cli.Context) (err error) {
			a, b, err := twoReadings(ctx)
			if err != nil {
				return
			}
			tol := ctx.Float64("tol")
			dz := b.Zoom - a.Zoom
			mx, my := centreShift(a, b)
			// A shade wider than zoomEqual: the level is reached by an
			// animation the scene samples once it has settled.
			ok := math.Abs(dz-1) < 0.011 && math.Abs(mx) <= tol && math.Abs(my) <= tol
			return verdict(ok, "double click: zoom %+.2f (expected +1.00), centre moved %.2f,%.2f px (within %.0f: anchored at the centre)",
				dz, mx, my, tol)
		},
	}
}

func newKeyCommand() *cli.Command {
	return &cli.Command{
		Name:      "key",
		Usage:     "check that an arrow key panned by its fixed step and nothing else",
		ArgsUsage: "BEFORE.json AFTER.json",
		Flags: []cli.Flag{
			&cli.Float64Flag{Name: "dx", Usage: "expected centre travel, px", Required: true},
			&cli.Float64Flag{Name: "tol", Usage: "allowed slack, px", Required: true},
		},
		Action: func(ctx *cli.Context) (err error) {
			a, b, err := twoReadings(ctx)
			if err != nil {
				return
			}
			dx, tol := ctx.Float64("dx"), ctx.Float64("tol")
			mx, my := centreShift(a, b)
			ok := math.Abs(mx-dx) <= tol && math.Abs(my) <= tol && math.Abs(a.Zoom-b.Zoom) < zoomEqual
			return verdict(ok, "ArrowRight: centre moved %.2f,%.2f px (expected %.0f,0 within %.1f)", mx, my, dx, tol)
		},
	}
}

func newPipelineCommand() *cli.Command {
	return &cli.Command{
		Name:      "pipeline",
		Usage:     "check the tile pipeline of a reading: no errors, no re-ships, the canvas covered",
		ArgsUsage: "READING.json",
		Action: func(ctx *cli.Context) (err error) {
			path, err := oneArg(ctx)
			if err != nil {
				return
			}
			b, err := LoadReadout(path)
			if err != nil {
				return
			}
			ok := b.Errors == 0 && b.Reships == 0 && b.Loading == "false" && b.Loaded >= minTilesLoaded
			return verdict(ok, "tiles: %d requested, %d loaded, %d errors, re-ships %d, loading %s",
				b.Requested, b.Loaded, b.Errors, b.Reships, b.Loading)
		},
	}
}

// verdict prints the measurement the scene logs either way, then turns a
// failed check into a bare non-zero exit — the line has already been said, so
// a second rendering of it as an error would be noise on the scene's stderr.
func verdict(ok bool, format string, args ...any) error {
	fmt.Printf(format+"\n", args...)
	if ok {
		return nil
	}
	return cli.Exit("", 1)
}

func oneArg(ctx *cli.Context) (path string, err error) {
	if ctx.NArg() != 1 {
		err = cli.Exit("usage: "+ctx.Command.HelpName+" "+ctx.Command.ArgsUsage, 2)
		return
	}
	path = ctx.Args().Get(0)
	return
}

func twoReadings(ctx *cli.Context) (a Readout, b Readout, err error) {
	if ctx.NArg() != 2 {
		err = cli.Exit("usage: "+ctx.Command.HelpName+" "+ctx.Command.ArgsUsage, 2)
		return
	}
	if a, err = LoadReadout(ctx.Args().Get(0)); err != nil {
		return
	}
	b, err = LoadReadout(ctx.Args().Get(1))
	return
}

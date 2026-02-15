//go:build llm_generated_gemini3pro

package finddivisions

import (
	"fmt"
	"image/png"
	"math"
	"math/rand"
	"os"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
	"golang.org/x/image/font/gofont/goregular"
)

type TestCase struct {
	Name         string
	Min          float64
	Max          float64
	DesiredTicks int
}

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "numerical",
		Subcommands: []*cli.Command{
			{
				Name: "find-divisions",
				Subcommands: []*cli.Command{
					{
						Name: "generate",
						Flags: []cli.Flag{
							&cli.Float64Flag{
								Name:     "fontSize",
								Value:    12.0,
								Category: "drawing",
							},
							&cli.Float64Flag{
								Name:     "dpi",
								Value:    96.0,
								Category: "drawing",
							},
							&cli.BoolFlag{
								Name:     "onlyLoose",
								Category: "algorithm",
							},
							&cli.BoolFlag{
								Name:     "fastMode",
								Category: "algorithm",
							},
							&cli.Float64Flag{
								Name:     "axisWidth",
								Value:    400.0,
								Category: "harness",
							},
							&cli.IntFlag{
								Name:     "rowHeight",
								Value:    150.0,
								Category: "harness",
							},
							&cli.IntFlag{
								Name:     "canvasWidth",
								Value:    600,
								Category: "harness",
							},
							&cli.Float64Flag{
								Name:     "min",
								Category: "data",
							},
							&cli.Float64Flag{
								Name:     "max",
								Category: "data",
							},
							&cli.IntFlag{
								Name:     "desiredTicks",
								Category: "data",
							},
						},
						Action: func(context *cli.Context) error {
							fontSize := context.Float64("fontSize")
							dpi := context.Float64("dpi")
							axisWidth := context.Float64("axisWidth")
							rowHeight := context.Int("rowHeight")
							W := context.Int("canvasWidth")

							scorer, err := NewExhaustiveScorer(goregular.TTF, fontSize, dpi, axisWidth, true)
							if err != nil {
								return eh.Errorf("unable to create scorer: %w", err)
							}

							// 2. Define Test Cases
							cases := []TestCase{
								{"Heckbert Standard", 8.1, 14.1, 4},
								{"Zero Crossing", -10, 10, 5},
								{"Tiny Range", 0.0, 0.1, 5},
								{"Scientific", 0, 10000000, 5},
								{"Offset Start", 0.12, 0.18, 4},
								{"Loose Constraint", 98, 452, 6},
								{"Negative Offset", -0.9, -0.1, 4},
								// A case designed to trigger overlap if not handled:
								{"Overlap Risk", 100000, 100005, 10},
							}
							if context.IsSet("min") {
								cases = append(cases[:0],
									TestCase{
										Name:         "cli",
										Min:          context.Float64("min"),
										Max:          context.Float64("max"),
										DesiredTicks: context.Int("desiredTicks"),
									})
							}

							// 3. Prepare Canvas
							// Height = (Title + 2 * AxisHeight) * Count
							H := len(cases) * rowHeight
							dc := gg.NewContext(W, H)

							// Set white background
							dc.SetRGB(1, 1, 1)
							dc.Clear()

							// Load font for drawing
							var font *truetype.Font
							font, err = truetype.Parse(goregular.TTF)
							if err != nil {
								return eh.Errorf("unable to parse goregular ttf: %w", err)
							}
							fontFace := truetype.NewFace(font, &truetype.Options{
								Size:              fontSize,
								DPI:               dpi,
								Hinting:           0,
								GlyphCacheEntries: 0,
								SubPixelsX:        0,
								SubPixelsY:        0,
							})
							dc.SetFontFace(fontFace)
							opts := TalbotOptions{
								Weights:   DefaultWeights,
								OnlyLoose: context.Bool("onlyLoose"),
								FastMode:  context.Bool("fastMode"),
								Qs:        nil,
							}

							// 4. Render Loop
							for i, tc := range cases {
								offsetY := float64(i * rowHeight)

								// Run Algorithm
								res := Talbot(tc.Min, tc.Max, tc.DesiredTicks, opts, scorer)

								// Draw Title
								dc.SetRGB(0, 0, 0)
								dc.DrawStringAnchored(fmt.Sprintf("%s (Request: %d, Score: %.2f)", tc.Name, tc.DesiredTicks, res.Score), 10, offsetY+20, 0, 0)

								// Draw The Axis
								drawAxisVisual(dc, 50, offsetY+80, axisWidth, tc, res)
							}

							return png.Encode(os.Stdout, dc.Image())
						},
					},
				},
			},
		},
	}
}

func drawAxisVisual(dc *gg.Context, x, y, width float64, tc TestCase, res TalbotResult) {
	// Determine the "World View"
	// We want to show slightly more than the ticks to see margins
	viewMin := math.Min(tc.Min, res.Min)
	viewMax := math.Max(tc.Max, res.Max)
	rangeVal := viewMax - viewMin

	// Add 5% padding visually
	viewMin -= rangeVal * 0.05
	viewMax += rangeVal * 0.05
	rangeVal = viewMax - viewMin
	scale := width / rangeVal

	mapX := func(val float64) float64 {
		return x + (val-viewMin)*scale
	}

	// 1. Draw Data Range (Blue Bar)
	// This shows where the actual data lives
	dc.SetRGBA(0, 0, 1, 0.2) // Blue transparent
	dataLeft := mapX(tc.Min)
	dataRight := mapX(tc.Max)
	dc.DrawRectangle(dataLeft, y-10, dataRight-dataLeft, 20)
	dc.Fill()

	// 2. Draw Axis Line (Black)
	dc.SetRGB(0, 0, 0)
	dc.DrawLine(x, y, x+width, y)
	dc.Stroke()

	// 3. Draw Ticks and Labels
	for i, tickVal := range res.Ticks {
		screenX := mapX(tickVal)

		// Tick Mark
		dc.DrawLine(screenX, y, screenX, y+5)
		dc.Stroke()

		// Label
		// Use the label string generated by the algorithm (res.Labels)
		labelStr := res.Labels[i]

		// Measure text to center it
		w, _ := dc.MeasureString(labelStr)

		dc.DrawString(labelStr, screenX-(w/2), y+20)
	}

	// 4. Draw Data Bounds markers (Red dots)
	dc.SetRGB(1, 0, 0)
	dc.DrawCircle(dataLeft, y, 2)
	dc.DrawCircle(dataRight, y, 2)
	dc.Fill()

	// Add some random data points to visualize density
	rng := rand.New(rand.NewSource(99))
	dc.SetRGBA(0, 0.5, 0, 0.5) // Green dots
	for k := 0; k < 20; k++ {
		val := tc.Min + rng.Float64()*(tc.Max-tc.Min)
		px := mapX(val)
		// Jitter Y slightly
		py := y - 5 - rng.Float64()*10
		dc.DrawCircle(px, py, 1.5)
		dc.Fill()
	}
}

//go:build llm_generated_gemini3pro

package finddivisions

import (
	"bytes"
	"fmt"
	"image/png"
	"math"
	"math/rand"
	"os"

	"github.com/fogleman/gg"
	"github.com/go-text/typesetting/font"
	"github.com/golang/freetype/truetype"
	"github.com/stergiotis/boxer/public/containers/ragged"
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
							&cli.BoolFlag{
								Name:     "nonuniformDecimals",
								Category: "scorer",
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
							&cli.IntFlag{
								Name:     "measurerCacheSize",
								Category: "measurer",
								Value:    4096,
							},
							&cli.BoolFlag{
								Name: "log",
							},
						},
						Action: func(context *cli.Context) error {
							fontSize := context.Float64("fontSize")
							dpi := context.Float64("dpi")
							axisWidth := context.Float64("axisWidth")
							rowHeight := context.Int("rowHeight")
							W := context.Int("canvasWidth")
							axisStartX := 50.0

							face, err := font.ParseTTF(bytes.NewReader(goregular.TTF))
							if err != nil {
								return eh.Errorf("unable to parse ttf: %w", err)
							}
							textMeasurer := NewTextMeasurerGoHarfbuzz(face)
							cachingMeasurer := NewCachingTextMeasurer(textMeasurer, context.Int("measurerCacheSize"))
							scorer := NewExhaustiveScorer(fontSize, dpi, axisWidth, !context.Bool("nonuniformDecimals"), cachingMeasurer)

							log := context.Bool("log")

							opts := TalbotOptions{
								Weights:   DefaultWeights,
								OnlyLoose: context.Bool("onlyLoose"),
								FastMode:  context.Bool("fastMode"),
								Qs:        nil,
							}

							var ttfFont *truetype.Font
							ttfFont, err = truetype.Parse(goregular.TTF)
							if err != nil {
								return eh.Errorf("unable to parse goregular ttf: %w", err)
							}
							fontFace := truetype.NewFace(ttfFont, &truetype.Options{
								Size:              fontSize,
								DPI:               dpi,
								Hinting:           0,
								GlyphCacheEntries: 0,
								SubPixelsX:        0,
								SubPixelsY:        0,
							})
							var dc *gg.Context
							prepareDc := func(nCases int) {
								// Height = (Title + 2 * AxisHeight) * Count
								H := nCases * rowHeight

								dc = gg.NewContext(W, H)
								// Set white background
								dc.SetRGB(1, 1, 1)
								dc.Clear()
								dc.SetFontFace(fontFace)
							}

							if log {
								// Define Test Cases
								cases := []TestCase{
									{"Standard Decades", 1, 1000, 5}, // 1, 10, 100, 1000
									{"Small Range", 10, 50, 4},       // Should trigger sub-decade logic
									{"Tiny Numbers", 0.0001, 0.1, 5}, // 10^-4 ... 10^-1
									{"Offset Data", 15, 4500, 5},     // 10, 100, 1000, 10000
									{"Large Range", 1e-5, 1e5, 10},   // Many decades
									{"Close to Power", 90, 1100, 4},  // 90 is close to 100
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
								prepareDc(len(cases))

								for i, tc := range cases {
									offsetY := float64(i * rowHeight)

									var res LogResult
									res, err = TalbotLogarithmic(tc.Min, tc.Max, tc.DesiredTicks, opts, scorer)
									if err != nil {
										fmt.Printf("Error in %s: %v\n", tc.Name, err)
										continue
									}

									// 1. Title
									dc.SetRGB(0, 0, 0)
									dc.DrawStringAnchored(fmt.Sprintf("%s (Range: %g - %g)", tc.Name, tc.Min, tc.Max), 10, offsetY+20, 0, 0)

									// 2. Draw Axis
									drawLogAxis(dc, axisStartX, offsetY+80, axisWidth, tc, res)
								}
							} else {
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
								prepareDc(3 * len(cases))

								for i, tc := range cases {
									offsetY := float64(i * rowHeight)

									cachingMeasurer.Reset()
									// Run Algorithm
									res := Talbot(tc.Min, tc.Max, tc.DesiredTicks, opts, scorer)

									// Draw Title
									dc.SetRGB(0, 0, 0)
									dc.DrawStringAnchored(fmt.Sprintf("%s %s (Request: %d, Score: %.2f, (Cache Hits: %d, Misses: %d))", res.Algorithm, tc.Name, tc.DesiredTicks, res.Score, cachingMeasurer.Hits, cachingMeasurer.Misses), 10, offsetY+20, 0, 0)

									// Draw The Axis
									drawAxisVisual(dc, axisStartX, offsetY+80, axisWidth, tc, res)
								}
								for i, tc := range cases {
									offsetY := float64((i + len(cases)) * rowHeight)

									// Run Algorithm
									res := Nelder(tc.Min, tc.Max, tc.DesiredTicks, NelderDefaultQs)

									// Draw Title
									dc.SetRGB(0, 0, 0)
									dc.DrawStringAnchored(fmt.Sprintf("%s %s (Request: %d, Score: %.2f)", res.Algorithm, tc.Name, tc.DesiredTicks, res.Score), 10, offsetY+20, 0, 0)

									// Draw The Axis
									drawAxisVisual(dc, axisStartX, offsetY+80, axisWidth, tc, res)
								}
								for i, tc := range cases {
									offsetY := float64((i + 2*len(cases)) * rowHeight)

									// Run Algorithm
									var res AxisLayout
									res, err = Heckbert(tc.Min, tc.Max, tc.DesiredTicks)
									if err != nil {
										return eh.Errorf("unable to apply heckbert algorithm: %w", err)
									}

									// Draw Title
									dc.SetRGB(0, 0, 0)
									dc.DrawStringAnchored(fmt.Sprintf("%s %s (Request: %d, Score: %.2f)", res.Algorithm, tc.Name, tc.DesiredTicks, res.Score), 10, offsetY+20, 0, 0)

									// Draw The Axis
									drawAxisVisual(dc, axisStartX, offsetY+80, axisWidth, tc, res)
								}
							}

							return png.Encode(os.Stdout, dc.Image())
						},
					},
				},
			},
		},
	}
}

func drawAxisVisual(dc *gg.Context, x, y, width float64, tc TestCase, res AxisLayout) {
	// Determine the "World View"
	// We want to show slightly more than the ticks to see margins
	viewMin := math.Min(tc.Min, res.ViewMin)
	viewMax := math.Max(tc.Max, res.ViewMax)
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
	for tickVal, labelStr := range res.IterateTicks(func(tick float64) string {
		return fmt.Sprintf("%g", tick)
	}) {
		screenX := mapX(tickVal)

		// Tick Mark
		dc.DrawLine(screenX, y, screenX, y+5)
		dc.Stroke()

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

func drawLogAxis(dc *gg.Context, x, y, width float64, tc TestCase, res LogResult) {
	// 1. Determine Viewport (Log Space)
	// The ViewMin/ViewMax from LogResult are already in linear space (e.g., 10, 1000)
	// We need to log them for coordinate mapping.

	viewMinLog := math.Log10(res.AxisResult.ViewMin)
	viewMaxLog := math.Log10(res.AxisResult.ViewMax)
	viewRangeLog := viewMaxLog - viewMinLog

	// Mapping Function: Maps a linear value to screen X using Log scaling
	mapX := func(val float64) float64 {
		if val <= 0 {
			return x // Safety
		}
		valLog := math.Log10(val)
		t := (valLog - viewMinLog) / viewRangeLog
		return x + t*width
	}

	// 2. Draw Data Range (Blue Bar)
	dc.SetRGBA(0, 0, 1, 0.2)
	dataLeft := mapX(tc.Min)
	dataRight := mapX(tc.Max)

	// Handle case where data is clipped by view (though algorithm shouldn't allow this for loose)
	if dataLeft < x {
		dataLeft = x
	}
	if dataRight > x+width {
		dataRight = x + width
	}

	dc.DrawRectangle(dataLeft, y-10, dataRight-dataLeft, 20)
	dc.Fill()

	// 3. Draw Axis Line
	dc.SetRGB(0, 0, 0)
	dc.DrawLine(x, y, x+width, y)
	dc.Stroke()

	// 4. Draw Ticks
	for tick, label := range ragged.Zip2(res.AxisResult.TickValues, res.AxisResult.TickLabels) {
		px := mapX(tick)

		// Major Tick Mark
		dc.DrawLine(px, y, px, y+8)
		dc.Stroke()

		// Label
		// Use the formatted label from the result
		dc.DrawStringAnchored(label, px, y+25, 0.5, 1.0)
	}

	// 5. Visualize Logarithmic Distribution (Green Dots)
	// This helps confirm the axis "feels" logarithmic
	rng := rand.New(rand.NewSource(99))
	dc.SetRGBA(0, 0.5, 0, 0.5) // Green dots

	minLog := math.Log10(tc.Min)
	maxLog := math.Log10(tc.Max)

	for k := 0; k < 30; k++ {
		// Generate random point in LOG space, then convert to linear
		// This creates a uniform visual distribution on a log scale
		rLog := minLog + rng.Float64()*(maxLog-minLog)
		val := math.Pow(10, rLog)

		px := mapX(val)
		py := y - 5 - rng.Float64()*10

		dc.DrawCircle(px, py, 2)
		dc.Fill()
	}
}

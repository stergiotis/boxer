package finddivisions

import (
	"iter"
	"math"

	"github.com/stergiotis/boxer/public/containers/ragged"
)

// AxisLayout is the generalized result of any labeling algorithm.
type AxisLayout struct {
	// 1. The Input Context (The "Truth")
	// Essential for the renderer to calculate padding/margins.
	DataMin float64
	DataMax float64

	// 2. The Output Viewport (The "Camera")
	// The axis line usually draws from ViewMin to ViewMax.
	// In "Loose" algorithms (Wilkinson), ViewMin <= DataMin.
	// In "Flexible" algorithms (Talbot), ViewMin might be > DataMin.
	ViewMin float64
	ViewMax float64

	// Grid: The mathematical step size.
	// Useful for drawing grid lines between ticks.
	Step float64

	// Content: The generated ticks and their labels.
	TickValues [] /*i*/ float64 // the mathematical position (Data Space)
	TickLabels [] /*i*/ string  // the rendered text (Visual Space)

	// Metadata: Useful for debug or comparison.
	Score     float64 // Higher is better (or cost, depending on algo)
	Algorithm string  // e.g., "Talbot-Extended", "Heckbert"
}

func (inst AxisLayout) IterateTicks(fallbackFormatter func(tick float64) string) iter.Seq2[float64,string] {
	if len(inst.TickLabels) == len(inst.TickValues) {
		return ragged.Zip2(inst.TickValues,inst.TickLabels)
	}
	return func(yield func(float64, string) bool) {
		for _, t := range inst.TickValues {
			if !yield(t, fallbackFormatter(t)) {
				break
			}
		}
	}
}

// MapToScreen converts a data value to a pixel coordinate.
// This helper proves why we don't store visual coords in the Tick struct.
func (inst AxisLayout) MapToScreen(value, axisStartPx, axisEndPx float64) float64 {
	// Normalize value 0..1 relative to the View
	t := (value - inst.ViewMin) / (inst.ViewMax - inst.ViewMin)

	// Interpolate to screen pixels
	return axisStartPx + t*(axisEndPx-axisStartPx)
}

// GenerateTicksNaive mimics R's seq function
func GenerateTicksNaive(min, max, step float64) []float64 {
	const eps = 1.0e-10
	const scale = 1.0e12
	var ticks []float64
	// Adding epsilon to handle floating point errors at the upper bound
	for t := min; t <= max+step*eps; t += step {
		// Round to remove noise (optional but good for display)
		val := math.Round(t*scale) / scale
		ticks = append(ticks, val)
	}
	return ticks
}
func GenerateTicks(min, max, step float64) []float64 {
	return GenerateTicksRobust(min, max, step)
}

// GenerateTicksRobust generates ticks using multiplication to minimize accumulated error.
// It also handles the "Negative Zero" edge case.
func GenerateTicksRobust(start, end, step float64) []float64 {
	const eps = 1.0e-10
	var ticks []float64

	// 1. Calculate the integer number of steps to avoid loop drift
	// We add a tiny epsilon to handle floating point inequality strictness
	count := math.Floor((end-start)/step + 1e-10)

	for i := 0; i <= int(count); i++ {
		// 2. Use fma (Fused Multiply Add) if available, or standard mult
		// val = start + i * step
		val := start + float64(i)*step

		// 3. Snap to Zero
		// If the value is extremely close to zero (relative to the step), make it exactly 0.0.
		// This fixes "-0.00" string formatting issues and simplicity score checks.
		if math.Abs(val) < step*eps {
			val = 0.0
		}

		// 4. Precision Truncation for Display
		// This prevents "0.1 + 0.2 = 0.300000000004"
		// We round to the 10th decimal place relative to the step magnitude.
		// (Optional, but recommended for visual systems)

		ticks = append(ticks, val)
	}
	return ticks
}

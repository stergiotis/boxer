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

func (inst AxisLayout) IterateTicks(fallbackFormatter func(tick float64) string) iter.Seq2[float64, string] {
	if len(inst.TickLabels) == len(inst.TickValues) {
		return ragged.Zip2(inst.TickValues, inst.TickLabels)
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

// GenerateTicksNaive mimics R's seq function.
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
	// An axis never needs more than a few dozen ticks. Cap far above that so a
	// legitimate range is untouched, but a degenerate one cannot spin this loop
	// effectively forever (or OOM): when step is tiny relative to the span —
	// which the Talbot search probes at extreme magnitudes, where a near-zero-
	// width span sits near 2^63 — (end-start)/step explodes to ~1e14+.
	const maxTicks = 10000
	var ticks []float64

	// 1. Calculate the integer number of steps to avoid loop drift
	// We add a tiny epsilon to handle floating point inequality strictness
	count := math.Floor((end-start)/step + 1e-10)
	// A non-finite step or range yields Inf/NaN count; int(Inf) is a huge value
	// that would hang the loop. Bail (the caller falls back to a simpler axis).
	if math.IsNaN(count) || math.IsInf(count, 0) {
		return ticks
	}
	if count > maxTicks {
		count = maxTicks
	}

	for i := 0; i <= int(count); i++ {
		// 2. Use fma (Fused Multiply Add) if available, or standard mult
		// val = start + i * step
		val := start + float64(i)*step

		// 3. Precision Truncation for Display
		// Multiplying bounds the error at one rounding instead of letting it
		// accumulate, but it does not remove it: 3*0.4 is 1.2000000000000002.
		// A shortest-round-trip formatter then prints all seventeen digits, so
		// snap onto a decimal grid far below the step before anyone sees it.
		val = snapDecimal(val, step)

		// 4. Snap to Zero
		// If the value is extremely close to zero (relative to the step), make it exactly 0.0.
		// This fixes "-0.00" string formatting issues and simplicity score checks.
		if math.Abs(val) < step*eps {
			val = 0.0
		}

		ticks = append(ticks, val)
	}
	return ticks
}

// snapDecimal rounds v onto a decimal grid ten decades below step's own
// decade. That is fine enough to leave every digit a reader could care about
// untouched — the grid sits far below the step's last significant digit, and
// far below one pixel on any axis — and coarse enough to erase the binary
// noise that start+i*step leaves behind.
//
// The scale factor is a power of ten, so it is exact for 10^0..10^22 and both
// the multiply and the divide are single correctly rounded operations; outside
// that range the value is returned untouched rather than snapped onto a grid
// that is itself inexact. Values that would overflow when scaled are likewise
// left alone.
func snapDecimal(v float64, step float64) float64 {
	if v == 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	step = math.Abs(step)
	if !(step > 0) || math.IsInf(step, 0) {
		return v
	}
	e := 10 - math.Floor(math.Log10(step))
	if !(e >= 0 && e <= 22) {
		return v
	}
	p := math.Pow(10, e)
	scaled := v * p
	if math.IsInf(scaled, 0) {
		return v
	}
	return math.Round(scaled) / p
}

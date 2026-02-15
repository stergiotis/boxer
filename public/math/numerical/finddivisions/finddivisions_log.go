package finddivisions

import (
	"fmt"
	"math"
)

type LogResult struct {
	AxisResult AxisLayout // The visual layout
	Exponents  []float64  // The raw log-values (useful for debugging)
}

func TalbotLogarithmic(dmin, dmax float64, m int, opts TalbotOptions, scorer LegibilityScorerI) (LogResult, error) {
	if dmin <= 0 || dmax <= 0 {
		return LogResult{}, fmt.Errorf("logarithmic axis requires positive data > 0")
	}

	// 1. Transform to Log Space
	lmin := math.Log10(dmin)
	lmax := math.Log10(dmax)

	// 2. Adjust Q for Log Space
	// In log space, we usually only want integer steps (1.0 -> factor of 10).
	// We might tolerate 0.5 (square root of 10 ≈ 3.16) or 0.301 (approx factor 2).
	// But usually, humans just want powers of 10.
	var logQ []float64

	rangeDecades := lmax - lmin
	if rangeDecades > 1.5 {
		// Strict powers of 10 for wide ranges
		logQ = []float64{1}
		// Q values represent LOG-steps.
		// 1.0  = 1 Decade (10^1)
		// 0.5  = sqrt(10) (~3.16)
		// 0.2  = 5 sub-ticks per decade
		// 0.25 = 4 sub-ticks per decade
		logQ = []float64{1, 0.5, 0.2, 0.25, 0.1}
	} else {
		// For small ranges (e.g. 10 to 50), we allow finer geometric steps.
		// These will produce ticks like 10, 31.6, 100 (steps of 0.5 log)
		logQ = []float64{1, 0.5, 0.25, 0.2}
	}

	// 3. Run the Linear Extended Algorithm on the Exponents
	// We disable "onlyLoose" usually, because exact power-of-10 bounds are rare data points.
	// We pass a 'nil' scorer here because we want to score the *Exponents* (0, 1, 2)
	// for simplicity, not the rendered "1000" strings which are wide.
	// Optimization: Ideally, we would wrap the scorer to score the inverse-transformed strings.
	opts.Qs = logQ
	res := Talbot(lmin, lmax, m, opts, nil)

	// 4. Transform Back (Inverse)
	// We need to build a new AxisResult because the tick values must be 10^x

	realTicks := make([]float64, len(res.TickValues))
	realLabels := make([]string, len(res.TickValues))
	exponents := make([]float64, len(res.TickValues))

	for i, expVal := range res.TickValues {
		realVal := math.Pow(10, expVal)
		exponents[i] = expVal

		// 5. Generate Log-Appropriate Labels
		var label string
		if math.Abs(expVal-math.Round(expVal)) < 1e-7 {
			// It's an integer power (e.g., 10^2)
			// Decide between "100" and "10^2" based on magnitude
			if math.Abs(expVal) <= 4 {
				label = fmt.Sprintf("%g", realVal) // 0.01, 100, 10000
			} else {
				label = fmt.Sprintf("10^%d", int(math.Round(expVal))) // 10^5 (Generic notation)
				// Or use fancy unicode: fmt.Sprintf("10%s", toSuperscript(int(expVal)))
			}
		} else {
			// It's a geometric intermediate (e.g., 10^1.5 ≈ 31.6)
			label = fmt.Sprintf("%.2g", realVal)
		}

		realTicks[i] = realVal
		realLabels[i] = label
	}

	layout := AxisLayout{
		DataMin:    dmin,
		DataMax:    dmax,
		ViewMin:    math.Pow(10, res.ViewMin),
		ViewMax:    math.Pow(10, res.ViewMax),
		Step:       res.Step, // This is the step in LOG space (e.g. 1.0)
		TickValues: realTicks,
		TickLabels: realLabels,
		Score:      res.Score,
		Algorithm:  "Talbot-Log",
	}

	return LogResult{AxisResult: layout, Exponents: exponents}, nil
}

//go:build llm_generated_gemini3pro

package finddivisions

import (
	"fmt"
	"math"
)

/*
While published in *Graphics Gems I* in **1990** (originally in C), Heckbert's method codified heuristics that had been used in plotting software (often written in Fortran) since the 1970s.
The goal is to generate axis labels that are "human-readable" (e.g., multiples of 1, 2, or 5) rather than raw mathematical divisions.

**The Steps:**
1.  **Calculate the range:** $R = \text{max} - \text{min}$.
2.  **Calculate the "rough" step:** $S_{rough} = R / \text{desired\_ticks}$.
3.  **Normalize:** Divide the rough step by the magnitude of the number (power of 10) to get a fraction between 1.0 and 10.0.
4.  **Round to a "Nice" fraction:** Round that fraction to the nearest "nice" number: **1, 2, or 5**.
5.  **Re-scale:** Multiply the nice fraction back by the magnitude to get the final **tick spacing**.
6.  **Calculate bounds:** Find the new minimum and maximum that are multiples of this spacing and enclose the original data.
*/

// TickResult holds the calculated nicely spaced ticks
type TickResult struct {
	Min     float64
	Max     float64
	Spacing float64
	Ticks   []float64
}

// nicenum rounds x to a "nice" number (1, 2, 5, 10).
// If round is true, it rounds to the nearest nice number.
// If round is false, it takes the ceiling (loose).
func nicenum(x float64, round bool) float64 {
	exp := math.Floor(math.Log10(x))
	f := x / math.Pow(10, exp)
	var nf float64

	if round {
		if f < 1.5 {
			nf = 1
		} else if f < 3 {
			nf = 2
		} else if f < 7 {
			nf = 5
		} else {
			nf = 10
		}
	} else {
		if f <= 1 {
			nf = 1
		} else if f <= 2 {
			nf = 2
		} else if f <= 5 {
			nf = 5
		} else {
			nf = 10
		}
	}
	return nf * math.Pow(10, exp)
}

// CalculateTicks generates nice graph labels.
func CalculateTicks(min, max float64, desiredTicks int) (TickResult, error) {
	if desiredTicks < 2 {
		return TickResult{}, fmt.Errorf("desiredTicks must be at least 2")
	}
	if min == max {
		return TickResult{Min: min, Max: max, Spacing: 0, Ticks: []float64{min}}, nil
	}
	if min > max {
		min, max = max, min
	}

	rangeVal := nicenum(max-min, false)
	d := nicenum(rangeVal/float64(desiredTicks-1), true)

	graphMin := math.Floor(min/d) * d
	graphMax := math.Ceil(max/d) * d

	// Handle floating point precision issues for the loop
	var ticks []float64
	// We use a small epsilon to ensure the last tick is included despite float errors
	epsilon := d * 1e-10

	for val := graphMin; val <= graphMax+epsilon; val += d {
		// Clean up floating point noise (e.g. 0.3000000000004)
		// This is a simple way to truncate precision errors for display
		cleanVal := math.Round(val/d) * d
		ticks = append(ticks, cleanVal)
	}

	return TickResult{
		Min:     graphMin,
		Max:     graphMax,
		Spacing: d,
		Ticks:   ticks,
	}, nil
}

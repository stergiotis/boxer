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

// Heckbert generates nice graph labels.
func Heckbert(min, max float64, desiredTicks int) (AxisLayout, error) {
	if desiredTicks < 2 {
		return AxisLayout{}, fmt.Errorf("desiredTicks must be at least 2")
	}
	if min > max {
		min, max = max, min
	}
	if min == max {
		return AxisLayout{
			DataMin:    min,
			DataMax:    max,
			ViewMin:    min,
			ViewMax:    max,
			Step:       0,
			TickValues: nil,
			TickLabels: nil,
			Score:      0,
			Algorithm:  "Heckbert",
		}, nil
	}

	rangeVal := nicenum(max-min, false)
	d := nicenum(rangeVal/float64(desiredTicks-1), true)

	viewMin := math.Floor(min/d) * d
	viewMax := math.Ceil(max/d) * d

	return AxisLayout{
		DataMin:    min,
		DataMax:    max,
		ViewMin:    viewMin,
		ViewMax:    viewMax,
		Step:       d,
		TickValues: GenerateTicks(viewMin, viewMax, d),
		TickLabels: nil,
		Score:      0,
		Algorithm:  "Heckbert",
	}, nil
}

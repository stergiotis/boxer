//go:build llm_generated_gemini3pro

package numerical

import "math"

/* Ported by Gemini 3 Pro from https://cran.r-project.org/web/packages/labeling/index.html
Original licence:
> Copyright (c) 2020, Justin Talbot
>
> Permission is hereby granted, free of charge, to any person obtaining
> a copy of this software and associated documentation files (the
> "Software"), to deal in the Software without restriction, including
> without limitation the rights to use, copy, modify, merge, publish,
> distribute, sublicense, and/or sell copies of the Software, and to
> permit persons to whom the Software is furnished to do so, subject to
> the following conditions:
>
> The above copyright notice and this permission notice shall be
> included in all copies or substantial portions of the Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
> EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
> MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
> NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
> LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
> OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
> WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

// Constants mimicking R's environment
const (
	DoubleEps = 2.220446049250313e-16
	Eps       = DoubleEps * 100.0
)

type ExtendedResult struct {
	Min   float64
	Max   float64
	Step  float64
	Score float64
	Ticks []float64
}

// Weights for the optimization components
type Weights struct {
	Simplicity float64
	Coverage   float64
	Density    float64
	Legibility float64
}

// LegibilityScorerI defines how to evaluate the visual quality of a tick set.
type LegibilityScorerI interface {
	// Score returns a value between -Inf and 1.0.
	// lmin, lmax, lstep: The generated tick parameters.
	// dmin, dmax: The original data range.
	Score(lmin, lmax, lstep, dmin, dmax float64) float64
}

// NoOpLegibility is the default implementation (always returns 1.0).
type NoOpLegibility struct{}

func (n NoOpLegibility) Score(lmin, lmax, lstep, dmin, dmax float64) float64 {
	return 1.0
}

var DefaultWeights = Weights{0.25, 0.2, 0.5, 0.05}
var DefaultQ = []float64{1, 5, 2, 2.5, 4, 3}

// simplicity calculates the simplicity score (how "nice" the numbers are)
func simplicity(q float64, Q []float64, j int, lmin, lmax, lstep float64) float64 {
	n := float64(len(Q))
	i := -1
	for idx, val := range Q {
		if val == q {
			i = idx + 1 // R uses 1-based indexing
			break
		}
	}

	// v is 1 if labels include 0, else 0
	v := 0.0
	mod := math.Mod(lmin, lstep)
	// Handle negative modulo logic to match R's behavior or standard math
	if mod < 0 {
		mod += lstep
	}

	// Check if a tick hits 0. Logic: if remainder is near 0 or near step
	isZero := (mod < Eps || (lstep-mod) < Eps) && lmin <= 0 && lmax >= 0
	if isZero {
		v = 1.0
	}

	return 1.0 - (float64(i)-1.0)/(n-1.0) - float64(j) + v
}

func simplicityMax(q float64, Q []float64, j int) float64 {
	n := float64(len(Q))
	i := -1
	for idx, val := range Q {
		if val == q {
			i = idx + 1
			break
		}
	}
	v := 1.0 // Assume best case (includes zero)
	return 1.0 - (float64(i)-1.0)/(n-1.0) - float64(j) + v
}

func coverage(dmin, dmax, lmin, lmax float64) float64 {
	r := dmax - dmin
	return 1.0 - 0.5*(math.Pow(dmax-lmax, 2)+math.Pow(dmin-lmin, 2))/math.Pow(0.1*r, 2)
}

func coverageMax(dmin, dmax, span float64) float64 {
	r := dmax - dmin
	if span > r {
		half := (span - r) / 2.0
		return 1.0 - 0.5*(2*math.Pow(half, 2))/math.Pow(0.1*r, 2)
	}
	return 1.0
}

func density(k, m int, dmin, dmax, lmin, lmax float64) float64 {
	r := float64(k-1) / (lmax - lmin)
	rt := float64(m-1) / (math.Max(lmax, dmax) - math.Min(dmin, lmin))
	return 2.0 - math.Max(r/rt, rt/r)
}

func densityMax(k, m int) float64 {
	if k >= m {
		return 2.0 - float64(k-1)/float64(m-1)
	}
	return 1.0
}

// GenerateTicks mimics R's seq function
func GenerateTicks(min, max, step float64) []float64 {
	var ticks []float64
	// Adding epsilon to handle floating point errors at the upper bound
	for t := min; t <= max+step*1e-10; t += step {
		// Round to remove noise (optional but good for display)
		scale := 1e12
		val := math.Round(t*scale) / scale
		ticks = append(ticks, val)
	}
	return ticks
}

// Extended implements the Extended Wilkinson Algorithm
func Extended(dmin, dmax float64, m int, Q []float64, onlyLoose bool, w Weights, scorer LegibilityScorerI) ExtendedResult {
	if Q == nil {
		Q = DefaultQ
	}

	if dmin > dmax {
		dmin, dmax = dmax, dmin
	}

	if dmax-dmin < Eps {
		return ExtendedResult{Min: dmin, Max: dmax, Step: 0, Ticks: []float64{dmin}}
	}
	// If scorer is nil, use default
	if scorer == nil {
		scorer = NoOpLegibility{}
	}

	best := ExtendedResult{Score: -2.0} // Initialize with worst possible score

	// Outer loop: j (Simplicity / Skipping)
	// Theoretically infinite, but usually terminates quickly. Safety cap at 1000.
	for j := 1; j < 1000; j++ {
		for _, q := range Q {
			sm := simplicityMax(q, Q, j)

			// Pruning 1
			if (w.Simplicity*sm + w.Coverage + w.Density + w.Legibility) < best.Score {
				// Optimization: If the best possible simplicity here is worse than our current best,
				// and since simplicity decreases as j increases, we can stop searching entirely.
				// This acts as "j -> Inf" break.
				goto Finish
			}

			// Middle loop: k (Density / Tick Count)
			// Starts at 2 ticks. Safety cap at 1000.
			for k := 2; k < 1000; k++ {
				dm := densityMax(k, m)

				// Pruning 2
				if (w.Simplicity*sm + w.Coverage + w.Density*dm + w.Legibility) < best.Score {
					break // This k is too bad, and higher k will differ more from m
				}

				delta := (dmax - dmin) / float64(k+1) / float64(j) / q
				z := math.Ceil(math.Log10(delta))

				// Inner loop: z (Coverage / Zoom level)
				// Safety cap.
				for zLoop := 0; zLoop < 100; zLoop++ {
					step := float64(j) * q * math.Pow(10, z)

					// Calculate span for k ticks
					span := step * float64(k-1)
					cm := coverageMax(dmin, dmax, span)

					// Pruning 3
					if (w.Simplicity*sm + w.Coverage*cm + w.Density*dm + w.Legibility) < best.Score {
						break // Larger z means larger step, worse coverage
					}

					minStart := math.Floor(dmax/step)*float64(j) - float64(k-1)*float64(j)
					maxStart := math.Ceil(dmin/step) * float64(j)

					if minStart > maxStart {
						z++
						continue
					}

					// Innermost loop: offset/phase
					for start := minStart; start <= maxStart; start += float64(j) {
						lmin := start * (step / float64(j))
						lmax := lmin + step*float64(k-1)
						lstep := step

						s := simplicity(q, Q, j, lmin, lmax, lstep)
						c := coverage(dmin, dmax, lmin, lmax)
						g := density(k, m, dmin, dmax, lmin, lmax)
						l := scorer.Score(lmin, lmax, lstep, dmin, dmax)

						score := w.Simplicity*s + w.Coverage*c + w.Density*g + w.Legibility*l

						// Constraints
						isLoose := lmin <= dmin && lmax >= dmax
						if score > best.Score && (!onlyLoose || isLoose) {
							best = ExtendedResult{
								Min:   lmin,
								Max:   lmax,
								Step:  lstep,
								Score: score,
							}
						}
					}
					z++
				}
			}
		}
	}

Finish:
	best.Ticks = GenerateTicks(best.Min, best.Max, best.Step)
	return best
}

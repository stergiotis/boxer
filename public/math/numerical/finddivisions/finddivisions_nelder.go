package finddivisions

import (
	"math"
)

/*
J. A. Nelder. (1976). Algorithm AS 96: A Simple Algorithm for Scaling Graphs. Journal of the Royal Statistical Society. Series C (Applied Statistics), 25(1), 94–96.
doi:10.2307/2346537

Translated from Fortran to Chez Scheme: P. Stergiotis
Translated from Chez Scheme to go: Gemini 3 Pro Preview

```scheme
(define-exported find-divisions/nelder
  (case-lambda
    ((fmin fmax n) (find-divisions/nelder fmin fmax n '#(1 12/10 16/10 2 25/10 3 4 5 6 8 10)))
    ((fmin fmax n Q)
                (%find-divisions-in-contract fmin fmax n)
                (let* ((exact (and (exact? fmin) (exact? fmax) (exact? n)))
                         (nq (vector-length Q))
                         (fmax (inexact->exact fmax))
                         (fmin (inexact->exact fmin))
                         (n (inexact->exact n))
                         (rn (- n 1))
                         (step (/ (- fmax fmin) rn))
                         ;; calculate step
                         (s (let loop ((s step))
                              (cond
                                ((<= s 1) (loop (* s 10)))
                                ((< 10 s) (loop (/ s 10)))
                                (else s)))))
                    (let-values (((valmin valmax step)
                                  (let* ((q (let loop ((i 0)) (cond ((<= s (vector-ref Q i)) (vector-ref Q i)) (else (loop (fx+ i 1))))))
                                         (step (/ (* step q) s))
                                         (range (* step rn))
                                         ;; make first estimate of valmin
                                         (x (* 1/2 (+ 1 (/ (+ fmin fmax (- range)) step))))
                                         (j (if (negative? x) (- (truncate x) 1) (truncate x)))
                                         (valmin (if (and (>= fmin 0) (>= range fmax)) 0 (* step j))))
                                    (if (or (> fmax 0) (< range (- fmin)))
                                      ;; goto 200
                                      (values valmin (+ valmin range) step)
                                      (values (- range) 0 step)))))
                                (if exact
                                  (values (- valmin (/ step 2)) (+ valmax (/ step 2)) step)
                                  (values (exact->inexact (- valmin (/ step 2))) (exact->inexact (+ valmax (/ step 2))) (exact->inexact step))))))))
```
*/

// Nelder implements J.A. Nelder's 1976 scaling algorithm.
// fmin, fmax: data range
// n: desired number of ticks (must be > 1)
// Q: optional list of "nice" steps (normalized 1-10). Pass nil for default.
func Nelder(fmin, fmax float64, n int, Q []float64) AxisLayout {
	// Default Q from the Scheme code
	if Q == nil {
		Q = []float64{1.0, 1.2, 1.6, 2.0, 2.5, 3.0, 4.0, 5.0, 6.0, 8.0, 10.0}
	}

	if fmin > fmax {
		fmin, fmax = fmax, fmin
	}
	// If range is 0, create an artificial range centered on the value
	if fmin == fmax {
		fmax = fmin + 1.0
		fmin = fmin - 1.0
	}

	rn := float64(n - 1)
	step := (fmax - fmin) / rn

	// Normalize step s to be between 1 and 10
	s := step
	powerOf10 := 1.0

	// Corresponds to Scheme loop: ((<= s 1) ...)
	for s <= 1.0 {
		s *= 10.0
		powerOf10 *= 10.0
	}
	// Corresponds to Scheme loop: ((< 10 s) ...)
	for s > 10.0 {
		s /= 10.0
		powerOf10 /= 10.0
	}

	// Find the smallest q in Q such that s <= q
	// Corresponds to Scheme: (let loop ((i 0)) ...)
	chosenQ := Q[len(Q)-1]
	for _, q := range Q {
		// We use a tiny epsilon for float comparison safety
		if s <= q+1e-14 {
			chosenQ = q
			break
		}
	}

	// Recalculate step with the chosen nice number
	// Scheme: (step (/ (* step q) s))
	// Because s was effectively (step * powerOf10), we can just do:
	step = chosenQ / powerOf10

	rangeVal := step * rn

	// Make first estimate of valmin (centering logic)
	// Scheme: (* 1/2 (+ 1 (/ (+ fmin fmax (- range)) step)))
	x := 0.5 * (1.0 + (fmin+fmax-rangeVal)/step)

	// Truncate logic
	// Scheme: (j (if (negative? x) (- (truncate x) 1) (truncate x)))
	// Note: math.Trunc in Go behaves like Scheme truncate.
	var j float64
	if x < 0 {
		j = math.Trunc(x) - 1.0
	} else {
		j = math.Trunc(x)
	}

	// Zero crossing logic
	// Scheme: (valmin (if (and (>= fmin 0) (>= range fmax)) 0 (* step j)))
	// We add epsilon to comparisons to handle float inaccuracies
	epsilon := step * 1e-10
	var valmin float64
	if fmin >= -epsilon && rangeVal >= fmax-epsilon {
		valmin = 0.0
	} else {
		valmin = step * j
	}

	// Final bounds adjustment
	// Scheme: (if (or (> fmax 0) (< range (- fmin))) ... )
	// The scheme code calculates ticks, then pads boundaries by step/2.

	// We replicate the exact logic:
	var finalMin, finalMax float64

	if fmax > epsilon || rangeVal < (-fmin+epsilon) {
		// Standard case
		finalMin = valmin
		finalMax = valmin + rangeVal
	} else {
		// Case where range is entirely negative and far from 0?
		finalMin = -rangeVal
		finalMax = 0
	}

	// The Scheme code returns: values (- valmin (/ step 2)) ...
	// This adds a half-step padding to the graph bounds.
	r := AxisLayout{
		DataMin:    fmin,
		DataMax:    fmax,
		ViewMin:    finalMin - step/2.0,
		ViewMax:    finalMax + step/2.0,
		Step:       step,
		TickLabels: nil,
		Score:      0,
		Algorithm:  "Nelder",
	}
	r.TickValues = GenerateTicks(r.ViewMin, r.ViewMax, r.Step)
	return r
}

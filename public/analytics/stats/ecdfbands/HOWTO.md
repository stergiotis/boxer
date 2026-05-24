---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to compute simultaneous CDF confidence bands

Three common recipes against the `ecdfbands` API. All entry points
return errors on bad input rather than panicking, so the snippets
below show the canonical error-handling shape.

## Recipe 1 — Band from a sorted iid sample

The default workflow: you have an iid sample on the unit interval,
you want a 95% simultaneous confidence band on its CDF.

```go
import "github.com/stergiotis/boxer/public/analytics/stats/ecdfbands"

// sample is iid Uniform(0, 1) or any continuous distribution rescaled
// to [0, 1]. Must be sorted ascending; BandsForSample does not sort.
sample := []float64{...}
slices.Sort(sample)

band, err := ecdfbands.BandsForSample(sample, 0.05, ecdfbands.BandMethodBerkJones)
if err != nil {
    return eh.Errorf("simultaneous band: %w", err)
}

for i, x := range band.Xs {
    fmt.Printf("F(%.3f) ∈ [%.3f, %.3f]\n", x, band.LowerCDF[i], band.UpperCDF[i])
}
```

`band.CritC` is the critical value driving the band — useful when
comparing methods.

## Recipe 2 — Band from a streaming sketch

When the sample is too large to hold (n > 10⁶), keep a t-digest or
Greenwald-Khanna sketch alongside the data and call `BandsForGrid`
at a chosen x-grid.

```go
const n = 5_000_000

digest := tdigest.NewTDigest()
for x := range stream {
    digest.Push(x)
}

// Evaluate F_n on a 200-point grid covering the data range.
xs := make([]float64, 200)
fn := make([]float64, 200)
xmin, xmax := digest.Min(), digest.Max()
for i := range xs {
    xs[i] = xmin + (xmax-xmin)*float64(i)/float64(len(xs)-1)
    fn[i] = digest.CDF(xs[i])
}

g, err := ecdfbands.BandsForGrid(xs, fn, n, 0.05,
    ecdfbands.BandMethodBerkJones)
if err != nil {
    return eh.Errorf("streaming band: %w", err)
}
for i, x := range g.Xs {
    fmt.Printf("F(%.3f) ∈ [%.3f, %.3f]\n", x, g.LowerCDF[i], g.UpperCDF[i])
}
```

The grid does not need to coincide with observed values; the
package evaluates the band family's continuous-p edge at every
F_n(xs[i]).

## Recipe 3 — Compare band methods at a fixed configuration

When deciding which family fits a particular dashboard, evaluate
all four at the same (n, α) and inspect the geometries.

```go
methods := []ecdfbands.BandMethodE{
    ecdfbands.BandMethodBerkJones,
    ecdfbands.BandMethodDKW,
    ecdfbands.BandMethodEqualPrecision,
    ecdfbands.BandMethodHigherCriticism,
}

const n = 100
const alpha = 0.05

for _, m := range methods {
    lower, upper, err := ecdfbands.QuantileBoundaries(n, alpha, m)
    if err != nil {
        return err
    }
    c, _ := ecdfbands.CriticalValue(n, alpha, m)
    fmt.Printf("method=%d c=%.3f median-width=%.3f tail-width=%.3f\n",
        m, c, upper[n/2]-lower[n/2], upper[n-1]-lower[n-1])
}
```

Berk-Jones produces the tightest tail bands; DKW yields the widest
in the tails but the simplest closed form; EP and HC trade
tightness in opposite directions.

## Choosing a method

| Method | Tail behaviour | When to pick it |
|---|---|---|
| Berk-Jones | Tight tails | Default. Best at detecting deviations near 0 or 1. |
| DKW | Constant strip | When you need a closed-form formula or visual symmetry. |
| Equal Precision | Uniform precision | When the tails are not the primary concern but you want consistent precision across the interior. |
| Higher Criticism | Adaptive | When the alternative is sparse, heterogeneous mixtures. |

## Verification

`BandsForSample` produces an empirically (1-α)-calibrated band on
iid uniform data. To verify a specific (n, α, method) configuration:

```bash
go test -tags slow -run TestCoverageBerkJones \
  ./public/analytics/stats/ecdfbands/...
```

Expect each cell to log a < 4σ deviation from nominal coverage.

## Troubleshooting

- **Symptom:** `upper bracket falls short of target P`.
  **Cause:** the family's `criticalValueBracket(n, α)` upper
  endpoint does not reach the target probability.
  **Fix:** widen the bracket in `band_<family>.go` and add a
  regression test at the failing configuration.

- **Symptom:** `crossingProbabilitySteck` produces a NaN or
  negative P.
  **Cause:** invoked at n > 30 where Hessenberg LU is unstable.
  **Fix:** use `CrossingAlgorithmAuto` (the default for high-level
  entry points) or pass `CrossingAlgorithmMoscovich` explicitly.

- **Symptom:** `BandsForSample` errors with `sample not sorted`.
  **Cause:** input slice is not non-decreasing.
  **Fix:** `slices.Sort(sample)` before the call. The package does
  not sort in-place to avoid surprising the caller.

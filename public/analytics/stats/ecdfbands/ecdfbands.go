//go:build llm_generated_opus47

package ecdfbands

import (
	"context"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// SampleBand bundles a finite-sample exact simultaneous (1-α)·100%
// confidence band on the CDF computed from an iid sorted sample.
// The band is consumable as a step function: between adjacent order
// statistics Xs[i] and Xs[i+1], F is bounded by LowerCDF[i] and
// UpperCDF[i].
//
// Fields:
//
//   - Xs        — the sorted input sample, echoed for convenience.
//   - LowerCDF  — length-n band lower edges.
//   - UpperCDF  — length-n band upper edges.
//   - Method    — band family used.
//   - Alpha     — nominal complement-of-coverage.
//   - CritC     — critical value c that the inversion produced; useful
//     diagnostic when reasoning about band shape or comparing methods.
type SampleBand struct {
	Xs       []float64
	LowerCDF []float64
	UpperCDF []float64
	Method   BandMethodE
	Alpha    float64
	CritC    float64
}

// BandsForSample computes the simultaneous (1-α)·100% confidence band
// on F using the given band method. The input sorted slice must be
// non-decreasing; the method enumerates the band family
// (Berk-Jones, DKW, equal-precision, higher-criticism).
//
// The crossing-probability engine is dispatched by CrossingAlgorithmAuto
// — Steck-Noé for small n (≤ steckN), Moscovich-Nadler above.
//
// Allocates fresh slices for SampleBand.LowerCDF / UpperCDF; the
// returned Xs slice is a copy of sorted (mutating the input post-call
// will not corrupt the returned band).
func BandsForSample(sorted []float64, alpha float64, method BandMethodE) (b SampleBand, err error) {
	n := len(sorted)
	if n == 0 {
		err = eh.Errorf("empty sample")
		return
	}
	if err = validateSorted(sorted); err != nil {
		return
	}
	c, lower, upper, err := criticalValueAndBands(n, alpha, method, CrossingAlgorithmAuto)
	if err != nil {
		return
	}
	b.Xs = append([]float64(nil), sorted...)
	b.LowerCDF = lower
	b.UpperCDF = upper
	b.Method = method
	b.Alpha = alpha
	b.CritC = c
	return
}

// GridBand bundles a simultaneous confidence band evaluated at an
// explicit set of x positions where the empirical CDF value F_n is
// known — the streaming-friendly counterpart to SampleBand. The
// interpretation is identical: at each xs[i], F(xs[i]) is contained
// in [LowerCDF[i], UpperCDF[i]] with simultaneous (1-α)·100% coverage.
//
// Fields:
//
//   - Xs        — caller-supplied grid positions (echoed for convenience).
//   - LowerCDF  — band lower edges, length len(Xs).
//   - UpperCDF  — band upper edges, length len(Xs).
//   - Method    — band family used.
//   - Alpha     — nominal complement-of-coverage.
//   - N         — sample size on which F_n was computed.
//   - CritC     — critical value c that the inversion produced.
type GridBand struct {
	Xs       []float64
	LowerCDF []float64
	UpperCDF []float64
	Method   BandMethodE
	Alpha    float64
	N        int
	CritC    float64
}

// BandsForGrid evaluates the simultaneous confidence band at user-
// supplied x positions where the empirical CDF value F_n is known.
// This is the streaming-friendly entry point: callers holding a
// t-digest (or any other ECDF estimator) at sample size n compute
// F_n at a fixed grid of x values and let BandsForGrid expand each
// to a band on F.
//
// Inputs:
//
//   - xs    — grid positions, monotone non-decreasing. Not required
//     to coincide with any observed value.
//   - fnAt  — F_n(xs[i]) ∈ [0, 1], monotone non-decreasing.
//   - n     — total sample size on which F_n was computed.
//   - alpha — desired confidence complement.
//   - method — band family.
//
// The returned GridBand's LowerCDF / UpperCDF have length len(xs).
// For grid points where F_n is in (0, 1), the band is the per-p
// band of the chosen family at the inverted critical value c. For
// F_n == 0 the band lower edge is 0 and the upper edge is the
// family's "rank-zero" tail bound; for F_n == 1 the lower edge is
// the family's "rank-n" tail bound and the upper edge is 1.
func BandsForGrid(xs, fnAt []float64, n int, alpha float64, method BandMethodE) (b GridBand, err error) {
	if len(xs) != len(fnAt) {
		err = eh.Errorf("xs and fnAt length mismatch (%d vs %d)", len(xs), len(fnAt))
		return
	}
	if n <= 0 {
		err = eh.Errorf("n must be positive, got %d", n)
		return
	}
	for i, v := range fnAt {
		if math.IsNaN(v) || v < 0 || v > 1 {
			err = eh.Errorf("fnAt[%d] out of [0,1]: %v", i, v)
			return
		}
		if i > 0 && v < fnAt[i-1] {
			err = eh.Errorf("fnAt not monotone at i=%d (%v < %v)", i, v, fnAt[i-1])
			return
		}
	}
	c, _, _, err := criticalValueAndBands(n, alpha, method, CrossingAlgorithmAuto)
	if err != nil {
		return
	}

	family := bandFamilyDispatch(method)
	if family == nil {
		err = eh.Errorf("unknown BandMethodE %d", method)
		return
	}

	lower := make([]float64, len(xs))
	upper := make([]float64, len(xs))
	for i, p := range fnAt {
		lo, hi := family.bandAtP(n, c, p)
		lower[i] = lo
		upper[i] = hi
	}
	b.Xs = append([]float64(nil), xs...)
	b.LowerCDF = lower
	b.UpperCDF = upper
	b.Method = method
	b.Alpha = alpha
	b.N = n
	b.CritC = c
	return
}

// DkwBandForGrid evaluates the Dvoretzky-Kiefer-Wolfowitz (Massart 1990)
// simultaneous (1-α)·100% confidence band at the closed-form half-width
//
//	ε = √( ln(2/α) / (2n) )
//
// directly — with NO critical-value inversion. It is therefore O(len(xs))
// and effectively instant at any n, where every exact family (Berk-Jones,
// equal-precision, higher-criticism, and even DKW routed through
// BandsForGrid, which bisection-refines ε against the crossing
// probability) pays an O(n²)-per-eval inversion that runs into minutes at
// n≈1e4.
//
// The result is the symmetric ε-strip around F_n clipped to [0,1]. It is
// conservative — the asymptotic Massart ε is not refined down to exact
// crossing probability — which is exactly what you want for a band drawn
// immediately as a preview while a tighter exact band warms in the
// background (see widgets/ecdf RenderGridPreview). xs/fnAt carry the same
// contract as [BandsForGrid]: equal length, fnAt ∈ [0,1] non-decreasing; n
// is the sample size F_n was built on.
func DkwBandForGrid(xs, fnAt []float64, n int, alpha float64) (b GridBand, err error) {
	if len(xs) != len(fnAt) {
		err = eh.Errorf("xs and fnAt length mismatch (%d vs %d)", len(xs), len(fnAt))
		return
	}
	if n <= 0 {
		err = eh.Errorf("n must be positive, got %d", n)
		return
	}
	if alpha <= 0 || alpha >= 1 || math.IsNaN(alpha) {
		err = eh.Errorf("alpha must lie strictly inside (0, 1), got %v", alpha)
		return
	}
	for i, v := range fnAt {
		if math.IsNaN(v) || v < 0 || v > 1 {
			err = eh.Errorf("fnAt[%d] out of [0,1]: %v", i, v)
			return
		}
		if i > 0 && v < fnAt[i-1] {
			err = eh.Errorf("fnAt not monotone at i=%d (%v < %v)", i, v, fnAt[i-1])
			return
		}
	}
	eps := dkwEpsilon(n, alpha)
	fam := dkwFamily{}
	lower := make([]float64, len(xs))
	upper := make([]float64, len(xs))
	for i, p := range fnAt {
		lower[i], upper[i] = fam.bandAtP(n, eps, p)
	}
	b.Xs = append([]float64(nil), xs...)
	b.LowerCDF = lower
	b.UpperCDF = upper
	b.Method = BandMethodDKW
	b.Alpha = alpha
	b.N = n
	b.CritC = eps
	return
}

// dkwEpsilon is the closed-form DKW-Massart band half-width
// ε = √(ln(2/α)/(2n)). Shared by [DkwBandForGrid] and the dkwFamily
// critical-value bracket so the instant preview and the bisection-refined
// exact DKW band start from the same asymptotic value.
func dkwEpsilon(n int, alpha float64) float64 {
	return math.Sqrt(math.Log(2/alpha) / (2 * float64(n)))
}

// QuantileBoundaries returns the raw per-rank band edges
// (lower[i], upper[i]) for the given (n, α, method). lower[i] /
// upper[i] is the simultaneous (1-α)·100% interval on U_{(i+1)} —
// the (i+1)-th order statistic of n iid Uniform(0, 1) draws under H0.
//
// This is the low-level entry point; callers wanting CDF-axis bands
// for visualisation should prefer BandsForSample / BandsForGrid.
func QuantileBoundaries(n int, alpha float64, method BandMethodE) (lower, upper []float64, err error) {
	_, lower, upper, err = criticalValueAndBands(n, alpha, method, CrossingAlgorithmAuto)
	return
}

// CriticalValue returns just the inverted critical value c for the
// given (n, α, method) — the diagnostic value driving each family's
// band geometry. Useful when comparing band methods at the same
// nominal coverage.
func CriticalValue(n int, alpha float64, method BandMethodE) (c float64, err error) {
	c, _, _, err = criticalValueAndBands(n, alpha, method, CrossingAlgorithmAuto)
	return
}


// BandReady reports whether the (n, α, method) critical value is
// already cached — i.e. whether a subsequent BandsForGrid /
// BandsForSample returns without running the O(n²) inversion.
// Non-blocking: a pure cache probe, no computation. Render loops use
// this to choose between drawing the band now and scheduling a
// background WarmBand.
func BandReady(n int, alpha float64, method BandMethodE) bool {
	key := bandCacheKey{n: n, method: method, alphaBits: quantizeAlpha(alpha)}
	bandCacheMu.RLock()
	_, ok := bandCache[key]
	bandCacheMu.RUnlock()
	return ok
}

// WarmBand computes and caches the (n, α, method) critical value when
// absent, reporting bisection progress via onProgress and honouring ctx
// for cancellation. Built to run on a background goroutine: once it
// returns nil, BandReady(n, α, method) is true and the following
// BandsForGrid / BandsForSample is a cheap cache hit. A cancelled solve
// returns ctx.Err() (wrapped) and caches nothing.
func WarmBand(ctx context.Context, n int, alpha float64, method BandMethodE, onProgress ProgressFunc) (err error) {
	_, _, _, err = criticalValueAndBandsCtx(ctx, n, alpha, method, CrossingAlgorithmAuto, onProgress)
	return
}

// validateSorted checks the non-decreasing invariant on a sample
// slice. NaN values are rejected. Callers should sort before
// calling BandsForSample — this routine does not sort in place.
func validateSorted(sorted []float64) (err error) {
	for i, x := range sorted {
		if math.IsNaN(x) {
			err = eh.Errorf("NaN in sample at i=%d", i)
			return
		}
		if i > 0 && x < sorted[i-1] {
			err = eh.Errorf("sample not sorted at i=%d (%v < %v)", i, x, sorted[i-1])
			return
		}
	}
	return
}

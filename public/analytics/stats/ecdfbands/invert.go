package ecdfbands

import (
	"context"
	"math"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ProgressFunc receives solver progress: `done` crossing-probability
// evaluations completed out of an estimated `total` (two bracket checks
// plus the bisection iterations, fixed up front). Each eval is the same
// O(n²) cost, so the ratio is a near-linear progress signal callers can
// turn into a fraction and ETA. nil disables reporting. Invoked on the
// solving goroutine — keep it cheap and non-blocking.
type ProgressFunc func(done, total int)

// criticalValueAndBands returns the critical value c and the
// per-rank boundary sequences (lower, upper) for the given band
// method at confidence 1-α. The critical value is the unique
// solution of CrossingProbability(lower(c), upper(c)) = 1 - α
// found by bisection inside the family's bracket.
//
// Results are cached by (n, method, quantized α). The α quantization
// is at 1e-9 — finer than any practical user request, coarse enough
// to avoid trivial misses from numerical ε-noise.
//
// algo selects the engine used during inversion; under the Auto
// dispatcher this is Moscovich-Nadler for n > steckN and Steck-Noé
// otherwise. Passing CrossingAlgorithmSteck at large n is honoured
// but will be slow and lose digits — primarily useful for testing.
//
// Returned slices are freshly allocated copies — the caller may
// mutate them without polluting the cache.
//
// Concurrency: cache access is mutex-protected, but two goroutines
// missing the cache for the same key will both compute the result
// and both write — the second write overwrites with identical data.
// Wasteful but harmless. No double-checked locking is in place
// because the inversion itself is deterministic and the duplication
// cost amortises away across realistic workloads.
func criticalValueAndBands(n int, alpha float64, method BandMethodE, algo CrossingAlgorithmE) (c float64, lower, upper []float64, err error) {
	return criticalValueAndBandsCtx(context.Background(), n, alpha, method, algo, nil)
}

// criticalValueAndBandsCtx is the cancellable, progress-reporting
// implementation behind criticalValueAndBands. ctx cancellation lands
// within one O(n²) eval; onProgress fires once per eval (see
// ProgressFunc). The cache lookup/store and α validation are identical
// to the wrapper — only the inversion gains ctx + progress.
func criticalValueAndBandsCtx(
	ctx context.Context, n int, alpha float64, method BandMethodE, algo CrossingAlgorithmE, onProgress ProgressFunc,
) (c float64, lower, upper []float64, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if n <= 0 {
		err = eh.Errorf("n must be positive, got %d", n)
		return
	}
	if alpha <= 0 || alpha >= 1 {
		err = eh.Errorf("alpha must lie strictly inside (0, 1), got %v", alpha)
		return
	}
	if math.IsNaN(alpha) {
		err = eh.Errorf("alpha must not be NaN")
		return
	}

	key := bandCacheKey{n: n, method: method, alphaBits: quantizeAlpha(alpha)}
	bandCacheMu.RLock()
	if e, ok := bandCache[key]; ok {
		bandCacheMu.RUnlock()
		c = e.c
		lower = append([]float64(nil), e.lower...)
		upper = append([]float64(nil), e.upper...)
		return
	}
	bandCacheMu.RUnlock()

	family := bandFamilyDispatch(method)
	if family == nil {
		err = eh.Errorf("unknown BandMethodE %d", method)
		return
	}

	c, lower, upper, err = invertCriticalValue(ctx, n, alpha, family, algo, onProgress)
	if err != nil {
		return
	}

	bandCacheMu.Lock()
	bandCache[key] = bandCacheEntry{
		c:     c,
		lower: append([]float64(nil), lower...),
		upper: append([]float64(nil), upper...),
	}
	bandCacheMu.Unlock()
	return
}

// invertCriticalValue performs the actual bisection given a band
// family and crossing-probability algorithm. Lifted out of
// criticalValueAndBands so the test suite can exercise it
// cache-free.
func invertCriticalValue(
	ctx context.Context, n int, alpha float64, family bandFamilyI, algo CrossingAlgorithmE, onProgress ProgressFunc,
) (c float64, lower, upper []float64, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target := 1 - alpha
	cLo, cHi := family.criticalValueBracket(n, alpha)
	if !(cLo < cHi) {
		err = eh.Errorf("%s: empty bracket (%v, %v)", family.name(), cLo, cHi)
		return
	}

	lower = make([]float64, n)
	upper = make([]float64, n)

	// total is the worst-case number of crossing-probability evals: two
	// bracket-validation evals plus the bisection iterations below. Each
	// eval is the same O(n²) cost, so reporting once per eval yields a
	// near-linear progress signal and a stable ETA. The eval closure
	// also checks ctx so a cancellation lands within one eval.
	const bisectIters = 60
	total := 2 + bisectIters
	done := 0
	eval := func(cAt float64) (p float64, err error) {
		if err = ctx.Err(); err != nil {
			err = eh.Errorf("ecdf band inversion cancelled after %d/%d evals: %w", done, total, err)
			return
		}
		family.boundaries(n, cAt, lower, upper)
		if p, err = CrossingProbability(lower, upper, algo); err != nil {
			return
		}
		done++
		if onProgress != nil {
			onProgress(done, total)
		}
		return
	}

	// Verify the bracket actually contains the target by computing
	// P at each endpoint. The lower endpoint should sit below the
	// target probability (the band is too narrow); the upper above.
	pLo, err := eval(cLo)
	if err != nil {
		return
	}
	pHi, err := eval(cHi)
	if err != nil {
		return
	}
	if pLo > target {
		err = eb.Build().
			Str("family", family.name()).
			Float64("cLo", cLo).
			Float64("pLo", pLo).
			Float64("target", target).
			Errorf("lower bracket already exceeds target P; widen the family's bracket")
		return
	}
	if pHi < target {
		err = eb.Build().
			Str("family", family.name()).
			Float64("cHi", cHi).
			Float64("pHi", pHi).
			Float64("target", target).
			Errorf("upper bracket falls short of target P; widen the family's bracket")
		return
	}

	// Bisect on c. The crossing probability is monotone increasing
	// in c for every family — wider bands enclose more outcomes.
	// 60 iterations sees us through to machine precision in c. pLo
	// and pHi are recomputed only at the bracket-validation step
	// above; inside the loop we only need pMid for comparison
	// against target.
	_ = pLo
	_ = pHi
	for range bisectIters {
		cMid := 0.5 * (cLo + cHi)
		if cMid <= cLo || cMid >= cHi {
			break
		}
		pMid, perr := eval(cMid)
		if perr != nil {
			err = perr
			return
		}
		if pMid < target {
			cLo = cMid
		} else {
			cHi = cMid
		}
	}
	c = 0.5 * (cLo + cHi)
	family.boundaries(n, c, lower, upper)
	if onProgress != nil {
		onProgress(total, total)
	}
	return
}

// bandCacheKey identifies one cached inversion result. The alpha
// component is the bit pattern of math.Round(alpha · 1e9) / 1e9,
// quantising user inputs that differ only in the last ULPs so they
// share an entry.
type bandCacheKey struct {
	n         int
	method    BandMethodE
	alphaBits uint64
}

type bandCacheEntry struct {
	c     float64
	lower []float64
	upper []float64
}

var (
	bandCacheMu sync.RWMutex
	bandCache   = map[bandCacheKey]bandCacheEntry{}
)

// quantizeAlpha rounds alpha to the nearest 1e-9 grid point and
// returns its bit pattern. Used as the cache key for α so adjacent
// floating-point neighbours hit the same entry.
func quantizeAlpha(alpha float64) uint64 {
	q := math.Round(alpha*1e9) / 1e9
	return math.Float64bits(q)
}

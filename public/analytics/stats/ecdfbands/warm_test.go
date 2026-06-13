package ecdfbands

import (
	"context"
	"math"
	"testing"
)

// TestLogFactorialTableMatchesLgamma asserts the memoised table returns
// exactly the math.Lgamma values it replaced (both inside the table and
// via the past-high-water fallback), so the tabulation is a pure
// speedup with no change to band values.
func TestLogFactorialTableMatchesLgamma(t *testing.T) {
	ensureLogFactorials(256)
	for k := 0; k <= 256; k++ {
		got := logFactorial(k)
		want := 0.0
		if k >= 2 {
			want, _ = math.Lgamma(float64(k + 1))
		}
		if math.Abs(got-want) > 1e-12*(1+math.Abs(want)) {
			t.Fatalf("logFactorial(%d) = %v, want %v", k, got, want)
		}
	}
	// Past the table high-water mark the fallback must still be correct.
	want5000, _ := math.Lgamma(float64(5001))
	if got := logFactorial(5000); math.Abs(got-want5000) > 1e-9*(1+math.Abs(want5000)) {
		t.Fatalf("logFactorial(5000) fallback = %v, want %v", got, want5000)
	}
}

// TestBandReadyWarmBand checks the non-blocking probe flips from false
// to true once WarmBand has populated the cache for that (n, α, method).
func TestBandReadyWarmBand(t *testing.T) {
	const n = 63
	const alpha = 0.0731 // unusual values: unlikely to be cached by other tests
	method := BandMethodBerkJones

	if BandReady(n, alpha, method) {
		t.Fatalf("BandReady true before any warm-up")
	}
	if err := WarmBand(context.Background(), n, alpha, method, nil); err != nil {
		t.Fatalf("WarmBand: %v", err)
	}
	if !BandReady(n, alpha, method) {
		t.Fatalf("BandReady false after WarmBand")
	}
}

// TestWarmBandProgress checks the progress callback fires monotonically
// in [0,total] and ends at total/total.
func TestWarmBandProgress(t *testing.T) {
	const n = 50
	const alpha = 0.041
	var calls, lastDone, lastTotal int
	err := WarmBand(context.Background(), n, alpha, BandMethodBerkJones,
		func(done, total int) {
			calls++
			if done < 0 || total <= 0 || done > total {
				t.Errorf("progress out of range: %d/%d", done, total)
			}
			lastDone, lastTotal = done, total
		})
	if err != nil {
		t.Fatalf("WarmBand: %v", err)
	}
	if calls == 0 {
		t.Fatalf("progress callback never fired")
	}
	if lastDone != lastTotal {
		t.Fatalf("final progress %d/%d not complete", lastDone, lastTotal)
	}
}

// TestWarmBandCancel checks a cancelled context aborts the solve with an
// error and caches nothing.
func TestWarmBandCancel(t *testing.T) {
	const n = 100
	const alpha = 0.037
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled: the first eval must bail

	if err := WarmBand(ctx, n, alpha, BandMethodBerkJones, nil); err == nil {
		t.Fatalf("expected error from cancelled WarmBand")
	}
	if BandReady(n, alpha, BandMethodBerkJones) {
		t.Fatalf("cancelled solve must not populate the cache")
	}
}

package implot

import (
	"math"
	"testing"
)

// An infinite sample, or a finite range whose span overflows, once gave a
// NaN bin index and panicked on counts[MinInt64].
func TestBinSamples_NonFinite(t *testing.T) {
	counts, lo, width, n := binSamples([]float64{0, 1, math.Inf(1), math.Inf(-1)}, 4, false)
	if n != 2 || lo != 0 || math.Abs(width-0.25) > 1e-12 {
		t.Errorf("Inf samples not dropped: n=%d lo=%v width=%v", n, lo, width)
	}
	sum := 0.0
	for _, cn := range counts {
		sum += cn
	}
	if sum != 2 {
		t.Errorf("counts sum %v, want 2", sum)
	}
	if counts, _, _, n = binSamples([]float64{-1e308, 1e308}, 4, false); n != 0 || counts != nil {
		t.Errorf("overflowing span: got n=%d counts=%v, want empty", n, counts)
	}
}

func TestBin2D_NonFinite(t *testing.T) {
	values, _, _, _, _, ok := bin2D([]float64{0, 1, math.Inf(1)}, []float64{0, 1, 0.5}, 2, 2)
	if !ok {
		t.Fatal("bin2D failed on finite pairs beside an Inf")
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	if sum != 2 {
		t.Errorf("values sum %v, want 2", sum)
	}
	if _, _, _, _, _, ok = bin2D([]float64{-1e308, 1e308}, []float64{0, 1}, 4, 4); ok {
		t.Error("overflowing x span: want ok=false")
	}
}

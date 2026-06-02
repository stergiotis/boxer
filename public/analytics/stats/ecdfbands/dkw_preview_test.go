package ecdfbands

import (
	"math"
	"testing"
)

// TestDkwBandForGrid covers the closed-form DKW preview band: the half-width
// matches the Massart formula, the band brackets F_n within [0,1], and the
// input contract is enforced.
func TestDkwBandForGrid(t *testing.T) {
	const alpha = 0.05
	for _, n := range []int{2, 50, 1000, 10000} {
		gridN := 64
		xs := make([]float64, gridN)
		fn := make([]float64, gridN)
		for i := range xs {
			xs[i] = float64(i)
			fn[i] = float64(i) / float64(gridN-1)
		}
		b, err := DkwBandForGrid(xs, fn, n, alpha)
		if err != nil {
			t.Fatalf("n=%d: unexpected error: %v", n, err)
		}
		wantEps := math.Sqrt(math.Log(2/alpha) / (2 * float64(n)))
		if math.Abs(b.CritC-wantEps) > 1e-12 {
			t.Errorf("n=%d: critC %.8g != closed-form ε %.8g", n, b.CritC, wantEps)
		}
		if b.Method != BandMethodDKW || b.N != n || b.Alpha != alpha {
			t.Errorf("n=%d: metadata mismatch method=%v N=%d alpha=%v", n, b.Method, b.N, b.Alpha)
		}
		for i := range fn {
			lo, hi := b.LowerCDF[i], b.UpperCDF[i]
			if lo < 0 || hi > 1 || lo > hi {
				t.Fatalf("n=%d i=%d: degenerate band [%.4f,%.4f]", n, i, lo, hi)
			}
			if lo > fn[i]+1e-12 || hi < fn[i]-1e-12 {
				t.Fatalf("n=%d i=%d: band [%.4f,%.4f] does not bracket fn=%.4f", n, i, lo, hi, fn[i])
			}
		}
	}
}

// TestDkwBandForGridIsConservativeVsExact pins the "preview" contract: the
// closed-form Massart band is at least as wide as the bisection-refined
// exact DKW band routed through BandsForGrid — i.e. it over-covers, never
// under-covers, so swapping to an exact band only ever tightens.
func TestDkwBandForGridIsConservativeVsExact(t *testing.T) {
	const n = 200 // small so the exact inversion is cheap
	const alpha = 0.05
	gridN := 32
	xs := make([]float64, gridN)
	fn := make([]float64, gridN)
	for i := range xs {
		xs[i] = float64(i)
		fn[i] = float64(i) / float64(gridN-1)
	}
	preview, err := DkwBandForGrid(xs, fn, n, alpha)
	if err != nil {
		t.Fatal(err)
	}
	exact, err := BandsForGrid(xs, fn, n, alpha, BandMethodDKW)
	if err != nil {
		t.Fatal(err)
	}
	for i := range fn {
		// preview must enclose the exact band at every grid point.
		if preview.LowerCDF[i] > exact.LowerCDF[i]+1e-9 || preview.UpperCDF[i] < exact.UpperCDF[i]-1e-9 {
			t.Errorf("i=%d: preview [%.5f,%.5f] does not enclose exact [%.5f,%.5f]",
				i, preview.LowerCDF[i], preview.UpperCDF[i], exact.LowerCDF[i], exact.UpperCDF[i])
		}
	}
}

func TestDkwBandForGridValidation(t *testing.T) {
	good := []float64{0, 0.5, 1}
	cases := []struct {
		name string
		xs   []float64
		fn   []float64
		n    int
		a    float64
	}{
		{"len mismatch", []float64{0, 1, 2}, []float64{0, 1}, 10, 0.05},
		{"n<=0", []float64{0, 1}, []float64{0, 1}, 0, 0.05},
		{"alpha<=0", good, good, 10, 0},
		{"alpha>=1", good, good, 10, 1},
		{"alpha NaN", good, good, 10, math.NaN()},
		{"fn>1", []float64{0, 1}, []float64{0, 1.5}, 10, 0.05},
		{"fn non-monotone", []float64{0, 1}, []float64{0.5, 0.2}, 10, 0.05},
	}
	for _, tc := range cases {
		if _, err := DkwBandForGrid(tc.xs, tc.fn, tc.n, tc.a); err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
		}
	}
}

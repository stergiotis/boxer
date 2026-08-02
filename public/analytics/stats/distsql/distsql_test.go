package distsql

import (
	"math"
	"slices"
	"strings"
	"testing"
)

func TestGridLevelsPinnedShape(t *testing.T) {
	g := GridLevels()
	if len(g) != GridLevelCount {
		t.Fatalf("GridLevels() has %d levels, GridLevelCount pins %d", len(g), GridLevelCount)
	}
	for i, p := range g {
		if !(p > 0 && p < 1) {
			t.Fatalf("level [%d] = %v outside (0,1)", i, p)
		}
		if i > 0 && !(p > g[i-1]) {
			t.Fatalf("levels not strictly ascending at [%d]: %v after %v", i, p, g[i-1])
		}
	}
	if err := ValidateSeries(g, g); err != nil {
		t.Fatalf("the default grid must validate against the contract: %v", err)
	}
}

func TestGridLevelsMembership(t *testing.T) {
	g := GridLevels()
	has := func(p float64) bool { return slices.Contains(g, p) }
	for k := 1; k <= gridDyadicMaxDepth; k++ {
		p := math.Ldexp(1, -k)
		if !has(p) || !has(1-p) {
			t.Fatalf("dyadic depth %d (%v / %v) missing", k, p, 1-p)
		}
	}
	for j := 1; j < gridUniformDenom; j++ {
		if !has(float64(j) / gridUniformDenom) {
			t.Fatalf("uniform level %d/%d missing", j, gridUniformDenom)
		}
	}
	for _, tail := range gridTailLevels {
		if !has(tail) || !has(1-tail) {
			t.Fatalf("tail level %v / %v missing", tail, 1-tail)
		}
	}
}

func TestValidateSeriesRejects(t *testing.T) {
	cases := []struct {
		name    string
		ps, qs  []float64
		wantSub string
	}{
		{"length mismatch", []float64{0.25, 0.5}, []float64{1}, "length mismatch"},
		{"too short", []float64{0.5}, []float64{1}, "grid too short"},
		{"p out of range", []float64{0.5, 1.0}, []float64{1, 2}, "outside (0, 1)"},
		{"p zero", []float64{0.0, 0.5}, []float64{1, 2}, "outside (0, 1)"},
		{"not ascending", []float64{0.5, 0.5}, []float64{1, 2}, "not strictly ascending"},
		{"qs decreasing", []float64{0.25, 0.5}, []float64{2, 1}, "not non-decreasing"},
	}
	for _, tc := range cases {
		err := ValidateSeries(tc.ps, tc.qs)
		if err == nil {
			t.Fatalf("%s: expected reject, got nil", tc.name)
		}
		if !strings.Contains(err.Error(), tc.wantSub) {
			t.Fatalf("%s: message %q does not contain %q", tc.name, err.Error(), tc.wantSub)
		}
	}
	if err := ValidateSeries([]float64{0.25, 0.5, 0.75}, []float64{1, 1, 2}); err != nil {
		t.Fatalf("ties in qs are legal (plateau): %v", err)
	}
}

func TestValidateHist(t *testing.T) {
	if err := ValidateHist([]float64{0, 1}, []float64{1, 2}, []float64{3, 4}); err != nil {
		t.Fatalf("valid triplet rejected: %v", err)
	}
	if err := ValidateHist([]float64{0}, []float64{1, 2}, []float64{3}); err == nil ||
		!strings.Contains(err.Error(), "lengths differ") {
		t.Fatalf("length mismatch not rejected: %v", err)
	}
	if err := ValidateHist([]float64{0, 1}, []float64{1, 1}, []float64{3, 4}); err == nil ||
		!strings.Contains(err.Error(), "non-positive width") {
		t.Fatalf("zero-width bin not rejected: %v", err)
	}
}

func TestGridOracleQuantile(t *testing.T) {
	ps := []float64{0.25, 0.5, 0.75}
	qs := []float64{10.0, 20.0, 40.0}
	o, err := NewGridOracle(ps, qs, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := o.Quantile(0.5); got != 20 {
		t.Fatalf("exact grid hit: got %v", got)
	}
	if got := o.Quantile(0.375); got != 15 {
		t.Fatalf("midpoint interp: got %v want 15", got)
	}
	if got := o.Quantile(0.01); got != 10 {
		t.Fatalf("below-grid clamp: got %v", got)
	}
	if got := o.Quantile(0.99); got != 40 {
		t.Fatalf("above-grid clamp: got %v", got)
	}
	if got := o.Count(); got != 100 {
		t.Fatalf("count: got %v", got)
	}
}

func TestGridOracleCDF(t *testing.T) {
	// Plateau at value 20 spanning p=0.5..0.75.
	ps := []float64{0.25, 0.5, 0.75, 0.9}
	qs := []float64{10.0, 20.0, 20.0, 40.0}
	o, err := NewGridOracle(ps, qs, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := o.CDF(20); got != 0.75 {
		t.Fatalf("plateau tie must resolve to upper p: got %v", got)
	}
	if got := o.CDF(15); got != 0.375 {
		t.Fatalf("strict-segment interp: got %v want 0.375", got)
	}
	if got := o.CDF(5); got != 0.25 {
		t.Fatalf("below-support clamp: got %v", got)
	}
	if got := o.CDF(50); got != 0.9 {
		t.Fatalf("above-support clamp: got %v", got)
	}
	// Round-trip on a strict segment.
	x := o.Quantile(0.3)
	if got := o.CDF(x); math.Abs(got-0.3) > 1e-12 {
		t.Fatalf("CDF(Quantile(0.3)) = %v", got)
	}
}

func TestDkwEpsilon(t *testing.T) {
	// Massart: n=100, alpha=0.05 → sqrt(ln(40)/200).
	want := math.Sqrt(math.Log(2/0.05) / 200)
	if got := DkwEpsilon(100, 0.05); math.Abs(got-want) > 1e-15 {
		t.Fatalf("got %v want %v", got, want)
	}
	if !math.IsInf(DkwEpsilon(0, 0.05), 1) {
		t.Fatal("n=0 must give +Inf")
	}
	if !math.IsNaN(DkwEpsilon(100, 0)) || !math.IsNaN(DkwEpsilon(100, 1)) {
		t.Fatal("alpha outside (0,1) must give NaN")
	}
}

func TestWasserstein1(t *testing.T) {
	ps := []float64{0.25, 0.5, 0.75}
	a := []float64{10.0, 20.0, 30.0}
	b := []float64{12.0, 22.0, 32.0} // constant shift of 2 over span 0.5
	if got := Wasserstein1(ps, a, b); math.Abs(got-1.0) > 1e-12 {
		t.Fatalf("constant shift: got %v want 1.0 (2 × span 0.5)", got)
	}
	if got := Wasserstein1(ps, a, a); got != 0 {
		t.Fatalf("identical: got %v", got)
	}
	if got := Wasserstein1(ps[:1], a[:1], b[:1]); !math.IsNaN(got) {
		t.Fatalf("short input must be NaN, got %v", got)
	}
}

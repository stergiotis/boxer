package riskviews

import (
	"math"
	"testing"
)

// The prototype's numbers are not available as fixtures (no JS engine in
// the loop), so these pin structural invariants and log the headline
// figures for comparison against the HTML page by eye.
func TestHeadlineFigures(t *testing.T) {
	if len(simRes) != simYears || len(simInh) != simYears {
		t.Fatalf("sample sizes %d %d", len(simRes), len(simInh))
	}
	for i := 1; i < simYears; i++ {
		if simRes[i] < simRes[i-1] {
			t.Fatal("simRes not sorted")
		}
	}
	lo, hi, ok := lecBreach()
	base, items := tornadoItems()
	t.Logf("EAL residual %s inherent %s; P(>=5m) residual %s; breach %v %s..%s; tornado base %s top %s %s..%s; brier %.3f -> %.3f",
		eur(ealResidual()), eur(ealInherent()), pct(exceed(simRes, 5e6), 1), ok, eur(lo), eur(hi),
		eur(base), items[0].Label, eur(items[0].Lo), eur(items[0].Hi), brier(calib[0]), brier(calib[1]))
	zero, beyond := 0, 0
	for _, v := range quantileDots(50) {
		if v < 1e3 {
			zero++
		}
		if v > reserve {
			beyond++
		}
	}
	t.Logf("dots: %d zero, %d beyond reserve; icons %v/%v", zero, beyond, math.Round(gmean(risks[0].IP)*100), math.Round(gmean(risks[0].P)*100))
	if math.Abs(ealResidual()-base) > 1e-6 {
		t.Fatalf("tornado base %v != EAL %v", base, ealResidual())
	}
}

func TestMulberry32(t *testing.T) {
	// The widely published first output of JS mulberry32(42).
	if v := mulberry32(42)(); math.Abs(v-0.6011037519201636) > 1e-15 {
		t.Fatalf("mulberry32(42)[0] = %.16f", v)
	}
}

func TestEur(t *testing.T) {
	for in, want := range map[float64]string{0: "€0", 1.5e6: "€1.5m", 12e6: "€12m", 2e6: "€2m", 450e3: "€450k", 999: "€999"} {
		if got := eur(in); got != want {
			t.Errorf("eur(%v) = %q want %q", in, got, want)
		}
	}
}

package ecdfbands

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBandMethodString pins the human-readable display names used in
// status lines/legends, and the unknown-value fallback (never blanks).
func TestBandMethodString(t *testing.T) {
	assert.Equal(t, "Berk-Jones", BandMethodBerkJones.String())
	assert.Equal(t, "DKW", BandMethodDKW.String())
	assert.Equal(t, "equal-precision", BandMethodEqualPrecision.String())
	assert.Equal(t, "higher-criticism", BandMethodHigherCriticism.String())
	assert.Contains(t, BandMethodE(250).String(), "unknown")
}

// TestBJBoundariesContainCentre verifies the structural invariant
// L_i ≤ p_i ≤ H_i — the band is centred on the i-th rank's null
// expectation.
func TestBJBoundariesContainCentre(t *testing.T) {
	for _, n := range []int{5, 20, 100} {
		lower := make([]float64, n)
		upper := make([]float64, n)
		berkJonesFamily{}.boundaries(n, 2.5, lower, upper)
		for i := range n {
			p := float64(i+1) / float64(n)
			assert.LessOrEqual(t, lower[i], p, "n=%d i=%d", n, i)
			assert.GreaterOrEqual(t, upper[i], p, "n=%d i=%d", n, i)
		}
	}
}

// TestBJBoundariesAreKLContours confirms that the bisection-derived
// edges lie exactly on the c/n KL level set: n · D(p_i ‖ q_l) = c
// and n · D(p_i ‖ q_u) = c to ulp-level tolerance.
func TestBJBoundariesAreKLContours(t *testing.T) {
	for _, n := range []int{10, 50, 200} {
		for _, c := range []float64{0.5, 2.0, 5.0} {
			lower := make([]float64, n)
			upper := make([]float64, n)
			berkJonesFamily{}.boundaries(n, c, lower, upper)
			target := c / float64(n)
			for i := range n {
				p := float64(i+1) / float64(n)
				if lower[i] > 0 {
					got := binomKL(p, lower[i])
					assert.InDelta(t, target, got, 1e-12,
						"lower n=%d i=%d c=%v: D(p ‖ q_l)=%v target=%v",
						n, i, c, got, target)
				}
				if upper[i] < 1 {
					got := binomKL(p, upper[i])
					assert.InDelta(t, target, got, 1e-12,
						"upper n=%d i=%d c=%v: D(p ‖ q_u)=%v target=%v",
						n, i, c, got, target)
				}
			}
		}
	}
}

// TestBJBoundariesAreMonotone validates that lower and upper edges
// non-decrease in i — required by the Steck/Moscovich engines.
func TestBJBoundariesAreMonotone(t *testing.T) {
	for _, n := range []int{5, 20, 100, 500} {
		for _, c := range []float64{0.5, 2.0, 5.0} {
			lower := make([]float64, n)
			upper := make([]float64, n)
			berkJonesFamily{}.boundaries(n, c, lower, upper)
			for i := 1; i < n; i++ {
				assert.LessOrEqual(t, lower[i-1], lower[i],
					"lower non-monotone n=%d c=%v at i=%d", n, c, i)
				assert.LessOrEqual(t, upper[i-1], upper[i],
					"upper non-monotone n=%d c=%v at i=%d", n, c, i)
			}
		}
	}
}

// TestBJBoundariesShrinkWithSmallerC ensures the band width is
// monotone-decreasing in c (smaller c → tighter band).
func TestBJBoundariesShrinkWithSmallerC(t *testing.T) {
	const n = 50
	lowerA := make([]float64, n)
	upperA := make([]float64, n)
	lowerB := make([]float64, n)
	upperB := make([]float64, n)
	berkJonesFamily{}.boundaries(n, 5.0, lowerA, upperA)
	berkJonesFamily{}.boundaries(n, 1.0, lowerB, upperB)
	for i := range n {
		assert.GreaterOrEqual(t, lowerB[i], lowerA[i],
			"smaller c should lift lower edge at i=%d", i)
		assert.LessOrEqual(t, upperB[i], upperA[i],
			"smaller c should drop upper edge at i=%d", i)
	}
}

// TestDKWBoundariesClosedForm checks the closed-form ε against
// the boundary positions emitted by dkwFamily.
func TestDKWBoundariesClosedForm(t *testing.T) {
	const n = 20
	const eps = 0.15
	lower := make([]float64, n)
	upper := make([]float64, n)
	dkwFamily{}.boundaries(n, eps, lower, upper)
	for i := range n {
		p := float64(i+1) / float64(n)
		wantLo := math.Max(0, p-eps)
		wantHi := math.Min(1, p-1.0/float64(n)+eps)
		assert.InDelta(t, wantLo, lower[i], 1e-15, "DKW lower at i=%d", i)
		assert.InDelta(t, wantHi, upper[i], 1e-15, "DKW upper at i=%d", i)
	}
}

// TestBandFamilyDispatch routes every BandMethodE value to the right
// implementation, and rejects unknown values.
func TestBandFamilyDispatch(t *testing.T) {
	cases := map[BandMethodE]string{
		BandMethodBerkJones:       "berkjones",
		BandMethodDKW:             "dkw",
		BandMethodEqualPrecision:  "equalprecision",
		BandMethodHigherCriticism: "highercriticism",
	}
	for m, want := range cases {
		f := bandFamilyDispatch(m)
		require.NotNil(t, f, "method %d", m)
		assert.Equal(t, want, f.name())
	}
	assert.Nil(t, bandFamilyDispatch(BandMethodE(99)))
}

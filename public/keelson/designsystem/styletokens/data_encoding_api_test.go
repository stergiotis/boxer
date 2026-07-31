// SPDX-License-Identifier: MIT

package styletokens_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/contrast"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/cvd"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens/data_encoding"
)

func TestSequentialEndpointsMatchLUT(t *testing.T) {
	tests := []struct {
		name    string
		palette styletokens.SequentialE
		lut     *[256][3]uint8
	}{
		{"batlow", styletokens.SequentialBatlow, &data_encoding.Batlow},
		{"lapaz", styletokens.SequentialLapaz, &data_encoding.Lapaz},
		{"viridis", styletokens.SequentialViridis, &data_encoding.Viridis},
		{"inferno", styletokens.SequentialInferno, &data_encoding.Inferno},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lo := styletokens.Sequential(tc.palette, 0.0)
			hi := styletokens.Sequential(tc.palette, 1.0)
			wantLo := tc.lut[0]
			wantHi := tc.lut[255]
			if lo.R != wantLo[0] || lo.G != wantLo[1] || lo.B != wantLo[2] {
				t.Errorf("t=0 want (%d,%d,%d) got (%d,%d,%d)",
					wantLo[0], wantLo[1], wantLo[2], lo.R, lo.G, lo.B)
			}
			if hi.R != wantHi[0] || hi.G != wantHi[1] || hi.B != wantHi[2] {
				t.Errorf("t=1 want (%d,%d,%d) got (%d,%d,%d)",
					wantHi[0], wantHi[1], wantHi[2], hi.R, hi.G, hi.B)
			}
			if lo.A != 0xFF || hi.A != 0xFF {
				t.Errorf("alpha want 0xFF got lo=%#x hi=%#x", lo.A, hi.A)
			}
		})
	}
}

func TestSequentialClampsOutOfRange(t *testing.T) {
	low := styletokens.Sequential(styletokens.SequentialBatlow, 0.0)
	neg := styletokens.Sequential(styletokens.SequentialBatlow, -0.5)
	if low != neg {
		t.Errorf("negative t should clamp to t=0: low=%+v neg=%+v", low, neg)
	}
	high := styletokens.Sequential(styletokens.SequentialBatlow, 1.0)
	big := styletokens.Sequential(styletokens.SequentialBatlow, 2.0)
	if high != big {
		t.Errorf("t>1 should clamp to t=1: high=%+v big=%+v", high, big)
	}
}

func TestDivergingMidpoint(t *testing.T) {
	mid := styletokens.Diverging(styletokens.DivergingVik, 0.0)
	lo := styletokens.Diverging(styletokens.DivergingVik, -1.0)
	hi := styletokens.Diverging(styletokens.DivergingVik, 1.0)
	if mid == lo || mid == hi {
		t.Errorf("vik midpoint should differ from endpoints: mid=%+v lo=%+v hi=%+v", mid, lo, hi)
	}
}

func TestQualitativeCycleWraps(t *testing.T) {
	n := styletokens.QualitativeCycleLen
	c0 := styletokens.QualitativeCycle(0)
	cN := styletokens.QualitativeCycle(n)
	c2N := styletokens.QualitativeCycle(2 * n)
	if c0 != cN || c0 != c2N {
		t.Errorf("qualitative cycle should wrap mod %d: c0=%+v cN=%+v c2N=%+v", n, c0, cN, c2N)
	}
	c3 := styletokens.QualitativeCycle(3)
	c3N := styletokens.QualitativeCycle(3 + n)
	if c3 != c3N {
		t.Errorf("qualitative cycle offset wrap: c3=%+v c3+N=%+v", c3, c3N)
	}
}

func TestQualitativeCycleAlphaOpaque(t *testing.T) {
	for i := range styletokens.QualitativeCycleLen {
		c := styletokens.QualitativeCycle(i)
		if c.A != 0xFF {
			t.Errorf("idx=%d alpha want 0xFF got %#x", i, c.A)
		}
	}
}

// TestQualitativeCyclePairwiseDistinct guards the qualitative palette's core
// contract: every pair of cycle entries must be tellable apart. The floor is
// perceptual (OKLab ΔE) rather than RGB-Euclidean — the RGB metric this
// replaced passed a palette whose worst pair was ΔE 4.9, because equal RGB
// steps are not equal perceptual steps. ADR-0156 §SD3 sets the floor.
func TestQualitativeCyclePairwiseDistinct(t *testing.T) {
	const minDeltaE = 15.0
	n := styletokens.QualitativeCycleLen
	for i := range n {
		for j := i + 1; j < n; j++ {
			a := styletokens.QualitativeCycle(i)
			b := styletokens.QualitativeCycle(j)
			de := cvd.DeltaEOklab(a.R, a.G, a.B, b.R, b.G, b.B)
			if de <= minDeltaE {
				t.Errorf("cycle entries %d and %d too close: (%d,%d,%d) vs (%d,%d,%d), ΔE=%.2f ≤ %.1f",
					i, j, a.R, a.G, a.B, b.R, b.G, b.B, de, minDeltaE)
			}
		}
	}
}

// TestQualitativeCyclePairwiseDistinctCVD is the same contract under
// simulated dichromacy. The floor is lower than the normal-vision one and
// is empirical, not perceptual: no qualitative palette of this cardinality
// reaches ΔE 15 once dichromacy collapses a colour axis — the semantic
// palette measures 0.2–0.5 under the same simulation. 6.0 is set below what
// the shipped palette achieves (min 7.5) and above every candidate ADR-0156
// §SD4 rejected, so it catches a regression without pretending to a
// threshold the literature does not supply.
func TestQualitativeCyclePairwiseDistinctCVD(t *testing.T) {
	const minDeltaE = 6.0
	n := styletokens.QualitativeCycleLen
	for _, cond := range []cvd.Type{cvd.Deuteranopia, cvd.Protanopia, cvd.Tritanopia} {
		for i := range n {
			for j := i + 1; j < n; j++ {
				a := styletokens.QualitativeCycle(i)
				b := styletokens.QualitativeCycle(j)
				ar, ag, ab := cvd.Simulate(cond, a.R, a.G, a.B)
				br, bg, bb := cvd.Simulate(cond, b.R, b.G, b.B)
				de := cvd.DeltaEOklab(ar, ag, ab, br, bg, bb)
				if de <= minDeltaE {
					t.Errorf("%s: cycle entries %d and %d too close: ΔE=%.2f ≤ %.1f",
						cond, i, j, de, minDeltaE)
				}
			}
		}
	}
}

// TestQualitativeCycleContrastOnSurface is the defect ADR-0156 fixes: every
// cycle entry must be legible as a foreground on the IDS spine. The gate is
// WCAG 1.4.11 (3:1 for graphical objects). NeutralBgSurface is the binding
// case — it is the lightest of the three dark surfaces, so clearing it
// clears NeutralBgPanel and NeutralBgFaint too.
func TestQualitativeCycleContrastOnSurface(t *testing.T) {
	floor := contrast.AAFloor(contrast.KindUI)
	bg := styletokens.NeutralBgSurface
	for i := range styletokens.QualitativeCycleLen {
		c := styletokens.QualitativeCycle(i)
		r := contrast.Ratio(c.R, c.G, c.B, bg.R, bg.G, bg.B)
		if r < floor {
			t.Errorf("slot %d (#%02x%02x%02x) is %.2f:1 on NeutralBgSurface, want ≥ %.1f:1",
				i, c.R, c.G, c.B, r, floor)
		}
	}
}

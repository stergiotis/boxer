//go:build llm_generated_opus47

// SPDX-License-Identifier: MIT

package styletokens_test

import (
	"testing"

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
	c0 := styletokens.QualitativeCycle(0)
	c10 := styletokens.QualitativeCycle(10)
	c20 := styletokens.QualitativeCycle(20)
	if c0 != c10 || c0 != c20 {
		t.Errorf("qualitative cycle should wrap mod 10: c0=%+v c10=%+v c20=%+v", c0, c10, c20)
	}
	c3 := styletokens.QualitativeCycle(3)
	c13 := styletokens.QualitativeCycle(13)
	if c3 != c13 {
		t.Errorf("qualitative cycle offset wrap: c3=%+v c13=%+v", c3, c13)
	}
}

func TestQualitativeCycleAlphaOpaque(t *testing.T) {
	for i := 0; i < 10; i++ {
		c := styletokens.QualitativeCycle(i)
		if c.A != 0xFF {
			t.Errorf("idx=%d alpha want 0xFF got %#x", i, c.A)
		}
	}
}

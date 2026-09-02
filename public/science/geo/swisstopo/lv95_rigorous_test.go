package swisstopo

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// rigorousToleranceMeters is the agreement demanded against the REFRAME
// reference points shared with lv95_test.go. The floor is not the projection,
// which reproduces PROJ's EPSG:2056 pipeline to under a micrometre, but the
// three-parameter datum shift: REFRAME uses a triangulated transformation, and
// the two disagree by up to ~1.5 cm across the national extent. Tightening this
// constant is only possible by adopting REFRAME's grid, not by better formulas.
const rigorousToleranceMeters = 0.02

func TestLV95ToWGS84Rigorous_ReferencePoints(t *testing.T) {
	for _, rp := range referencePoints {
		t.Run(rp.name, func(t *testing.T) {
			got := LV95ToWGS84Rigorous(rp.lv95)
			d := distanceMeters(got, rp.wgs)
			assert.LessOrEqual(t, d, rigorousToleranceMeters,
				"got %v want %v (%.4f m apart)", got, rp.wgs, d)
		})
	}
}

func TestWGS84ToLV95Rigorous_ReferencePoints(t *testing.T) {
	for _, rp := range referencePoints {
		t.Run(rp.name, func(t *testing.T) {
			got := WGS84ToLV95Rigorous(rp.wgs)
			assert.InDelta(t, rp.lv95.E, got.E, rigorousToleranceMeters)
			assert.InDelta(t, rp.lv95.N, got.N, rigorousToleranceMeters)
		})
	}
}

// TestRigorousBeatsApproximate pins the reason both implementations exist: the
// series in LV95ToWGS84 is correct to about a metre, and the closed form is
// three orders of magnitude closer to the same reference points.
func TestRigorousBeatsApproximate(t *testing.T) {
	worstApprox := 0.0
	worstRigorous := 0.0
	for _, rp := range referencePoints {
		if d := distanceMeters(LV95ToWGS84(rp.lv95), rp.wgs); d > worstApprox {
			worstApprox = d
		}
		if d := distanceMeters(LV95ToWGS84Rigorous(rp.lv95), rp.wgs); d > worstRigorous {
			worstRigorous = d
		}
	}
	t.Logf("worst deviation: approximate %.4f m, rigorous %.6f m", worstApprox, worstRigorous)
	require.Greater(t, worstApprox, 0.1, "the approximate formula is expected to deviate")
	require.Less(t, worstRigorous, rigorousToleranceMeters)
	require.Less(t, worstRigorous, worstApprox)
}

func TestRigorousRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// The Swiss national extent, comfortably inside the projection's domain.
		e := rapid.Float64Range(2_480_000, 2_840_000).Draw(rt, "E")
		n := rapid.Float64Range(1_070_000, 1_300_000).Draw(rt, "N")
		in := LV95Coord{E: e, N: n}
		back := WGS84ToLV95Rigorous(LV95ToWGS84Rigorous(in))
		// The round trip does not close exactly, and cannot: a
		// two-dimensional datum shift assumes zero ellipsoidal height in
		// both directions, so the return leg does not see the height the
		// forward leg implies. PROJ's push/pop of the vertical component
		// behaves the same way. The residual stays inside a few
		// millimetres across the national extent.
		require.InDelta(rt, in.E, back.E, 5e-3)
		require.InDelta(rt, in.N, back.N, 5e-3)
	})
}

func TestRigorousFundamentalPoint(t *testing.T) {
	// The fundamental point maps to the false origin by construction.
	lv := WGS84ToLV95Rigorous(WGS84Coord{Lat: fundamentalLatCH, Lon: fundamentalLonCH})
	// Bern's fundamental point is defined on Bessel; the WGS84 coordinate of
	// the same physical point differs, so only the projection stage is exact
	// here. Assert via the projection directly.
	phi := fundamentalLatCH * math.Pi / 180
	lambda := fundamentalLonCH * math.Pi / 180
	origin := swissProjection.forwardProjection(phi, lambda)
	assert.InDelta(t, lv95FalseE, origin.E, 1e-6)
	assert.InDelta(t, lv95FalseN, origin.N, 1e-6)
	assert.InDelta(t, lv95FalseE, lv.E, 500, "datum shift moves the point by a few hundred metres")
}

func BenchmarkLV95ToWGS84Rigorous(b *testing.B) {
	lv := LV95Coord{E: 2_600_000, N: 1_200_000}
	for b.Loop() {
		_ = LV95ToWGS84Rigorous(lv)
	}
}

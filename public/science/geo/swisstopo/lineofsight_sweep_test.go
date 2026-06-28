package swisstopo

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSweepAngleOffsets(t *testing.T) {
	tests := []struct {
		name      string
		halfRange float64
		step      float64
		want      []float64
	}{
		{"symmetric-2deg-half", 2, 0.5, []float64{-2, -1.5, -1, -0.5, 0, 0.5, 1, 1.5, 2}},
		{"zero-range-single", 0, 0.5, []float64{0}},
		{"nonmultiple-floors", 1, 0.4, []float64{-0.8, -0.4, 0, 0.4, 0.8}},
		{"zero-step-single", 2, 0, []float64{0}},
		{"single-step-each-side", 1, 1, []float64{-1, 0, 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sweepAngleOffsets(tc.halfRange, tc.step)
			require.Equal(t, len(tc.want), len(got), "offset count")
			for i := range tc.want {
				assert.InDelta(t, tc.want[i], got[i], 1e-9, "offset[%d]", i)
			}
		})
	}
}

func TestRotateTargetAbout(t *testing.T) {
	from := LV95Coord{E: 2_600_000, N: 1_200_000}
	to := LV95Coord{E: 2_601_000, N: 1_200_000} // 1000 m due grid-east

	t.Run("zero-deg-reproduces-target", func(t *testing.T) {
		got := rotateTargetAbout(from, to, 0)
		assert.InDelta(t, to.E, got.E, 1e-6)
		assert.InDelta(t, to.N, got.N, 1e-6)
	})

	t.Run("preserves-range", func(t *testing.T) {
		r0 := math.Hypot(to.E-from.E, to.N-from.N)
		for _, deg := range []float64{-90, -2, 0, 0.5, 30, 90, 180} {
			g := rotateTargetAbout(from, to, deg)
			r := math.Hypot(g.E-from.E, g.N-from.N)
			assert.InDelta(t, r0, r, 1e-6, "range at %g deg", deg)
		}
	})

	t.Run("ccw-90-maps-east-to-north", func(t *testing.T) {
		// +90° CCW maps grid-east (dE=+1000, dN=0) to grid-north
		// (dE=0, dN=+1000).
		g := rotateTargetAbout(from, to, 90)
		assert.InDelta(t, from.E, g.E, 1e-6)
		assert.InDelta(t, from.N+1000, g.N, 1e-6)
	})
}

func TestLineOfSightSweep_Validation(t *testing.T) {
	// A sampler over an empty temp dir suffices: validation happens
	// before any tile read.
	s, err := NewElevationSampler(context.Background(), t.TempDir())
	require.NoError(t, err)

	from := LV95Coord{E: 2_600_000, N: 1_200_000}
	to := LV95Coord{E: 2_601_000, N: 1_200_000}

	_, err = s.LineOfSightSweep(from, 1.7, to, 0, 2, 0)
	assert.Error(t, err, "stepDeg=0 with halfRange>0 must error")

	_, err = s.LineOfSightSweep(from, 1.7, to, 0, -1, 0.5)
	assert.Error(t, err, "negative halfRange must error")
}

func TestLineOfSightSweep_Structure(t *testing.T) {
	sampler := newTestSampler(t) // skips when tiles absent

	from := LV95Coord{E: 2_600_000, N: 1_200_500}
	to := LV95Coord{E: 2_600_998, N: 1_200_500}

	res, err := sampler.LineOfSightSweep(from, 1.7, to, 0, 2, 0.5)
	require.NoError(t, err)

	// 9 rays: -2 … +2 step 0.5.
	require.Len(t, res.AngleDeg, 9)
	require.Len(t, res.Rays, 9)
	require.Len(t, res.Targets, 9)

	// Ascending, symmetric, centre is 0.
	assert.InDelta(t, -2.0, res.AngleDeg[0], 1e-9)
	assert.InDelta(t, 0.0, res.AngleDeg[4], 1e-9)
	assert.InDelta(t, 2.0, res.AngleDeg[8], 1e-9)

	// Shared distance axis: every ray has the same ProfileDist length and
	// total distance.
	base := res.Rays[4].ProfileDist
	require.Greater(t, len(base), 1)
	for i, ray := range res.Rays {
		require.Equalf(t, len(base), len(ray.ProfileDist), "ray %d length", i)
		assert.InDeltaf(t, base[len(base)-1], ray.ProfileDist[len(ray.ProfileDist)-1], 1e-6, "ray %d total dist", i)
	}

	// The centre ray (offset 0) reproduces a direct LineOfSight.
	direct, err := sampler.LineOfSight(from, 1.7, to, 0)
	require.NoError(t, err)
	require.Equal(t, len(direct.ProfileElev), len(res.Rays[4].ProfileElev))
	for i := range direct.ProfileElev {
		assert.InDeltaf(t, direct.ProfileElev[i], res.Rays[4].ProfileElev[i], 1e-3, "centre terrain[%d]", i)
	}
	assert.Equal(t, direct.Visible, res.Rays[4].Visible)
}

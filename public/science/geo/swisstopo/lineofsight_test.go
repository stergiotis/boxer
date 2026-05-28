//go:build llm_generated_opus46

package swisstopo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLineOfSight_Visible(t *testing.T) {
	sampler := newTestSampler(t)

	// Bern Münster tower (100m above ground) to Gurten TV tower (30m above ground)
	// ~3.2km, verified VISIBLE in manual testing
	from := WGS84ToLV95(WGS84Coord{Lat: 46.9474, Lon: 7.4515})
	to := WGS84ToLV95(WGS84Coord{Lat: 46.9214, Lon: 7.4343})

	result, err := sampler.LineOfSight(from, 100, to, 30)
	require.NoError(t, err)

	t.Logf("from elev: %.1f m, to elev: %.1f m", result.FromElev, result.ToElev)
	t.Logf("profile points: %d", len(result.ProfileDist))
	assert.True(t, result.Visible, "Bern Münster +100m → Gurten +30m must be visible")

	{ // verify profile metadata
		assert.Greater(t, len(result.ProfileDist), 100)
		assert.Greater(t, float64(result.FromElev), 500.0)
		assert.Less(t, float64(result.FromElev), 600.0)
		assert.Greater(t, float64(result.ToElev), 650.0)
		assert.Less(t, float64(result.ToElev), 900.0)
	}
}

func TestLineOfSight_Obstructed_PilatusToRigi(t *testing.T) {
	sampler := newTestSampler(t)

	// Pilatus peak (2129m) to Rigi Kulm (1797m), ~20.7km
	// Verified OBSTRUCTED by the Pilatus ridge at ~1.3km
	from := WGS84ToLV95(WGS84Coord{Lat: 46.9739091, Lon: 8.2411592})
	to := WGS84ToLV95(WGS84Coord{Lat: 47.0567, Lon: 8.4853})

	result, err := sampler.LineOfSight(from, 1.7, to, 0)
	require.NoError(t, err)

	t.Logf("from elev: %.1f m, to elev: %.1f m", result.FromElev, result.ToElev)
	t.Logf("obstruction at %.1f m, elev %.1f m", result.ObstructionDist, result.ObstructionElev)

	assert.False(t, result.Visible, "Pilatus peak to Rigi must be obstructed")

	{ // verify terrain elevations match known values
		assert.InDelta(t, 2129.0, float64(result.FromElev), 5.0, "Pilatus peak elevation")
		assert.InDelta(t, 1797.0, float64(result.ToElev), 5.0, "Rigi Kulm elevation")
	}

	{ // obstruction should be within first ~2km (the Pilatus ridge)
		assert.Greater(t, result.ObstructionDist, 0.0)
		assert.Less(t, result.ObstructionDist, 2000.0, "obstruction must be near Pilatus ridge")
	}
}

func TestLineOfSight_Obstructed_RigiToBurgenstock(t *testing.T) {
	sampler := newTestSampler(t)

	// Rigi Kulm to Bürgenstock, ~10.3km
	// Verified OBSTRUCTED by Rigi's own ridge at ~1.5km
	from := WGS84ToLV95(WGS84Coord{Lat: 47.0567, Lon: 8.4853})
	to := WGS84ToLV95(WGS84Coord{Lat: 46.9985, Lon: 8.3797})

	result, err := sampler.LineOfSight(from, 1.7, to, 0)
	require.NoError(t, err)

	assert.False(t, result.Visible, "Rigi to Bürgenstock must be obstructed")
	assert.Greater(t, result.ObstructionDist, 0.0)
	assert.Less(t, result.ObstructionDist, 3000.0, "obstruction must be near Rigi ridge")
}

func TestLineOfSight_ProfileArrays(t *testing.T) {
	sampler := newTestSampler(t)

	from := LV95Coord{E: 2_600_000, N: 1_200_500}
	to := LV95Coord{E: 2_600_500, N: 1_200_500}

	result, err := sampler.LineOfSight(from, 1.7, to, 0)
	require.NoError(t, err)

	{ // verify all three arrays have same length
		n := len(result.ProfileDist)
		assert.Equal(t, n, len(result.ProfileElev))
		assert.Equal(t, n, len(result.LOSElev))
		assert.Greater(t, n, 100)
	}

	{ // verify LOS line is linear interpolation
		n := len(result.LOSElev)
		firstLOS := result.LOSElev[0]
		lastLOS := result.LOSElev[n-1]
		midLOS := result.LOSElev[n/2]
		expectedMid := (firstLOS + lastLOS) / 2
		assert.InDelta(t, expectedMid, midLOS, 1.0, "LOS midpoint must be linear average")
	}

	{ // verify LOS start and end include observer/target heights
		assert.InDelta(t, float64(result.FromElev)+1.7, float64(result.LOSElev[0]), 0.1)
		n := len(result.LOSElev)
		assert.InDelta(t, float64(result.ToElev)+0.0, float64(result.LOSElev[n-1]), 0.1)
	}
}

func TestLineOfSight_SamePoint(t *testing.T) {
	sampler := newTestSampler(t)

	pt := LV95Coord{E: 2_600_500, N: 1_200_500}
	result, err := sampler.LineOfSight(pt, 1.7, pt, 0)
	require.NoError(t, err)
	assert.True(t, result.Visible, "same-point LOS must be visible")
}

func TestLineOfSight_HeightMatters(t *testing.T) {
	sampler := newTestSampler(t)

	// Bern Münster to Gurten: obstructed at ground level, visible with height
	from := WGS84ToLV95(WGS84Coord{Lat: 46.9474, Lon: 7.4515})
	to := WGS84ToLV95(WGS84Coord{Lat: 46.9214, Lon: 7.4343})

	{ // ground level: should be obstructed (Gurten is uphill)
		result, err := sampler.LineOfSight(from, 0, to, 0)
		require.NoError(t, err)
		assert.False(t, result.Visible, "ground-to-ground Bern→Gurten must be obstructed")
	}

	{ // 100m tower + 30m target: should be visible (verified manually)
		result, err := sampler.LineOfSight(from, 100, to, 30)
		require.NoError(t, err)
		assert.True(t, result.Visible, "Bern +100m → Gurten +30m must be visible")
	}
}

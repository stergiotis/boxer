package swisstopo

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSampler(t *testing.T) *ElevationSampler {
	t.Helper()
	tilesAvailable(t)
	sampler, err := NewElevationSampler(context.Background(), testTilesDir)
	require.NoError(t, err)
	return sampler
}

// Reference elevations from swisstopo REST API
// https://api3.geo.admin.ch/rest/services/height?easting=E&northing=N
type elevRef struct {
	name    string
	lv95    LV95Coord
	apiElev float64 // from swisstopo height API (meters)
}

var elevRefs = []elevRef{
	{"Bern center", LV95Coord{E: 2_600_500, N: 1_200_500}, 538.7},
	{"Pilatus peak", LV95Coord{E: 2_661_066, N: 1_202_850}, 2125.2},
	{"Rigi Kulm", LV95Coord{E: 2_679_520, N: 1_212_272}, 1797.0},
}

func TestElevationSampler_Sample_ReferencePoints(t *testing.T) {
	sampler := newTestSampler(t)

	for _, ref := range elevRefs {
		t.Run(ref.name, func(t *testing.T) {
			elev, err := sampler.Sample(ref.lv95)
			require.NoError(t, err)
			t.Logf("%s: sampled=%.2f API=%.1f", ref.name, elev, ref.apiElev)
			// The REST API uses a different DEM product (DTM2) which may differ
			// slightly from the COG tile value. Allow 5m tolerance.
			assert.InDelta(t, ref.apiElev, float64(elev), 5.0,
				"elevation at %s differs from API by > 5m", ref.name)
		})
	}
}

func TestElevationSampler_Sample_WGS84RoundTrip(t *testing.T) {
	sampler := newTestSampler(t)

	// Convert WGS84 → LV95 → sample, verifying the full pipeline
	wgs := WGS84Coord{Lat: 46.951082876677035, Lon: 7.438632495274896} // Bern origin
	lv := WGS84ToLV95(wgs)
	elev, err := sampler.Sample(lv)
	require.NoError(t, err)
	t.Logf("Bern origin: LV95=%s elev=%.2f", lv, elev)
	assert.Greater(t, float64(elev), 400.0, "Bern elevation must be > 400m")
	assert.Less(t, float64(elev), 700.0, "Bern elevation must be < 700m")
}

func TestElevationSampler_SampleProfile(t *testing.T) {
	sampler := newTestSampler(t)

	from := LV95Coord{E: 2_600_000, N: 1_200_500}
	to := LV95Coord{E: 2_600_998, N: 1_200_500}

	distances, elevations, err := sampler.SampleProfile(from, to, 10.0)
	require.NoError(t, err)

	t.Logf("profile: %d points over %.1f m", len(distances), distances[len(distances)-1])

	{ // verify basic profile properties
		require.Greater(t, len(distances), 50, "profile must have > 50 points for ~1km")
		assert.InDelta(t, 0.0, distances[0], 0.001, "first distance must be 0")
		assert.InDelta(t, 998.0, distances[len(distances)-1], 10.0, "last distance must be ~998m")
		assert.Equal(t, len(distances), len(elevations), "parallel arrays must have same length")
	}

	{ // verify all elevations are in sane range for Bern area
		for i, elev := range elevations {
			assert.Greater(t, float64(elev), 400.0, "elevation[%d] too low", i)
			assert.Less(t, float64(elev), 700.0, "elevation[%d] too high", i)
		}
	}

	{ // verify distances are monotonically increasing
		for i := 1; i < len(distances); i++ {
			assert.Greater(t, distances[i], distances[i-1], "distances must be monotonically increasing at index %d", i)
		}
	}
}

func TestElevationSampler_SampleProfile_CrossTileBoundary(t *testing.T) {
	sampler := newTestSampler(t)

	// Profile that crosses a tile boundary (E from 2600xxx to 2601xxx)
	from := LV95Coord{E: 2_600_800, N: 1_200_500}
	to := LV95Coord{E: 2_601_200, N: 1_200_500}

	distances, elevations, err := sampler.SampleProfile(from, to, 2.0)
	require.NoError(t, err)

	t.Logf("cross-tile profile: %d points", len(distances))

	{ // verify no NaN or extreme values at tile boundary
		for i, elev := range elevations {
			assert.False(t, math.IsNaN(float64(elev)), "NaN at index %d (dist=%.1f)", i, distances[i])
			assert.False(t, math.IsInf(float64(elev), 0), "Inf at index %d", i)
			assert.Greater(t, float64(elev), 300.0, "elevation[%d] implausibly low", i)
			assert.Less(t, float64(elev), 800.0, "elevation[%d] implausibly high", i)
		}
	}

	{ // verify no discontinuity > 20m at consecutive points (2m apart in Bern is gentle terrain)
		for i := 1; i < len(elevations); i++ {
			diff := math.Abs(float64(elevations[i] - elevations[i-1]))
			assert.Less(t, diff, 20.0, "elevation jump > 20m between consecutive 2m samples at index %d", i)
		}
	}
}

func TestElevationSampler_SampleProfile_SamePoint(t *testing.T) {
	sampler := newTestSampler(t)

	pt := LV95Coord{E: 2_600_500, N: 1_200_500}
	distances, elevations, err := sampler.SampleProfile(pt, pt, 2.0)
	require.NoError(t, err)
	assert.Equal(t, 1, len(distances))
	assert.Equal(t, 1, len(elevations))
	assert.InDelta(t, 0.0, distances[0], 0.001)
}

func TestTileGridKey(t *testing.T) {
	tests := []struct {
		lv95     LV95Coord
		expectE  int32
		expectN  int32
	}{
		{LV95Coord{E: 2_600_000, N: 1_200_000}, 2600, 1200},
		{LV95Coord{E: 2_600_999, N: 1_200_999}, 2600, 1200},
		{LV95Coord{E: 2_601_000, N: 1_201_000}, 2601, 1201},
		{LV95Coord{E: 2_500_000, N: 1_100_000}, 2500, 1100},
		{LV95Coord{E: 2_750_999, N: 1_299_999}, 2750, 1299},
	}

	for _, tc := range tests {
		eKm, nKm := tileGridKey(tc.lv95)
		assert.Equal(t, tc.expectE, eKm, "eKm for %s", tc.lv95)
		assert.Equal(t, tc.expectN, nKm, "nKm for %s", tc.lv95)
	}
}

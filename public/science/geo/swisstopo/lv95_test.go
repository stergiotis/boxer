//go:build llm_generated_opus46

package swisstopo

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Reference points obtained from the official swisstopo REFRAME API
// (https://geodesy.geo.admin.ch/reframe/lv95towgs84).
// These are the ground truth for testing.
//
// The approximate formulas achieve sub-meter accuracy near the center of
// Switzerland and degrade to ~5m at the extreme corners. The tolerance
// field reflects the documented accuracy profile.
type refPoint struct {
	name      string
	lv95      LV95Coord
	wgs       WGS84Coord
	tolerance float64 // meters
}

var referencePoints = []refPoint{
	{
		name:      "Bern (origin)",
		lv95:      LV95Coord{E: 2_600_000, N: 1_200_000},
		wgs:       WGS84Coord{Lat: 46.951082876677035, Lon: 7.438632495274896},
		tolerance: 1.0,
	},
	{
		name:      "Rigi",
		lv95:      LV95Coord{E: 2_679_520, N: 1_212_273},
		wgs:       WGS84Coord{Lat: 47.056713687688273, Lon: 8.485305251305626},
		tolerance: 1.0,
	},
	{
		name:      "Matterhorn",
		lv95:      LV95Coord{E: 2_617_306, N: 1_091_805},
		wgs:       WGS84Coord{Lat: 45.977594088646363, Lon: 7.661934549499883},
		tolerance: 1.0,
	},
	{
		name:      "SE corner (Ticino)",
		lv95:      LV95Coord{E: 2_700_000, N: 1_100_000},
		wgs:       WGS84Coord{Lat: 46.044130338699667, Lon: 8.730497075536219},
		tolerance: 1.0,
	},
	{
		// extreme corner — approximate formula degrades to ~5m here
		name:      "NW corner (Jura)",
		lv95:      LV95Coord{E: 2_500_000, N: 1_300_000},
		wgs:       WGS84Coord{Lat: 47.842819172997942, Lon: 6.102749711571803},
		tolerance: 5.0,
	},
	{
		name:      "NE (Appenzell)",
		lv95:      LV95Coord{E: 2_750_000, N: 1_250_000},
		wgs:       WGS84Coord{Lat: 47.383742437194741, Lon: 9.425263056460189},
		tolerance: 1.0,
	},
	{
		name:      "SW (Lac Léman)",
		lv95:      LV95Coord{E: 2_550_000, N: 1_150_000},
		wgs:       WGS84Coord{Lat: 46.499437472808104, Lon: 6.787305984882277},
		tolerance: 1.0,
	},
	{
		name:      "Central (Lucerne)",
		lv95:      LV95Coord{E: 2_660_000, N: 1_185_000},
		wgs:       WGS84Coord{Lat: 46.813454372768454, Lon: 8.224800070158967},
		tolerance: 1.0,
	},
	{
		name:      "South of Bern",
		lv95:      LV95Coord{E: 2_600_000, N: 1_100_000},
		wgs:       WGS84Coord{Lat: 46.051531678272873, Lon: 7.438648049437290},
		tolerance: 1.0,
	},
	{
		name:      "North of Bern",
		lv95:      LV95Coord{E: 2_600_000, N: 1_300_000},
		wgs:       WGS84Coord{Lat: 47.850492443371422, Lon: 7.438616173366945},
		tolerance: 2.0,
	},
}

// distanceMeters returns the approximate surface distance in meters between two WGS84 points
// using the equirectangular approximation (sufficient for <1km distances in Switzerland).
func distanceMeters(a WGS84Coord, b WGS84Coord) float64 {
	const metersPerDegreeLat = 111_320.0
	midLat := (a.Lat + b.Lat) / 2.0
	metersPerDegreeLon := metersPerDegreeLat * math.Cos(midLat*math.Pi/180)

	dLat := (a.Lat - b.Lat) * metersPerDegreeLat
	dLon := (a.Lon - b.Lon) * metersPerDegreeLon
	return math.Sqrt(dLat*dLat + dLon*dLon)
}

func TestLV95ToWGS84_ReferencePoints(t *testing.T) {
	for _, rp := range referencePoints {
		t.Run(rp.name, func(t *testing.T) {
			got := LV95ToWGS84(rp.lv95)
			d := distanceMeters(got, rp.wgs)
			t.Logf("expected: %s", rp.wgs)
			t.Logf("got:      %s", got)
			t.Logf("error:    %.3f m (tolerance: %.1f m)", d, rp.tolerance)
			assert.Less(t, d, rp.tolerance, "error exceeds tolerance")
		})
	}
}

func TestWGS84ToLV95_ReferencePoints(t *testing.T) {
	for _, rp := range referencePoints {
		t.Run(rp.name, func(t *testing.T) {
			got := WGS84ToLV95(rp.wgs)
			dE := math.Abs(got.E - rp.lv95.E)
			dN := math.Abs(got.N - rp.lv95.N)
			d := math.Sqrt(dE*dE + dN*dN)
			t.Logf("expected: %s", rp.lv95)
			t.Logf("got:      %s", got)
			t.Logf("error:    %.3f m (dE=%.3f dN=%.3f, tolerance: %.1f m)", d, dE, dN, rp.tolerance)
			assert.Less(t, d, rp.tolerance, "error exceeds tolerance")
		})
	}
}

func TestRoundTrip_WGS84_LV95_WGS84(t *testing.T) {
	for _, rp := range referencePoints {
		t.Run(rp.name, func(t *testing.T) {
			lv := WGS84ToLV95(rp.wgs)
			back := LV95ToWGS84(lv)
			d := distanceMeters(back, rp.wgs)
			t.Logf("original:    %s", rp.wgs)
			t.Logf("round-trip:  %s", back)
			t.Logf("error:       %.6f m (tolerance: %.1f m)", d, rp.tolerance*2)
			assert.Less(t, d, rp.tolerance*2, "round-trip error exceeds 2x tolerance")
		})
	}
}

func TestRoundTrip_LV95_WGS84_LV95(t *testing.T) {
	for _, rp := range referencePoints {
		t.Run(rp.name, func(t *testing.T) {
			wgs := LV95ToWGS84(rp.lv95)
			back := WGS84ToLV95(wgs)
			dE := math.Abs(back.E - rp.lv95.E)
			dN := math.Abs(back.N - rp.lv95.N)
			d := math.Sqrt(dE*dE + dN*dN)
			t.Logf("original:    %s", rp.lv95)
			t.Logf("round-trip:  %s", back)
			t.Logf("error:       %.6f m (tolerance: %.1f m)", d, rp.tolerance*2)
			assert.Less(t, d, rp.tolerance*2, "round-trip error exceeds 2x tolerance")
		})
	}
}

func TestWGS84ToLV95Batch(t *testing.T) {
	n := len(referencePoints)
	wgsLat := make([]float64, n)
	wgsLon := make([]float64, n)
	lvE := make([]float64, n)
	lvN := make([]float64, n)

	for i, rp := range referencePoints {
		wgsLat[i] = rp.wgs.Lat
		wgsLon[i] = rp.wgs.Lon
	}

	WGS84ToLV95Batch(wgsLat, wgsLon, lvE, lvN)

	for i, rp := range referencePoints {
		single := WGS84ToLV95(rp.wgs)
		assert.InDelta(t, single.E, lvE[i], 1e-10, "batch E must equal scalar E for %s", rp.name)
		assert.InDelta(t, single.N, lvN[i], 1e-10, "batch N must equal scalar N for %s", rp.name)
	}
}

func TestLV95ToWGS84Batch(t *testing.T) {
	n := len(referencePoints)
	lvE := make([]float64, n)
	lvN := make([]float64, n)
	wgsLat := make([]float64, n)
	wgsLon := make([]float64, n)

	for i, rp := range referencePoints {
		lvE[i] = rp.lv95.E
		lvN[i] = rp.lv95.N
	}

	LV95ToWGS84Batch(lvE, lvN, wgsLat, wgsLon)

	for i, rp := range referencePoints {
		single := LV95ToWGS84(rp.lv95)
		assert.InDelta(t, single.Lat, wgsLat[i], 1e-10, "batch Lat must equal scalar Lat for %s", rp.name)
		assert.InDelta(t, single.Lon, wgsLon[i], 1e-10, "batch Lon must equal scalar Lon for %s", rp.name)
	}
}

func TestBernOrigin(t *testing.T) {
	// Verify LV95(2'600'000, 1'200'000) maps to the REFRAME-confirmed WGS84 for Bern.
	// Note: the Bern observatory sexagesimal coordinates (46°57'08.66" N, 7°26'22.50" E)
	// differ from the approximate formula's output by ~164m because the formula includes
	// the constant offsets (72.37m E, 147.07m N) that absorb the datum shift residual.
	// The REFRAME reference point IS the ground truth.
	bern := LV95Coord{E: 2_600_000, N: 1_200_000}
	wgs := LV95ToWGS84(bern)

	expected := WGS84Coord{Lat: 46.951082876677035, Lon: 7.438632495274896}
	d := distanceMeters(wgs, expected)
	t.Logf("Bern REFRAME offset: %.3f m", d)
	assert.Less(t, d, 2.0)

	lv := WGS84ToLV95(expected)
	dE := math.Abs(lv.E - bern.E)
	dN := math.Abs(lv.N - bern.N)
	dTotal := math.Sqrt(dE*dE + dN*dN)
	t.Logf("reverse offset: %.3f m (dE=%.3f dN=%.3f)", dTotal, dE, dN)
	assert.Less(t, dTotal, 2.0)
}

func TestEdgeCases(t *testing.T) {
	t.Run("extreme SW of Switzerland", func(t *testing.T) {
		// Geneva area
		lv := WGS84ToLV95(WGS84Coord{Lat: 46.2, Lon: 6.15})
		require.Greater(t, lv.E, 2_400_000.0)
		require.Less(t, lv.E, 2_600_000.0)
		require.Greater(t, lv.N, 1_100_000.0)
		require.Less(t, lv.N, 1_200_000.0)
	})

	t.Run("extreme NE of Switzerland", func(t *testing.T) {
		// Bodensee area
		lv := WGS84ToLV95(WGS84Coord{Lat: 47.6, Lon: 9.5})
		require.Greater(t, lv.E, 2_700_000.0)
		require.Less(t, lv.E, 2_800_000.0)
		require.Greater(t, lv.N, 1_250_000.0)
		require.Less(t, lv.N, 1_350_000.0)
	})
}

func BenchmarkWGS84ToLV95(b *testing.B) {
	wgs := WGS84Coord{Lat: 46.951082876677035, Lon: 7.438632495274896}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = WGS84ToLV95(wgs)
	}
}

func BenchmarkLV95ToWGS84(b *testing.B) {
	lv := LV95Coord{E: 2_600_000, N: 1_200_000}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = LV95ToWGS84(lv)
	}
}

func BenchmarkWGS84ToLV95Batch_10000(b *testing.B) {
	const n = 10_000
	wgsLat := make([]float64, n)
	wgsLon := make([]float64, n)
	lvE := make([]float64, n)
	lvN := make([]float64, n)

	for i := 0; i < n; i++ {
		wgsLat[i] = 45.8 + float64(i)/float64(n)*2.1
		wgsLon[i] = 5.9 + float64(i)/float64(n)*4.6
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WGS84ToLV95Batch(wgsLat, wgsLon, lvE, lvN)
	}
}

func BenchmarkLV95ToWGS84Batch_10000(b *testing.B) {
	const n = 10_000
	lvE := make([]float64, n)
	lvN := make([]float64, n)
	wgsLat := make([]float64, n)
	wgsLon := make([]float64, n)

	for i := 0; i < n; i++ {
		lvE[i] = 2_480_000 + float64(i)/float64(n)*300_000
		lvN[i] = 1_070_000 + float64(i)/float64(n)*280_000
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		LV95ToWGS84Batch(lvE, lvN, wgsLat, wgsLon)
	}
}

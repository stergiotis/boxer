//go:build llm_generated_opus46

package swisstopo

// Approximate formulas for converting between WGS84 and CH1903+/LV95
// as published by swisstopo (Federal Office of Topography).
//
// Source: "Formulas and constants for the calculation of the Swiss conformal
// cylindrical projection and for the transformation between coordinate systems"
//
// Accuracy: ~1 meter within Switzerland's extent.
// This is sub-pixel for the 2m swissALTI3D DEM tiles.
//
// The formulas use the Bern observatory as origin and operate on auxiliary
// variables derived from arc-seconds (WGS84→LV95) or fractional megameters
// (LV95→WGS84).

const (
	// Bern observatory in WGS84 arc-seconds
	bernLatArcSec = 169028.66 // 46°57'08.66"
	bernLonArcSec = 26782.5   // 7°26'22.50"

	// LV95 false origin (Bern)
	lv95FalseE = 2_600_000.0
	lv95FalseN = 1_200_000.0
)

// WGS84ToLV95 converts a WGS84 geographic coordinate to Swiss LV95 planimetric coordinates.
func WGS84ToLV95(wgs WGS84Coord) (lv LV95Coord) {
	// auxiliary values: shift to Bern origin, scale to 10'000 arc-seconds
	phiPrime := (wgs.Lat*3600 - bernLatArcSec) / 10_000
	lambdaPrime := (wgs.Lon*3600 - bernLonArcSec) / 10_000

	pp := phiPrime * phiPrime
	lp := lambdaPrime * lambdaPrime

	lv.E = lv95FalseE +
		72.37 +
		211_455.93*lambdaPrime -
		10_938.51*lambdaPrime*phiPrime -
		0.36*lambdaPrime*pp -
		44.54*lambdaPrime*lp

	lv.N = lv95FalseN +
		147.07 +
		308_807.95*phiPrime +
		3_745.25*lp +
		76.63*pp -
		194.56*lp*phiPrime +
		119.79*phiPrime*pp

	return
}

// LV95ToWGS84 converts Swiss LV95 planimetric coordinates to a WGS84 geographic coordinate.
func LV95ToWGS84(lv LV95Coord) (wgs WGS84Coord) {
	// auxiliary values: shift from false origin, scale to megameters
	yPrime := (lv.E - lv95FalseE) / 1_000_000
	xPrime := (lv.N - lv95FalseN) / 1_000_000

	xx := xPrime * xPrime
	yy := yPrime * yPrime

	// longitude in 10'000 arc-seconds
	lonSec10k := 2.6779094 +
		4.728982*yPrime +
		0.791484*yPrime*xPrime +
		0.1306*yPrime*xx -
		0.0436*yPrime*yy

	// latitude in 10'000 arc-seconds
	latSec10k := 16.9023892 +
		3.238272*xPrime -
		0.270978*yy -
		0.002528*xx -
		0.0447*yy*xPrime -
		0.0140*xPrime*xx

	// convert 10'000 arc-seconds → degrees
	wgs.Lon = lonSec10k * 10_000 / 3600
	wgs.Lat = latSec10k * 10_000 / 3600
	return
}

// WGS84ToLV95Batch converts slices of WGS84 coordinates to LV95 in place.
// All four slices must have equal length.
// This is the SoA (struct-of-arrays) variant for bulk conversion.
func WGS84ToLV95Batch(wgsLat []float64, wgsLon []float64, lvE []float64, lvN []float64) {
	n := len(wgsLat)
	for i := 0; i < n; i++ {
		phiPrime := (wgsLat[i]*3600 - bernLatArcSec) / 10_000
		lambdaPrime := (wgsLon[i]*3600 - bernLonArcSec) / 10_000

		pp := phiPrime * phiPrime
		lp := lambdaPrime * lambdaPrime

		lvE[i] = lv95FalseE +
			72.37 +
			211_455.93*lambdaPrime -
			10_938.51*lambdaPrime*phiPrime -
			0.36*lambdaPrime*pp -
			44.54*lambdaPrime*lp

		lvN[i] = lv95FalseN +
			147.07 +
			308_807.95*phiPrime +
			3_745.25*lp +
			76.63*pp -
			194.56*lp*phiPrime +
			119.79*phiPrime*pp
	}
}

// LV95ToWGS84Batch converts slices of LV95 coordinates to WGS84 in place.
// All four slices must have equal length.
func LV95ToWGS84Batch(lvE []float64, lvN []float64, wgsLat []float64, wgsLon []float64) {
	n := len(lvE)
	for i := 0; i < n; i++ {
		yPrime := (lvE[i] - lv95FalseE) / 1_000_000
		xPrime := (lvN[i] - lv95FalseN) / 1_000_000

		xx := xPrime * xPrime
		yy := yPrime * yPrime

		lonSec10k := 2.6779094 +
			4.728982*yPrime +
			0.791484*yPrime*xPrime +
			0.1306*yPrime*xx -
			0.0436*yPrime*yy

		latSec10k := 16.9023892 +
			3.238272*xPrime -
			0.270978*yy -
			0.002528*xx -
			0.0447*yy*xPrime -
			0.0140*xPrime*xx

		wgsLon[i] = lonSec10k * 10_000 / 3600
		wgsLat[i] = latSec10k * 10_000 / 3600
	}
}

package worldmap

import "math"

// The Natural Earth projection (Šavrič, Jenny, Patterson et al. 2011) — a
// pseudocylindrical compromise projection designed for schematic small-scale
// world maps, which is exactly this widget's use (ADR-0114 §SD2). Closed
// polynomial form; no interpolation table.
//
// x = λ · l(φ), y = d(φ), with φ/λ in radians and the polynomials below.
// The projection is symmetric in both axes; xMax is reached at (φ=0, λ=π)
// and yMax at φ=π/2.

func natEarthLength(phi float64) float64 {
	p2 := phi * phi
	p4 := p2 * p2
	return 0.870700 - 0.131979*p2 - 0.013791*p4 + p4*p4*p2*(0.003971-0.001529*p2)
}

func natEarthX(lonRad, latRad float64) float64 {
	return lonRad * natEarthLength(latRad)
}

func natEarthY(latRad float64) float64 {
	p2 := latRad * latRad
	p4 := p2 * p2
	return latRad * (1.007226 + p2*(0.015085+p4*(-0.044475+0.028874*p2-0.005916*p4)))
}

var (
	natEarthXMax = natEarthX(math.Pi, 0)
	natEarthYMax = natEarthY(math.Pi / 2)
)

// ProjectionAspect is the width:height ratio of the projected world extent —
// the raster is sized to this so the map never stretches.
func ProjectionAspect() float64 { return natEarthXMax / natEarthYMax }

// projectNorm maps (lonDeg, latDeg) into normalized projection space:
// x ∈ [0,1] west→east, y ∈ [0,1] north→south (y=0 is the north edge, matching
// the raster's row-major top-down layout).
func projectNorm(lonDeg, latDeg float64) (x, y float64) {
	lon := lonDeg * math.Pi / 180
	lat := latDeg * math.Pi / 180
	x = (natEarthX(lon, lat) + natEarthXMax) / (2 * natEarthXMax)
	y = (natEarthYMax - natEarthY(lat)) / (2 * natEarthYMax)
	return
}

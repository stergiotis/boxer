package worldmap

import (
	"fmt"
	"math"
)

// The projections the widget can draw the atlas under (ADR-0114 §SD2 and the
// 2026-09-06 update). Both are pseudocylindrical: x is linear in λ, y depends
// on φ alone, and both are closed polynomial forms — no interpolation table.
// Adding one means a forward pair (x, y) in this file plus a table entry; the
// atlas projects lazily per projection and the raster follows.

// Projection selects a forward projection. The zero value is the widget's
// default, so a Widget that never calls SetProjection draws Natural Earth.
type Projection uint8

const (
	// ProjectionNaturalEarth is the Natural Earth projection (Šavrič, Jenny,
	// Patterson et al. 2011): a compromise projection — neither equal-area nor
	// conformal — tuned by survey for the look of a small-scale world map.
	ProjectionNaturalEarth Projection = iota
	// ProjectionEqualEarth is the Equal Earth projection (Šavrič, Patterson,
	// Jenny 2018): equal-area, so a country's drawn area is proportional to
	// its true area — the property a choropleth's "how much, where" reading
	// wants, since an area-distorting projection weights the fills by
	// something other than the data.
	ProjectionEqualEarth
	projectionCount
)

// Projections lists the selectable projections in menu order.
var Projections = []Projection{ProjectionNaturalEarth, ProjectionEqualEarth}

// projSpec is one projection's forward pair plus the world extent derived from
// it. x/y take radians and return projected units of arbitrary scale —
// projectNorm normalizes them away, so only the ratio matters.
type projSpec struct {
	label string
	x     func(lonRad, latRad float64) float64
	y     func(latRad float64) float64

	// xMax / yMax bound the projected world; aspect is their ratio, doubled
	// on both axes and so unchanged.
	xMax, yMax, aspect float64
}

var projSpecs = [projectionCount]projSpec{
	ProjectionNaturalEarth: {label: "Natural Earth", x: natEarthX, y: natEarthY},
	ProjectionEqualEarth:   {label: "Equal Earth", x: equalEarthX, y: equalEarthY},
}

func init() {
	for i := range projSpecs {
		s := &projSpecs[i]
		// Both projections reach their widest at the equator and their
		// tallest at the pole. That is a property of these two formulas, not
		// of pseudocylindrical projections in general, so TestProjectionExtent
		// scans every parallel for a wider or taller one.
		s.xMax = s.x(math.Pi, 0)
		s.yMax = s.y(math.Pi / 2)
		s.aspect = s.xMax / s.yMax
	}
}

// spec resolves the projection, falling back to the default for a value
// outside the enum — a stale persisted setting draws a map rather than panics.
func (inst Projection) spec() *projSpec {
	if !inst.Valid() {
		inst = ProjectionNaturalEarth
	}
	return &projSpecs[inst]
}

// Valid reports whether the value names a projection this package knows.
func (inst Projection) Valid() bool { return inst < projectionCount }

// String is the human-facing name, as shown in a projection picker.
func (inst Projection) String() string {
	if !inst.Valid() {
		return fmt.Sprintf("Projection(%d)", uint8(inst))
	}
	return projSpecs[inst].label
}

// Aspect is the width:height ratio of the projected world extent — the raster
// and the on-screen canvas are sized to it so the map never stretches.
func (inst Projection) Aspect() float64 { return inst.spec().aspect }

// projectNorm maps (lonDeg, latDeg) into the projection's normalized space:
// x ∈ [0,1] west→east, y ∈ [0,1] north→south (y=0 is the north edge, matching
// the raster's row-major top-down layout).
func projectNorm(p Projection, lonDeg, latDeg float64) (x, y float64) {
	s := p.spec()
	lon := lonDeg * math.Pi / 180
	lat := latDeg * math.Pi / 180
	x = (s.x(lon, lat) + s.xMax) / (2 * s.xMax)
	y = (s.yMax - s.y(lat)) / (2 * s.yMax)
	return
}

// --- Natural Earth (Šavrič, Jenny, Patterson et al. 2011) --------------------
//
// x = λ · l(φ), y = d(φ), with the polynomials below. Symmetric in both axes.

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

// --- Equal Earth (Šavrič, Patterson, Jenny 2018) -----------------------------
//
//	θ = asin(√3/2 · sin φ)
//	y = θ · (A1 + A2θ² + θ⁶(A3 + A4θ²))
//	x = λ · cos θ / (√3/2 · dy/dθ)
//
// The x denominator is dy/dθ, which is what makes the projection equal-area:
// a parallel's spacing and its length move inversely. Coefficients as
// published; θ is the authalic-like parametric latitude, not φ.

const (
	eeA1 = 1.340264
	eeA2 = -0.081106
	eeA3 = 0.000893
	eeA4 = 0.003796
)

// eeM is √3/2, the sine scaling that makes the parametric latitude θ carry
// equal areas.
var eeM = math.Sqrt(3) / 2

func equalEarthTheta(latRad float64) float64 {
	return math.Asin(eeM * math.Sin(latRad))
}

func equalEarthY(latRad float64) float64 {
	t := equalEarthTheta(latRad)
	t2 := t * t
	t6 := t2 * t2 * t2
	return t * (eeA1 + eeA2*t2 + t6*(eeA3+eeA4*t2))
}

func equalEarthX(lonRad, latRad float64) float64 {
	t := equalEarthTheta(latRad)
	t2 := t * t
	t6 := t2 * t2 * t2
	// dy/dθ — strictly positive over θ ∈ [-π/3, π/3], so no division guard.
	dy := eeA1 + 3*eeA2*t2 + t6*(7*eeA3+9*eeA4*t2)
	return lonRad * math.Cos(t) / (eeM * dy)
}

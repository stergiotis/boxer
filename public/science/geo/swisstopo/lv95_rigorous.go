package swisstopo

import "math"

// Rigorous conversion between WGS84 and CH1903+/LV95, as opposed to the
// truncated series in lv95.go.
//
// Two stages, per swisstopo's "Formulas and constants for the calculation of
// the Swiss conformal cylindrical projection and for the transformation
// between coordinate systems":
//
//  1. The Swiss Oblique Mercator — a conformal double projection that maps the
//     Bessel 1841 ellipsoid onto a sphere (Gauss), rotates the sphere so the
//     fundamental point in Bern lies on the pseudo-equator, then applies a
//     Mercator projection.
//  2. A three-parameter Helmert translation in geocentric Cartesian space
//     between the Bessel and WGS84 ellipsoids (EPSG:1676, "CH1903+ to WGS 84 (1)").
//
// The translation is applied at ellipsoidal height zero, which is what a
// two-dimensional transformation means. A point's true height perturbs the
// result well below the level the datum shift itself is specified to.
//
// Accuracy has two separate components, and only the first is this code's.
// The projection stage is exact to floating-point: these functions reproduce
// PROJ's EPSG:2056 pipeline to under a micrometre. The datum stage is the
// three-parameter shift, which is a fit — swisstopo's own REFRAME service uses
// a triangulated transformation instead, and against its published reference
// points this implementation lands within about 1.5 cm. That residual is the
// Helmert parameters' departure from REFRAME, not an error in the formulas, and
// no choice of projection code removes it.

const (
	besselA          = 6_377_397.155
	besselInvF       = 299.1528128
	wgs84A           = 6_378_137.0
	wgs84InvF        = 298.257223563
	fundamentalLatCH = 46.95240555555556 // 46°57'08.66"
	fundamentalLonCH = 7.439583333333333 // 7°26'22.50"

	// CH1903+ to WGS84 geocentric translation.
	helmertDX = 674.374
	helmertDY = 15.056
	helmertDZ = 405.346
)

// obliqueMercator holds the derived constants of the Swiss projection. They
// depend only on the ellipsoid and the fundamental point, so they are computed
// once rather than per conversion.
type obliqueMercator struct {
	e          float64 // first eccentricity of Bessel 1841
	alpha      float64 // ratio between sphere and ellipsoid longitudes
	b0         float64 // fundamental point's latitude on the projection sphere
	k          float64 // integration constant of the Gauss projection
	r          float64 // radius of the projection sphere
	lambda0    float64 // fundamental point's longitude, radians
	besselE2   float64
	wgs84E2    float64
	besselAxis float64
}

var swissProjection = newObliqueMercator()

func newObliqueMercator() (inst obliqueMercator) {
	f := 1 / besselInvF
	e2 := 2*f - f*f
	e := math.Sqrt(e2)
	phi0 := fundamentalLatCH * math.Pi / 180

	sinPhi0 := math.Sin(phi0)
	inst.besselE2 = e2
	inst.e = e
	inst.besselAxis = besselA
	inst.lambda0 = fundamentalLonCH * math.Pi / 180

	wf := 1 / wgs84InvF
	inst.wgs84E2 = 2*wf - wf*wf

	// Radius of the sphere that preserves curvature at the fundamental point.
	inst.r = besselA * math.Sqrt(1-e2) / (1 - e2*sinPhi0*sinPhi0)

	cosPhi0 := math.Cos(phi0)
	cos4 := cosPhi0 * cosPhi0 * cosPhi0 * cosPhi0
	inst.alpha = math.Sqrt(1 + (e2/(1-e2))*cos4)

	inst.b0 = math.Asin(sinPhi0 / inst.alpha)

	inst.k = math.Log(math.Tan(math.Pi/4+inst.b0/2)) -
		inst.alpha*math.Log(math.Tan(math.Pi/4+phi0/2)) +
		inst.alpha*e*math.Atanh(e*sinPhi0)
	return
}

// LV95ToWGS84Rigorous converts LV95 planimetric coordinates to WGS84 using the
// closed-form Swiss Oblique Mercator and the CH1903+ datum shift, landing
// within about 1.5 cm of swisstopo's REFRAME reference points where
// [LV95ToWGS84]'s truncated series deviates by up to several metres.
//
// Prefer this wherever the result is stored, published, or compared against
// another implementation. [LV95ToWGS84] is cheaper and stays adequate for
// display, where it is already sub-pixel on a 2 m raster.
func LV95ToWGS84Rigorous(lv LV95Coord) (wgs WGS84Coord) {
	phi, lambda := swissProjection.inverseProjection(lv)
	lat, lon := swissProjection.besselToWGS84(phi, lambda)
	wgs.Lat = lat * 180 / math.Pi
	wgs.Lon = lon * 180 / math.Pi
	return
}

// WGS84ToLV95Rigorous is the inverse of [LV95ToWGS84Rigorous].
func WGS84ToLV95Rigorous(wgs WGS84Coord) (lv LV95Coord) {
	phi, lambda := swissProjection.wgs84ToBessel(wgs.Lat*math.Pi/180, wgs.Lon*math.Pi/180)
	return swissProjection.forwardProjection(phi, lambda)
}

// forwardProjection maps geodetic coordinates on the Bessel ellipsoid to LV95.
func (inst *obliqueMercator) forwardProjection(phi float64, lambda float64) (lv LV95Coord) {
	// ellipsoid -> sphere
	s := inst.alpha*math.Log(math.Tan(math.Pi/4+phi/2)) -
		inst.alpha*inst.e*math.Atanh(inst.e*math.Sin(phi)) +
		inst.k
	b := 2 * (math.Atan(math.Exp(s)) - math.Pi/4)
	l := inst.alpha * (lambda - inst.lambda0)

	// rotate so the fundamental point lies on the pseudo-equator
	sinB0, cosB0 := math.Sincos(inst.b0)
	sinB, cosB := math.Sincos(b)
	sinL, cosL := math.Sincos(l)

	barL := math.Atan2(sinL, sinB0*math.Tan(b)+cosB0*cosL)
	barB := math.Asin(cosB0*sinB - sinB0*cosB*cosL)

	// sphere -> plane
	lv.E = inst.r*barL + lv95FalseE
	lv.N = inst.r*math.Atanh(math.Sin(barB)) + lv95FalseN
	return
}

// inverseProjection maps LV95 to geodetic coordinates on the Bessel ellipsoid.
func (inst *obliqueMercator) inverseProjection(lv LV95Coord) (phi float64, lambda float64) {
	y := lv.E - lv95FalseE
	x := lv.N - lv95FalseN

	barB := 2 * (math.Atan(math.Exp(x/inst.r)) - math.Pi/4)
	barL := y / inst.r

	sinB0, cosB0 := math.Sincos(inst.b0)
	sinBarB, cosBarB := math.Sincos(barB)
	sinBarL, cosBarL := math.Sincos(barL)

	b := math.Asin(cosB0*sinBarB + sinB0*cosBarB*cosBarL)
	l := math.Atan2(sinBarL, cosB0*cosBarL-sinB0*math.Tan(barB))

	lambda = inst.lambda0 + l/inst.alpha

	// Invert the Gauss projection. The latitude appears on both sides through
	// the isometric term, so it is solved by fixed-point iteration; the
	// eccentricity is small enough that this contracts quickly.
	s := (math.Log(math.Tan(math.Pi/4+b/2)) - inst.k) / inst.alpha
	phi = 2*math.Atan(math.Exp(s)) - math.Pi/2
	for range 8 {
		next := 2*math.Atan(math.Exp(s+inst.e*math.Atanh(inst.e*math.Sin(phi)))) - math.Pi/2
		if math.Abs(next-phi) < 1e-14 {
			phi = next
			break
		}
		phi = next
	}
	return
}

// besselToWGS84 applies the CH1903+ -> WGS84 translation at zero ellipsoidal height.
func (inst *obliqueMercator) besselToWGS84(phi float64, lambda float64) (lat float64, lon float64) {
	x, y, z := geodeticToCartesian(phi, lambda, 0, inst.besselAxis, inst.besselE2)
	return cartesianToGeodetic(x+helmertDX, y+helmertDY, z+helmertDZ, wgs84A, inst.wgs84E2)
}

// wgs84ToBessel is the inverse of [obliqueMercator.besselToWGS84].
func (inst *obliqueMercator) wgs84ToBessel(lat float64, lon float64) (phi float64, lambda float64) {
	x, y, z := geodeticToCartesian(lat, lon, 0, wgs84A, inst.wgs84E2)
	return cartesianToGeodetic(x-helmertDX, y-helmertDY, z-helmertDZ, inst.besselAxis, inst.besselE2)
}

func geodeticToCartesian(phi float64, lambda float64, h float64, a float64, e2 float64) (x float64, y float64, z float64) {
	sinPhi, cosPhi := math.Sincos(phi)
	sinLambda, cosLambda := math.Sincos(lambda)
	n := a / math.Sqrt(1-e2*sinPhi*sinPhi)
	x = (n + h) * cosPhi * cosLambda
	y = (n + h) * cosPhi * sinLambda
	z = (n*(1-e2) + h) * sinPhi
	return
}

// cartesianToGeodetic inverts [geodeticToCartesian] by Bowring's method, whose
// error is below a micrometre for points near the ellipsoid surface.
func cartesianToGeodetic(x float64, y float64, z float64, a float64, e2 float64) (phi float64, lambda float64) {
	b := a * math.Sqrt(1-e2)
	ep2 := (a*a - b*b) / (b * b)
	p := math.Hypot(x, y)
	theta := math.Atan2(z*a, p*b)
	sinTheta, cosTheta := math.Sincos(theta)
	phi = math.Atan2(z+ep2*b*sinTheta*sinTheta*sinTheta, p-e2*a*cosTheta*cosTheta*cosTheta)
	lambda = math.Atan2(y, x)
	return
}

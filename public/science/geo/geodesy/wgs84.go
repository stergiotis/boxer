package geodesy

import "math"

// Wgs84SemiMajorM is the WGS84 equatorial radius a (NIMA TR8350.2). Every
// height in this package is above the ellipsoid these constants define.
const Wgs84SemiMajorM = 6_378_137.0

// Wgs84InvFlattening is the WGS84 inverse flattening 1/f.
const Wgs84InvFlattening = 298.257_223_563

// Wgs84Flattening is f = (a − b)/a.
const Wgs84Flattening = 1 / Wgs84InvFlattening

// Wgs84SemiMinorM is the polar radius b = a(1 − f).
const Wgs84SemiMinorM = Wgs84SemiMajorM * (1 - Wgs84Flattening)

// Wgs84EccentricitySq is the first eccentricity squared, e² = f(2 − f).
const Wgs84EccentricitySq = Wgs84Flattening * (2 - Wgs84Flattening)

// Wgs84MinCurvatureRadiusM is the smallest radius of curvature of the
// ellipsoid in any direction anywhere, b²/a: the meridian radius at the
// equator, the tightest curve on the surface. A sphere of this radius lies
// inside the ellipsoid's curvature everywhere, which is what a flat
// approximation's error bound needs.
const Wgs84MinCurvatureRadiusM = Wgs84SemiMinorM * Wgs84SemiMinorM / Wgs84SemiMajorM

// Wgs84MaxCurvatureRadiusM is the largest radius of curvature of the
// ellipsoid in any direction anywhere, a²/b: the radius at the poles, the
// flattest point. A sphere of this radius lies outside the ellipsoid's
// curvature everywhere.
const Wgs84MaxCurvatureRadiusM = Wgs84SemiMajorM * Wgs84SemiMajorM / Wgs84SemiMinorM

// MinMetresPerDegreeLat bounds a degree of latitude on WGS84 from below
// (110 574 m at the equator, where it is shortest), so a distance converted
// to degrees with it covers at least that distance; a degree of longitude
// at latitude φ is at least this times cos φ.
const MinMetresPerDegreeLat = 110_500.0

const degToRad = math.Pi / 180
const radToDeg = 180 / math.Pi

// Vec3 is a Cartesian vector in metres or metres per second. In ECEF its
// axes are those of the Earth-centred, Earth-fixed frame: X through the
// prime meridian on the equator, Z through the north pole. In a [Frame]'s
// coordinates X is east, Y north and Z up.
type Vec3 struct {
	X float64
	Y float64
	Z float64
}

func (inst Vec3) Add(o Vec3) (r Vec3) {
	r = Vec3{X: inst.X + o.X, Y: inst.Y + o.Y, Z: inst.Z + o.Z}
	return
}

func (inst Vec3) Sub(o Vec3) (r Vec3) {
	r = Vec3{X: inst.X - o.X, Y: inst.Y - o.Y, Z: inst.Z - o.Z}
	return
}

func (inst Vec3) Scale(k float64) (r Vec3) {
	r = Vec3{X: k * inst.X, Y: k * inst.Y, Z: k * inst.Z}
	return
}

func (inst Vec3) Dot(o Vec3) (d float64) {
	d = inst.X*o.X + inst.Y*o.Y + inst.Z*o.Z
	return
}

// Cross is the right-handed vector product inst × o.
func (inst Vec3) Cross(o Vec3) (r Vec3) {
	r = Vec3{
		X: inst.Y*o.Z - inst.Z*o.Y,
		Y: inst.Z*o.X - inst.X*o.Z,
		Z: inst.X*o.Y - inst.Y*o.X,
	}
	return
}

func (inst Vec3) Norm() (n float64) {
	n = math.Sqrt(inst.Dot(inst))
	return
}

// Geodetic is a WGS84 position. HeightM is above the ellipsoid, not above
// mean sea level (ADR-0356 §SD1): a terrain model referenced to a geoid,
// such as SRTM on EGM96, and a barometric altitude are both orthometric
// and must be lifted by the geoid undulation before they become one of
// these. The undulation lies within about ±100 m worldwide and varies
// slowly, so over a region of tens of kilometres one value serves.
type Geodetic struct {
	LatDeg  float64
	LonDeg  float64
	HeightM float64
}

// GeodeticToEcef converts a WGS84 position to Earth-centred, Earth-fixed
// metres. See EXPLANATION.md for the formula.
func GeodeticToEcef(g Geodetic) (p Vec3) {
	sinLat, cosLat := math.Sincos(g.LatDeg * degToRad)
	sinLon, cosLon := math.Sincos(g.LonDeg * degToRad)
	n := Wgs84SemiMajorM / math.Sqrt(1-Wgs84EccentricitySq*sinLat*sinLat)
	p = Vec3{
		X: (n + g.HeightM) * cosLat * cosLon,
		Y: (n + g.HeightM) * cosLat * sinLon,
		Z: (n*(1-Wgs84EccentricitySq) + g.HeightM) * sinLat,
	}
	return
}

// EcefToGeodetic is the inverse of [GeodeticToEcef], to sub-millimetre
// from the deepest trench to low Earth orbit. It iterates latitude and
// height to a fixed point (ADR-0356 §SD5); see EXPLANATION.md.
//
// The height is evaluated as ρ·cos φ + Z·sin φ − a²/N, which, unlike
// ρ/cos φ − N, stays well conditioned at the poles.
func EcefToGeodetic(p Vec3) (g Geodetic) {
	const a = Wgs84SemiMajorM
	const e2 = Wgs84EccentricitySq
	rho := math.Hypot(p.X, p.Y)
	lat := math.Atan2(p.Z, rho*(1-e2))
	h := 0.0
	for range 16 {
		sinLat, cosLat := math.Sincos(lat)
		n := a / math.Sqrt(1-e2*sinLat*sinLat)
		h = rho*cosLat + p.Z*sinLat - a*a/n
		next := math.Atan2(p.Z, rho*(1-e2*n/(n+h)))
		done := math.Abs(next-lat) <= 1e-14
		lat = next
		if done {
			break
		}
	}
	sinLat, cosLat := math.Sincos(lat)
	n := a / math.Sqrt(1-e2*sinLat*sinLat)
	h = rho*cosLat + p.Z*sinLat - a*a/n
	g = Geodetic{LatDeg: lat * radToDeg, LonDeg: math.Atan2(p.Y, p.X) * radToDeg, HeightM: h}
	return
}

// LocalUp is the unit normal to the ellipsoid at g, as an ECEF vector: the
// direction in which HeightM grows there. It is the last row of the
// east-north-up rotation at g, and the same as [Frame.Up] of a frame built
// at g.
func LocalUp(g Geodetic) (u Vec3) {
	sinLat, cosLat := math.Sincos(g.LatDeg * degToRad)
	sinLon, cosLon := math.Sincos(g.LonDeg * degToRad)
	u = Vec3{X: cosLat * cosLon, Y: cosLat * sinLon, Z: sinLat}
	return
}

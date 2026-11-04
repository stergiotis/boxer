package geodesy

import "math"

// Frame is a local east-north-up tangent frame at an origin. Converting
// ECEF to the frame is a translation and a rotation (ADR-0356 §SD2), so
// distances, ranges and radial velocities are the same in either; the
// frame exists to give directions names and to lay flat things, never to
// approximate the Earth as flat.
type Frame struct {
	origin     Geodetic
	originEcef Vec3
	// rot is the ECEF→ENU rotation, row-major: rows are the unit east,
	// north and up vectors at the origin.
	rot [9]float64
}

// NewFrame builds the tangent frame at origin.
func NewFrame(origin Geodetic) (f Frame) {
	f.origin = origin
	f.originEcef = GeodeticToEcef(origin)
	f.rot = enuRotation(origin.LatDeg, origin.LonDeg)
	return
}

// enuRotation is the ECEF→ENU rotation at a latitude and longitude, row
// major: the unit east, north and up vectors there.
func enuRotation(latDeg float64, lonDeg float64) (rot [9]float64) {
	sinLat, cosLat := math.Sincos(latDeg * degToRad)
	sinLon, cosLon := math.Sincos(lonDeg * degToRad)
	rot = [9]float64{
		-sinLon, cosLon, 0,
		-sinLat * cosLon, -sinLat * sinLon, cosLat,
		cosLat * cosLon, cosLat * sinLon, sinLat,
	}
	return
}

func rotate(rot *[9]float64, v Vec3) (r Vec3) {
	r = Vec3{
		X: rot[0]*v.X + rot[1]*v.Y + rot[2]*v.Z,
		Y: rot[3]*v.X + rot[4]*v.Y + rot[5]*v.Z,
		Z: rot[6]*v.X + rot[7]*v.Y + rot[8]*v.Z,
	}
	return
}

func rotateTransposed(rot *[9]float64, v Vec3) (r Vec3) {
	r = Vec3{
		X: rot[0]*v.X + rot[3]*v.Y + rot[6]*v.Z,
		Y: rot[1]*v.X + rot[4]*v.Y + rot[7]*v.Z,
		Z: rot[2]*v.X + rot[5]*v.Y + rot[8]*v.Z,
	}
	return
}

func (inst Frame) Origin() (g Geodetic) {
	g = inst.origin
	return
}

// OriginEcef is the origin in ECEF metres.
func (inst Frame) OriginEcef() (p Vec3) {
	p = inst.originEcef
	return
}

// East is the frame's east axis as an ECEF unit vector.
func (inst Frame) East() (e Vec3) {
	e = Vec3{X: inst.rot[0], Y: inst.rot[1], Z: inst.rot[2]}
	return
}

// North is the frame's north axis as an ECEF unit vector.
func (inst Frame) North() (n Vec3) {
	n = Vec3{X: inst.rot[3], Y: inst.rot[4], Z: inst.rot[5]}
	return
}

// Up is the frame's vertical as an ECEF unit vector: the normal to the
// ellipsoid at the origin.
func (inst Frame) Up() (u Vec3) {
	u = Vec3{X: inst.rot[6], Y: inst.rot[7], Z: inst.rot[8]}
	return
}

// EcefToEnu returns p in the frame: X east, Y north, Z up, metres from the
// origin.
func (inst Frame) EcefToEnu(p Vec3) (enu Vec3) {
	enu = rotate(&inst.rot, p.Sub(inst.originEcef))
	return
}

// EnuToEcef is the inverse of [Frame.EcefToEnu].
func (inst Frame) EnuToEcef(enu Vec3) (p Vec3) {
	p = rotateTransposed(&inst.rot, enu).Add(inst.originEcef)
	return
}

// RotateEnuToEcef rotates a vector (a velocity) from the frame's axes to
// ECEF axes without translating it.
func (inst Frame) RotateEnuToEcef(v Vec3) (r Vec3) {
	r = rotateTransposed(&inst.rot, v)
	return
}

// RotateEcefToEnu is the inverse of [Frame.RotateEnuToEcef].
func (inst Frame) RotateEcefToEnu(v Vec3) (r Vec3) {
	r = rotate(&inst.rot, v)
	return
}

// EnuToEcefAt rotates a vector given in the east-north-up frame at the
// point at — a velocity reported where it was measured — to ECEF axes,
// without building a [Frame] (ADR-0356 §SD3). It equals
// NewFrame(at).RotateEnuToEcef(v).
func EnuToEcefAt(at Geodetic, v Vec3) (r Vec3) {
	rot := enuRotation(at.LatDeg, at.LonDeg)
	r = rotateTransposed(&rot, v)
	return
}

// EcefToEnuAt is the inverse of [EnuToEcefAt].
func EcefToEnuAt(at Geodetic, v Vec3) (r Vec3) {
	rot := enuRotation(at.LatDeg, at.LonDeg)
	r = rotate(&rot, v)
	return
}

// GroundVelocityEnu turns a navigation velocity report — ground speed,
// true track clockwise from north, vertical rate positive climbing — into
// an east-north-up vector in the frame at the reporting point. It is what
// ADS-B and GNSS receivers report; rotate it with [EnuToEcefAt] at the
// position it was made.
func GroundVelocityEnu(groundSpeedMps float64, trackDeg float64, verticalRateMps float64) (v Vec3) {
	s, c := math.Sincos(trackDeg * degToRad)
	v = Vec3{X: groundSpeedMps * s, Y: groundSpeedMps * c, Z: verticalRateMps}
	return
}

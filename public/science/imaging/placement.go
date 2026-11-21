package imaging

import (
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// FrameKindE says what the coordinates a [Placement] yields are
// (ADR-0398 §SD4). The zero value is no frame: the pixels are not placed.
type FrameKindE uint8

const (
	// FrameKindPhysical is a scale in a unit; the origin means nothing.
	FrameKindPhysical FrameKindE = iota + 1
	// FrameKindReferenceSystem is a coordinate reference system named by
	// [Frame.Code]; the coordinates are whatever that system defines, and
	// nothing here converts them.
	FrameKindReferenceSystem
	// FrameKindCelestialGnomonic is the gnomonic (TAN) projection of the sky
	// about a reference point: the affine yields projection-plane
	// coordinates in degrees, which [Placement.World] evaluates to celestial
	// longitude and latitude.
	FrameKindCelestialGnomonic
)

// AllFrameKinds lists the frames a placement can have.
var AllFrameKinds = []FrameKindE{FrameKindPhysical, FrameKindReferenceSystem, FrameKindCelestialGnomonic}

func (inst FrameKindE) String() string {
	switch inst {
	case FrameKindPhysical:
		return "physical"
	case FrameKindReferenceSystem:
		return "reference-system"
	case FrameKindCelestialGnomonic:
		return "celestial-gnomonic"
	}
	return "none"
}

// Frame is what a placement's coordinates are in.
type Frame struct {
	Kind FrameKindE
	// Code names the system as the file gave it: "EPSG:2056"; for the sky
	// the system as declared ("ICRS", "FK5"). It is recorded, not resolved.
	Code string
	// AxisNames name the two coordinates: "E", "N"; "RA", "Dec"; "x", "y".
	AxisNames [2]string
	// Units are the units of the two coordinates: "m", "deg", "um"; empty is
	// unknown.
	Units [2]string
	// RefLon and RefLat are, for a gnomonic frame, the celestial longitude
	// and latitude of the reference point, in degrees.
	RefLon, RefLat float64
	// LonPole is, for a gnomonic frame, the native longitude of the
	// celestial pole in degrees: the file's own, or [DefaultLonPole] when it
	// states none.
	LonPole float64
}

// DefaultLonPole is the native longitude of the celestial pole a zenithal
// projection takes when the file states none (Calabretta & Greisen 2002,
// §2.4): 180° when the reference point is below the celestial pole, 0°
// when it is the pole.
func DefaultLonPole(refLat float64) float64 {
	if refLat >= 90 {
		return 0
	}
	return 180
}

// Placement says where pixels are (ADR-0398 §SD4): an affine from the
// continuous pixel coordinates of ADR-0373 §SD3 — pixel (col, row) covers
// [col, col+1) × [row, row+1), row 0 at the top — to coordinates in a
// frame. The zero value places nothing. A placement is not part of a
// digest.
type Placement struct {
	// Affine maps pixel coordinates to frame (or, for a gnomonic frame,
	// projection-plane) coordinates:
	// x = Affine[0] + Affine[1]·col + Affine[2]·row,
	// y = Affine[3] + Affine[4]·col + Affine[5]·row.
	Affine [6]float64
	Frame  Frame
	// Source names the statement of the file the placement was derived
	// from, for a person.
	Source string
	// Unapplied lists what the file declared that is not evaluated here: a
	// distortion, an alternate system, another projection.
	Unapplied []string
}

func (inst Placement) clone() Placement {
	inst.Unapplied = slices.Clone(inst.Unapplied)
	return inst
}

// IsZero reports whether the placement places nothing: it has no frame.
func (inst Placement) IsZero() bool { return inst.Frame.Kind == 0 }

// Validate refuses a placement that cannot be evaluated: an affine that is
// not finite or not invertible, a frame kind not listed in [AllFrameKinds],
// and for a gnomonic frame a reference point or pole longitude that is not
// finite, or a reference latitude outside [−90, 90]. The zero placement is
// valid and places nothing.
func (inst Placement) Validate() error {
	if inst.IsZero() {
		return nil
	}
	if !slices.Contains(AllFrameKinds, inst.Frame.Kind) {
		return eb.Build().Uint8("frame", uint8(inst.Frame.Kind)).Errorf("placement has an unknown frame kind")
	}
	for i, a := range inst.Affine {
		if math.IsNaN(a) || math.IsInf(a, 0) {
			return eb.Build().Int("term", i).Float64("value", a).Errorf("placement's affine is not finite")
		}
	}
	if det := inst.det(); det == 0 || math.IsInf(det, 0) || math.IsNaN(det) {
		return eb.Build().Float64("determinant", det).Errorf("placement's affine is not invertible")
	}
	if inst.Frame.Kind == FrameKindCelestialGnomonic {
		f := inst.Frame
		if !isFinite(f.RefLon) || !isFinite(f.LonPole) || !isFinite(f.RefLat) || f.RefLat < -90 || f.RefLat > 90 {
			return eb.Build().Float64("refLon", f.RefLon).Float64("refLat", f.RefLat).Float64("lonPole", f.LonPole).
				Errorf("gnomonic frame needs a finite reference point with latitude in [-90, 90] and a finite pole longitude")
		}
	}
	return nil
}

func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func (inst Placement) det() float64 {
	a := &inst.Affine
	return a[1]*a[5] - a[2]*a[4]
}

// Intermediate is the affine alone at pixel coordinates (col, row): frame
// coordinates for a physical or reference-system frame, projection-plane
// coordinates in degrees for a gnomonic one.
func (inst Placement) Intermediate(col, row float64) (x, y float64) {
	a := &inst.Affine
	return a[0] + a[1]*col + a[2]*row, a[3] + a[4]*col + a[5]*row
}

// World is the frame coordinates of pixel coordinates (col, row): the affine
// for a physical or reference-system frame; celestial longitude in [0, 360)
// and latitude, in degrees, for a gnomonic frame. The zero placement, and a
// result that is not finite, report ok false.
func (inst Placement) World(col, row float64) (a, b float64, ok bool) {
	x, y := inst.Intermediate(col, row)
	switch inst.Frame.Kind {
	case FrameKindPhysical, FrameKindReferenceSystem:
		a, b = x, y
	case FrameKindCelestialGnomonic:
		a, b = inst.Frame.deproject(x, y)
	default:
		return 0, 0, false
	}
	return a, b, isFinite(a) && isFinite(b)
}

// Pixel is the inverse of [Placement.World]: the pixel coordinates of frame
// coordinates (a, b). A sky position on the hemisphere the gnomonic
// projection cannot show, the zero placement, and an affine that is not
// invertible report ok false.
func (inst Placement) Pixel(a, b float64) (col, row float64, ok bool) {
	x, y := a, b
	switch inst.Frame.Kind {
	case FrameKindPhysical, FrameKindReferenceSystem:
	case FrameKindCelestialGnomonic:
		if x, y, ok = inst.Frame.project(a, b); !ok {
			return 0, 0, false
		}
	default:
		return 0, 0, false
	}
	det := inst.det()
	if det == 0 {
		return 0, 0, false
	}
	m := &inst.Affine
	dx, dy := x-m[0], y-m[3]
	col, row = (m[5]*dx-m[2]*dy)/det, (m[1]*dy-m[4]*dx)/det
	return col, row, isFinite(col) && isFinite(row)
}

// PixelSize is the lengths of the affine's column and row vectors: frame
// units (degrees in the projection plane for a gnomonic frame) per pixel
// along a row and along a column.
func (inst Placement) PixelSize() (sx, sy float64) {
	a := &inst.Affine
	return math.Hypot(a[1], a[4]), math.Hypot(a[2], a[5])
}

// Level is the placement of a reduced (k > 0) or magnified (k < 0) level
// whose cells are 2^k pixels on a side, so that cell (c, r) of the level
// covers pixels [c·2^k, (c+1)·2^k) × [r·2^k, (r+1)·2^k) (ADR-0398 §SD4). The
// zero placement stays zero.
func (inst Placement) Level(k int) Placement {
	out := inst.clone()
	if inst.IsZero() {
		return out
	}
	for _, i := range [...]int{1, 2, 4, 5} {
		out.Affine[i] = math.Ldexp(inst.Affine[i], k)
	}
	return out
}

// The gnomonic projection and the spherical rotation are those of
// Calabretta & Greisen 2002 (FITS WCS Paper II): §5.1.3 for TAN, and §2.3
// eqs. (2) and (5) for the rotation between native (φ, θ) and celestial
// (α, δ) coordinates, with the celestial coordinates of the native pole
// (α_p, δ_p) equal to the reference point, as for every zenithal projection
// (θ_0 = 90°), and φ_p = LonPole. They are evaluated on direction cosines
// rather than through the papers' asin and cot, which lose precision near
// the reference point and the poles; the expressions are the same.

// deproject turns projection-plane coordinates in degrees into celestial
// longitude and latitude in degrees.
func (inst Frame) deproject(x, y float64) (lon, lat float64) {
	// TAN: R_θ = (180/π)·cot θ, x = R_θ sin φ, y = −R_θ cos φ; so the native
	// direction is (cos θ cos φ, cos θ sin φ, sin θ) ∝ (−y·π/180, x·π/180, 1).
	nx, ny, nz := -y*math.Pi/180, x*math.Pi/180, 1.0
	// Rotate by −φ_p about the native z axis: n' = (cos θ cos(φ−φ_p),
	// cos θ sin(φ−φ_p), sin θ).
	sp, cp := sincosDeg(inst.LonPole)
	px, py := cp*nx+sp*ny, -sp*nx+cp*ny
	// Eq. (2): with u the celestial direction rotated by −α_p,
	// u_x = cos δ cos(α−α_p) = sin θ cos δ_p − cos θ sin δ_p cos(φ−φ_p),
	// u_y = cos δ sin(α−α_p) = −cos θ sin(φ−φ_p),
	// u_z = sin δ = sin θ sin δ_p + cos θ cos δ_p cos(φ−φ_p).
	sd, cd := sincosDeg(inst.RefLat)
	ux, uy, uz := cd*nz-sd*px, -py, sd*nz+cd*px
	lat = math.Atan2(uz, math.Hypot(ux, uy)) * 180 / math.Pi
	lon = inst.RefLon + math.Atan2(uy, ux)*180/math.Pi
	lon = math.Mod(lon, 360)
	if lon < 0 {
		lon += 360
	}
	if lon >= 360 {
		lon = 0
	}
	return
}

// project turns celestial longitude and latitude in degrees into
// projection-plane coordinates in degrees; ok is false on the hemisphere
// opposite the reference point, which TAN does not show (θ ≤ 0).
func (inst Frame) project(lon, lat float64) (x, y float64, ok bool) {
	sd, cd := sincosDeg(lat)
	sa, ca := sincosDeg(lon - inst.RefLon)
	ux, uy, uz := cd*ca, cd*sa, sd
	// Eq. (5), the inverse of the rotation above.
	sdp, cdp := sincosDeg(inst.RefLat)
	px, py, nz := cdp*uz-sdp*ux, -uy, cdp*ux+sdp*uz
	if !(nz > 0) {
		return 0, 0, false
	}
	sp, cp := sincosDeg(inst.LonPole)
	nx, ny := cp*px-sp*py, sp*px+cp*py
	return ny / nz * 180 / math.Pi, -nx / nz * 180 / math.Pi, true
}

// sincosDeg is the sine and cosine of an angle in degrees, exact at the
// multiples of 90°.
func sincosDeg(deg float64) (s, c float64) {
	r := math.Mod(deg, 360)
	if r < 0 {
		r += 360
	}
	switch r {
	case 0:
		return 0, 1
	case 90:
		return 1, 0
	case 180:
		return 0, -1
	case 270:
		return -1, 0
	}
	return math.Sincos(r * math.Pi / 180)
}

package grib

import (
	"math"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ScanFlags are the four independent bits of flag table 3.4, as coded. They
// say how the stored values are laid out; [Field.Values] does not apply
// them, [Grid.Points] follows them, and [Field.Raster] applies them once
// (ADR-0292 §R2).
type ScanFlags struct {
	// INegative: points in the first row run in the −i direction (east to
	// west on a lat/lon grid).
	INegative bool
	// JPositive: rows run in the +j direction (south to north).
	JPositive bool
	// JConsecutive: adjacent stored values are adjacent in j, not in i (the
	// array is column-major).
	JConsecutive bool
	// Alternating: every second row runs in the opposite i direction
	// (boustrophedonic).
	Alternating bool
	// Raw is the flag octet as coded, for the bits above the four.
	Raw uint8
}

func parseScanFlags(raw uint8) (f ScanFlags) {
	f.Raw = raw
	f.INegative = raw&0x80 != 0
	f.JPositive = raw&0x40 != 0
	f.JConsecutive = raw&0x20 != 0
	f.Alternating = raw&0x10 != 0
	return
}

// EarthShape is code table 3.2 with its radius or axes resolved to metres
// where the table fixes them or the message codes them. Radius is zero for
// an oblate shape; Major and Minor are zero for a sphere.
type EarthShape struct {
	Code         uint8
	Radius       float64
	Major, Minor float64
}

// LatLonGrid is the common shape of templates 3.0 (regular or reduced
// lat/lon), 3.1 (rotated) and 3.40 (Gaussian, regular or reduced). Angles
// are degrees as coded: longitudes in 0…360 in edition 2. Ni is zero and
// PL set when rows have their own point counts; Di is then zero.
type LatLonGrid struct {
	Ni, Nj     uint32
	Lat1, Lon1 float64
	Lat2, Lon2 float64
	Di, Dj     float64
	// AngleUnit is the resolution every coded angle has: 10⁻⁶ degrees or
	// the basic angle's fraction in edition 2, 10⁻³ degrees in edition 1.
	// An increment that misses the last point by less than one unit is a
	// rounding of the coded corners, not an inconsistency.
	AngleUnit float64
	// DiGiven and DjGiven are flag table 3.3's bits 3 and 4: whether the
	// increments are coded rather than to be derived from the corners.
	DiGiven, DjGiven bool
	// UVRelativeToGrid is flag table 3.3 bit 5: wind components are
	// relative to the grid's i and j, not to east and north.
	UVRelativeToGrid bool
	ResolutionFlags  uint8
	// PL holds each row's point count for a reduced grid, in j order as
	// stored (the first entry is the first stored row). PLSum is their sum,
	// which equals the point count only on a global grid.
	PL    []uint32
	PLSum uint64
	// N is the Gaussian grid's number of latitude circles between a pole
	// and the equator (template 3.40 only).
	N uint32
	// Rotated is set for template 3.1.
	Rotated *Rotation
}

// Rotation is template 3.1's rotated pole: the geographic position of the
// rotated system's south pole and the rotation about the rotated axis.
type Rotation struct {
	SouthPoleLat, SouthPoleLon float64
	Angle                      float64
}

// LambertGrid is template 3.30 as coded, metres and degrees.
type LambertGrid struct {
	Nx, Ny                     uint32
	Lat1, Lon1                 float64
	LaD, LoV                   float64
	Dx, Dy                     float64
	ProjectionCentre           uint8
	Latin1, Latin2             float64
	SouthPoleLat, SouthPoleLon float64
	ResolutionFlags            uint8
	UVRelativeToGrid           bool
}

// PolarStereographicGrid is template 3.20 as coded.
type PolarStereographicGrid struct {
	Nx, Ny           uint32
	Lat1, Lon1       float64
	LaD, LoV         float64
	Dx, Dy           float64
	ProjectionCentre uint8
	ResolutionFlags  uint8
	UVRelativeToGrid bool
}

// MercatorGrid is template 3.10 as coded.
type MercatorGrid struct {
	Ni, Nj           uint32
	Lat1, Lon1       float64
	LaD              float64
	Lat2, Lon2       float64
	Dx, Dy           float64
	Orientation      float64
	ResolutionFlags  uint8
	UVRelativeToGrid bool
}

// SpaceViewGrid is template 3.90 as coded; the sub-satellite point and the
// altitude are the fields a consumer projects with.
type SpaceViewGrid struct {
	Nx, Ny           uint32
	Lap, Lop         float64
	Dx, Dy           uint32
	Xp, Yp           float64
	Orientation      float64
	Nr               float64
	Xo, Yo           uint32
	ResolutionFlags  uint8
	UVRelativeToGrid bool
}

// UnstructuredGrid is template 3.101: the grid is named, not described. The
// coordinates live in the producer's horizontal-constants file, keyed by
// UUID (ADR-0292 §R10).
type UnstructuredGrid struct {
	NumberOfGridUsed        uint8
	NumberOfGridInReference uint8
	UUID                    [16]byte
}

// Grid is Section 3. Exactly one of the typed fields is set for a template
// the reader lays out; for any other template Raw holds the template's
// octets and the typed fields are nil, while Template, NumPoints and Shape
// are always filled.
type Grid struct {
	// Template is the GRIB2 grid definition template number; an edition 1
	// grid is mapped onto the template that describes the same layout
	// (0 → 3.0, 4 → 3.40, 10 → 3.1, 14 → 3.40 rotated, 1 → 3.10, 3 → 3.30,
	// 5 → 3.20, 50 → 3.50), and Grib1Type keeps code table 6's value.
	Template  uint16
	Grib1Type uint8
	Source    uint8
	NumPoints uint32
	// Optional list of points: OctetsPerPoint and Interpretation are octets
	// 11 and 12; PL sits in [LatLonGrid.PL].
	OctetsPerPoint uint8
	Interpretation uint8
	Shape          EarthShape
	Scan           ScanFlags
	// Raw is the template's octets from octet 15 on, without the optional
	// point list.
	Raw []byte

	LatLon             *LatLonGrid
	Lambert            *LambertGrid
	PolarStereographic *PolarStereographicGrid
	Mercator           *MercatorGrid
	SpaceView          *SpaceViewGrid
	Unstructured       *UnstructuredGrid
}

// microDegrees is the unit of every coded angle when template 3.0's basic
// angle is zero or missing.
const microDegrees = 1e-6

// maxPoints bounds a grid so a corrupt count cannot drive an allocation:
// 2³² points at 8 bytes is 32 GB, and no producer's field is that large.
const maxPoints = 1 << 31

func parseGrid(body []byte) (g Grid, err error) {
	r := rd{b: body}
	g.Source = r.u8()
	g.NumPoints = r.u32()
	g.OctetsPerPoint = r.u8()
	g.Interpretation = r.u8()
	g.Template = r.u16()
	err = r.truncation("grid definition section")
	if err != nil {
		return
	}
	if g.Source != 0 {
		err = unsupported("grid definition source " + strconv.Itoa(int(g.Source)))
		return
	}
	if g.NumPoints == 0 || g.NumPoints > maxPoints {
		err = eb.Build().Uint32("points", g.NumPoints).Errorf("grid point count not credible: %w", ErrMalformed)
		return
	}
	tpl := r.rest()
	// The optional point list follows the template; its length is known
	// only from the template's own Nj, so each layout takes it.
	switch g.Template {
	case 0, 1, 40:
		err = g.parseLatLon(tpl)
	case 10:
		err = g.parseMercator(tpl)
	case 20:
		err = g.parsePolarStereographic(tpl)
	case 30:
		err = g.parseLambert(tpl)
	case 90:
		err = g.parseSpaceView(tpl)
	case 101:
		err = g.parseUnstructured(tpl)
	default:
		g.Raw = tpl
		if g.OctetsPerPoint != 0 {
			err = unsupported("grid template 3." + strconv.Itoa(int(g.Template)) + " with a point list")
		}
	}
	return
}

// parseShape reads octets 15–30 shared by every earth-referenced template.
func (inst *Grid) parseShape(r *rd) {
	inst.Shape.Code = r.u8()
	rScale := r.u8()
	rValue, rMissing := r.u32m()
	majScale := r.u8()
	majValue, majMissing := r.u32m()
	minScale := r.u8()
	minValue, minMissing := r.u32m()
	switch inst.Shape.Code {
	case 0:
		inst.Shape.Radius = 6367470
	case 1:
		if !rMissing && rScale != math.MaxUint8 {
			inst.Shape.Radius = scaled(rValue, rScale)
		}
	case 2:
		inst.Shape.Major, inst.Shape.Minor = 6378160, 6356775
	case 3:
		if !majMissing && !minMissing {
			inst.Shape.Major, inst.Shape.Minor = scaled(majValue, majScale)*1000, scaled(minValue, minScale)*1000
		}
	case 4:
		inst.Shape.Major, inst.Shape.Minor = 6378137, 6356752.314
	case 5:
		inst.Shape.Major, inst.Shape.Minor = 6378137, 6356752.3142
	case 6:
		inst.Shape.Radius = 6371229
	case 7:
		if !majMissing && !minMissing {
			inst.Shape.Major, inst.Shape.Minor = scaled(majValue, majScale), scaled(minValue, minScale)
		}
	case 8:
		inst.Shape.Radius = 6371200
	case 9:
		inst.Shape.Major, inst.Shape.Minor = 6377563.396, 6356256.909
	}
}

// scaled is value × 10^−scale, the encoding of every scaled quantity.
func scaled(value uint32, scale uint8) (v float64) {
	v = float64(value) * math.Pow(10, -float64(scale))
	return
}

func (inst *Grid) parseLatLon(tpl []byte) (err error) {
	r := rd{b: tpl}
	inst.parseShape(&r)
	g := &LatLonGrid{}
	ni, niMissing := r.u32m()
	g.Nj = r.u32()
	basic := r.u32()
	sub, subMissing := r.u32m()
	lat1 := r.s32()
	lon1 := r.s32()
	g.ResolutionFlags = r.u8()
	lat2 := r.s32()
	lon2 := r.s32()
	di, diMissing := r.u32m()
	var dj uint32
	var djMissing bool
	if inst.Template == 40 {
		g.N = r.u32()
	} else {
		dj, djMissing = r.u32m()
	}
	inst.Scan = parseScanFlags(r.u8())
	if inst.Template == 1 {
		rot := &Rotation{}
		spLat := r.s32()
		spLon := r.s32()
		angle := r.s32()
		rot.SouthPoleLat = float64(spLat) * microDegrees
		rot.SouthPoleLon = float64(spLon) * microDegrees
		rot.Angle = float64(angle) * microDegrees
		g.Rotated = rot
	}
	err = r.truncation("grid template 3." + strconv.Itoa(int(inst.Template)))
	if err != nil {
		return
	}
	// The unit of every coded angle: 10⁻⁶ degrees, or basic/subdivisions
	// when a basic angle is present. A basic angle of 0 and a missing
	// subdivision count both mean the default (ADR-0292 §R5).
	unit := microDegrees
	if basic != 0 && !subMissing && sub != 0 {
		unit = float64(basic) / float64(sub)
	}
	g.AngleUnit = unit
	g.Lat1, g.Lon1 = float64(lat1)*unit, float64(lon1)*unit
	g.Lat2, g.Lon2 = float64(lat2)*unit, float64(lon2)*unit
	g.DiGiven = g.ResolutionFlags&0x20 != 0
	g.DjGiven = g.ResolutionFlags&0x10 != 0
	g.UVRelativeToGrid = g.ResolutionFlags&0x08 != 0
	if !diMissing {
		g.Di = float64(di) * unit
	}
	if !djMissing {
		g.Dj = float64(dj) * unit
	}
	inst.Raw = tpl[:len(tpl)-r.remaining()]
	if g.Nj == 0 || g.Nj > maxPoints {
		err = eb.Build().Uint32("nj", g.Nj).Errorf("row count not credible: %w", ErrMalformed)
		return
	}
	if inst.OctetsPerPoint != 0 {
		// Reduced grid: one count per row.
		if !niMissing {
			err = eb.Build().Uint32("ni", ni).Errorf("point list with a coded Ni: %w", ErrInconsistent)
			return
		}
		if inst.Interpretation != 1 {
			err = unsupported("point list interpretation " + strconv.Itoa(int(inst.Interpretation)))
			return
		}
		if inst.OctetsPerPoint < 1 || inst.OctetsPerPoint > 4 {
			err = eb.Build().Uint8("octets", inst.OctetsPerPoint).Errorf("point list octet width: %w", ErrMalformed)
			return
		}
		if r.remaining() != int(g.Nj)*int(inst.OctetsPerPoint) {
			err = eb.Build().Int("available", r.remaining()).Uint32("nj", g.Nj).Uint8("octets", inst.OctetsPerPoint).Errorf("point list length does not match the row count: %w", ErrInconsistent)
			return
		}
		g.PL = make([]uint32, g.Nj)
		var total uint64
		for j := range g.PL {
			g.PL[j] = uint32(r.uN(int(inst.OctetsPerPoint)))
			total += uint64(g.PL[j])
		}
		// The rows sum to the point count on a global grid. ECMWF codes a
		// sub-area of a reduced Gaussian grid with the global row lengths
		// and a smaller count, so a mismatch marks a sub-area rather than
		// a defect; Points refuses it by name.
		g.PLSum = total
	} else {
		if niMissing {
			err = eb.Build().Errorf("Ni missing without a point list: %w", ErrInconsistent)
			return
		}
		g.Ni = ni
		if uint64(g.Ni)*uint64(g.Nj) != uint64(inst.NumPoints) {
			err = eb.Build().Uint32("ni", g.Ni).Uint32("nj", g.Nj).Uint32("points", inst.NumPoints).Errorf("Ni × Nj is not the point count: %w", ErrInconsistent)
			return
		}
		if r.remaining() != 0 {
			err = eb.Build().Int("extra", r.remaining()).Errorf("bytes after the grid template: %w", ErrMalformed)
			return
		}
	}
	inst.LatLon = g
	return
}

func (inst *Grid) parseMercator(tpl []byte) (err error) {
	r := rd{b: tpl}
	inst.parseShape(&r)
	g := &MercatorGrid{}
	g.Ni = r.u32()
	g.Nj = r.u32()
	g.Lat1 = float64(r.s32()) * microDegrees
	g.Lon1 = float64(r.s32()) * microDegrees
	g.ResolutionFlags = r.u8()
	g.LaD = float64(r.s32()) * microDegrees
	g.Lat2 = float64(r.s32()) * microDegrees
	g.Lon2 = float64(r.s32()) * microDegrees
	inst.Scan = parseScanFlags(r.u8())
	g.Orientation = float64(r.u32()) * microDegrees
	g.Dx = float64(r.u32()) * 1e-3
	g.Dy = float64(r.u32()) * 1e-3
	err = r.truncation("grid template 3.10")
	if err != nil {
		return
	}
	g.UVRelativeToGrid = g.ResolutionFlags&0x08 != 0
	inst.Raw = tpl
	err = inst.checkRectangular(g.Ni, g.Nj, r.remaining())
	inst.Mercator = g
	return
}

func (inst *Grid) parsePolarStereographic(tpl []byte) (err error) {
	r := rd{b: tpl}
	inst.parseShape(&r)
	g := &PolarStereographicGrid{}
	g.Nx = r.u32()
	g.Ny = r.u32()
	g.Lat1 = float64(r.s32()) * microDegrees
	g.Lon1 = float64(r.s32()) * microDegrees
	g.ResolutionFlags = r.u8()
	g.LaD = float64(r.s32()) * microDegrees
	g.LoV = float64(r.s32()) * microDegrees
	g.Dx = float64(r.u32()) * 1e-3
	g.Dy = float64(r.u32()) * 1e-3
	g.ProjectionCentre = r.u8()
	inst.Scan = parseScanFlags(r.u8())
	err = r.truncation("grid template 3.20")
	if err != nil {
		return
	}
	g.UVRelativeToGrid = g.ResolutionFlags&0x08 != 0
	inst.Raw = tpl
	err = inst.checkRectangular(g.Nx, g.Ny, r.remaining())
	inst.PolarStereographic = g
	return
}

func (inst *Grid) parseLambert(tpl []byte) (err error) {
	r := rd{b: tpl}
	inst.parseShape(&r)
	g := &LambertGrid{}
	g.Nx = r.u32()
	g.Ny = r.u32()
	g.Lat1 = float64(r.s32()) * microDegrees
	g.Lon1 = float64(r.s32()) * microDegrees
	g.ResolutionFlags = r.u8()
	g.LaD = float64(r.s32()) * microDegrees
	g.LoV = float64(r.s32()) * microDegrees
	g.Dx = float64(r.u32()) * 1e-3
	g.Dy = float64(r.u32()) * 1e-3
	g.ProjectionCentre = r.u8()
	inst.Scan = parseScanFlags(r.u8())
	g.Latin1 = float64(r.s32()) * microDegrees
	g.Latin2 = float64(r.s32()) * microDegrees
	g.SouthPoleLat = float64(r.s32()) * microDegrees
	g.SouthPoleLon = float64(r.s32()) * microDegrees
	err = r.truncation("grid template 3.30")
	if err != nil {
		return
	}
	g.UVRelativeToGrid = g.ResolutionFlags&0x08 != 0
	inst.Raw = tpl
	err = inst.checkRectangular(g.Nx, g.Ny, r.remaining())
	inst.Lambert = g
	return
}

func (inst *Grid) parseSpaceView(tpl []byte) (err error) {
	r := rd{b: tpl}
	inst.parseShape(&r)
	g := &SpaceViewGrid{}
	g.Nx = r.u32()
	g.Ny = r.u32()
	g.Lap = float64(r.s32()) * microDegrees
	g.Lop = float64(r.s32()) * microDegrees
	g.ResolutionFlags = r.u8()
	g.Dx = r.u32()
	g.Dy = r.u32()
	g.Xp = float64(r.u32()) * 1e-3
	g.Yp = float64(r.u32()) * 1e-3
	inst.Scan = parseScanFlags(r.u8())
	g.Orientation = float64(r.u32()) * microDegrees
	g.Nr = float64(r.u32()) * 1e-6
	g.Xo = r.u32()
	g.Yo = r.u32()
	err = r.truncation("grid template 3.90")
	if err != nil {
		return
	}
	g.UVRelativeToGrid = g.ResolutionFlags&0x08 != 0
	inst.Raw = tpl
	err = inst.checkRectangular(g.Nx, g.Ny, r.remaining())
	inst.SpaceView = g
	return
}

func (inst *Grid) parseUnstructured(tpl []byte) (err error) {
	r := rd{b: tpl}
	g := &UnstructuredGrid{}
	g.NumberOfGridUsed = r.u8()
	g.NumberOfGridInReference = r.u8()
	copy(g.UUID[:], r.take(16))
	err = r.truncation("grid template 3.101")
	if err != nil {
		return
	}
	inst.Raw = tpl
	if inst.OctetsPerPoint != 0 {
		err = unsupported("grid template 3.101 with a point list")
		return
	}
	inst.Unstructured = g
	return
}

// checkRectangular holds a template's Nx × Ny against the section's point
// count and refuses a point list, which the rectangular templates do not
// define.
func (inst *Grid) checkRectangular(nx, ny uint32, extra int) (err error) {
	if uint64(nx)*uint64(ny) != uint64(inst.NumPoints) {
		err = eb.Build().Uint32("nx", nx).Uint32("ny", ny).Uint32("points", inst.NumPoints).Errorf("Nx × Ny is not the point count: %w", ErrInconsistent)
		return
	}
	if inst.OctetsPerPoint != 0 {
		err = unsupported("grid template 3." + strconv.Itoa(int(inst.Template)) + " with a point list")
		return
	}
	if extra != 0 {
		err = eb.Build().Int("extra", extra).Errorf("bytes after the grid template: %w", ErrMalformed)
	}
	return
}

// Dims returns the grid's rectangular dimensions when it has them: Ni (or
// Nx) points per row and Nj (Ny) rows. ok is false for reduced and
// unstructured grids and for templates the reader does not lay out.
func (inst *Grid) Dims() (ni, nj uint32, ok bool) {
	switch {
	case inst.LatLon != nil && inst.LatLon.PL == nil:
		ni, nj, ok = inst.LatLon.Ni, inst.LatLon.Nj, true
	case inst.Lambert != nil:
		ni, nj, ok = inst.Lambert.Nx, inst.Lambert.Ny, true
	case inst.PolarStereographic != nil:
		ni, nj, ok = inst.PolarStereographic.Nx, inst.PolarStereographic.Ny, true
	case inst.Mercator != nil:
		ni, nj, ok = inst.Mercator.Ni, inst.Mercator.Nj, true
	case inst.SpaceView != nil:
		ni, nj, ok = inst.SpaceView.Nx, inst.SpaceView.Ny, true
	}
	return
}

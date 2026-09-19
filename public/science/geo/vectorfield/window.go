package vectorfield

import "math"

// Window is a rectangle of a field at one step, regular in latitude and
// longitude.
//
// What it promises, each clause a defect some tool has shipped (ADR-0249 SD1):
//
//   - U and V are earth-relative east and north, never grid-relative.
//   - Rows run north to south, columns west to east, and West and North are
//     the position of sample (0, 0) — a node, not a pixel edge. Longitudes are
//     in the request's unwrapped frame, so they increase along a row even
//     across the antimeridian.
//   - A missing sample is NaN in U, V and Speed together, and NaN is the only
//     spelling of missing.
//   - Where the data exists the window reaches at least one sample beyond the
//     requested bounds on every side, so interpolating anywhere inside them
//     finds its neighbours.
//   - No more than the request's MaxCols columns and MaxRows rows fall inside
//     the requested bounds.
//
// A window with no columns or rows is valid: the request lay outside a
// regional field.
type Window struct {
	West, North float64
	DLon, DLat  float64 // both positive; row r lies at North - r*DLat
	Cols, Rows  int

	// U and V hold the vector mean of what each sample covers, row-major.
	// Advect with these.
	U, V []float32
	// Speed holds the scalar mean of the magnitude, never shorter than the
	// length of (U, V) and longer where directions disagree within a sample.
	// Colour and read speed from this.
	Speed []float32

	Step int
	// Level is how many times the source halved its native grid to reach this
	// window; a renderer uses it to tell a change of level from a pan.
	Level int
	// Version changes when the same request would return different values.
	Version uint64
}

// LonAt is the longitude of column col, in the window's unwrapped frame.
func (inst *Window) LonAt(col int) (lon float64) {
	return inst.West + float64(col)*inst.DLon
}

// LatAt is the latitude of row row.
func (inst *Window) LatAt(row int) (lat float64) {
	return inst.North - float64(row)*inst.DLat
}

// East is the longitude of the last column.
func (inst *Window) East() (lon float64) {
	return inst.LonAt(inst.Cols - 1)
}

// South is the latitude of the last row.
func (inst *Window) South() (lat float64) {
	return inst.LatAt(inst.Rows - 1)
}

// IsEmpty reports a window without samples.
func (inst *Window) IsEmpty() (empty bool) {
	return inst.Cols <= 0 || inst.Rows <= 0
}

// cell finds the interpolation cell of a position: the column and row of its
// north-west corner and the fractions toward east and south. ok is false when
// the four corners are not all inside the window.
func (inst *Window) cell(lon, lat float64) (col, row int, fx, fy float64, ok bool) {
	if inst.Cols < 2 || inst.Rows < 2 {
		return
	}
	gx := (lon - inst.West) / inst.DLon
	gy := (inst.North - lat) / inst.DLat
	// The comparisons are written so that NaN fails them.
	if !(gx >= 0 && gy >= 0 && gx <= float64(inst.Cols-1) && gy <= float64(inst.Rows-1)) {
		return
	}
	col = int(gx)
	row = int(gy)
	if col > inst.Cols-2 {
		col = inst.Cols - 2
	}
	if row > inst.Rows-2 {
		row = inst.Rows - 2
	}
	fx = gx - float64(col)
	fy = gy - float64(row)
	ok = true
	return
}

// Covers reports whether the window holds all four samples around a
// position, whatever their values.
func (inst *Window) Covers(lon, lat float64) (covers bool) {
	_, _, _, _, covers = inst.cell(lon, lat)
	return
}

// Sample interpolates the field bilinearly by component. It is strict: if any
// of the four samples around the position is missing, or the position is not
// covered, ok is false — a value is never invented from fewer than four, so a
// particle stops a sample short of a coast and never crosses it.
//
// Interpolating components shortens the vector between directions that
// disagree, which is why speed is interpolated from its own plane.
func (inst *Window) Sample(lon, lat float64) (u, v, speed float32, ok bool) {
	col, row, fx, fy, inside := inst.cell(lon, lat)
	if !inside {
		return
	}
	i00 := row*inst.Cols + col
	i10 := i00 + 1
	i01 := i00 + inst.Cols
	i11 := i01 + 1
	u00, u10, u01, u11 := inst.U[i00], inst.U[i10], inst.U[i01], inst.U[i11]
	// NaN in one plane is NaN in all three, so one plane decides.
	if u00 != u00 || u10 != u10 || u01 != u01 || u11 != u11 {
		return
	}
	w00 := float32((1 - fx) * (1 - fy))
	w10 := float32(fx * (1 - fy))
	w01 := float32((1 - fx) * fy)
	w11 := float32(fx * fy)
	u = w00*u00 + w10*u10 + w01*u01 + w11*u11
	v = w00*inst.V[i00] + w10*inst.V[i10] + w01*inst.V[i01] + w11*inst.V[i11]
	speed = w00*inst.Speed[i00] + w10*inst.Speed[i10] + w01*inst.Speed[i01] + w11*inst.Speed[i11]
	ok = true
	return
}

// Constancy is the ratio of the vector mean's length to the scalar mean at a
// sample: 1 where everything the sample covers points one way, toward 0 where
// it does not. NaN for a missing or calm sample.
func (inst *Window) Constancy(col, row int) (constancy float32) {
	i := row*inst.Cols + col
	s := inst.Speed[i]
	if !(s > 0) {
		return float32(math.NaN())
	}
	return float32(math.Hypot(float64(inst.U[i]), float64(inst.V[i]))) / s
}

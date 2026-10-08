package grib

import (
	"iter"
	"math"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Points returns an iterator over the geographic (latitude, longitude) of
// every grid point in stored order — the order of [Field.Values] — for the
// grid templates the reader lays out: 3.0 regular and reduced lat/lon, 3.1
// rotated lat/lon and 3.40 regular and reduced Gaussian, all global or
// rectangular sub-areas of a regular grid. Longitudes are in the range the
// message codes them (0…360 for edition 2); a rotated grid's points are
// un-rotated to geographic coordinates. Every other template, and a reduced
// grid that is not global, is [ErrUnsupported] naming the template
// (ADR-0292 §R2, §R10).
func (inst *Grid) Points() (points iter.Seq2[float64, float64], err error) {
	g := inst.LatLon
	if g == nil {
		err = unsupported("grid template 3." + strconv.Itoa(int(inst.Template)) + " points")
		return
	}
	if g.PL != nil {
		// Global: the rows sum to the point count and the longest row ends
		// one increment short of the first longitude. ECMWF codes sub-areas
		// both ways — global row lengths with a smaller count, and cut rows
		// with a shorter longitude span.
		var maxPL uint32
		for _, n := range g.PL {
			maxPL = max(maxPL, n)
		}
		spanWant := 360 - 360/float64(max(maxPL, 1))
		span := math.Mod(g.Lon2-g.Lon1+720, 360)
		// ECMWF codes the octahedral grid's last longitude as the first
		// one plus a full circle; a sub-area never does.
		fullCircle := span < 1e-3 || span > 360-1e-3
		if g.PLSum != uint64(inst.NumPoints) || maxPL == 0 || (math.Abs(span-spanWant) > 1e-3 && !fullCircle) {
			err = unsupported("grid template 3." + strconv.Itoa(int(inst.Template)) + " points on a reduced sub-area")
			return
		}
	}
	rows, err := inst.rowLatitudes()
	if err != nil {
		return
	}
	if g.PL != nil {
		points = inst.reducedPoints(rows)
		return
	}
	// Regular: Ni columns from Lon1 with increment Di in the coded i
	// direction. When the increment is not given it is derived from the
	// corners, which must then be consistent with Ni.
	di := g.Di
	dir := 1.0
	if inst.Scan.INegative {
		dir = -1
	}
	var derived float64
	if g.Ni >= 2 {
		span := math.Mod(dir*(g.Lon2-g.Lon1)+720, 360)
		derived = span / float64(g.Ni-1)
	}
	if !g.DiGiven || di == 0 {
		di = derived
	} else if g.Ni > 1 {
		// Increments given: the last point must fall where they say. A
		// coded increment is rounded — to the angle unit, or to a
		// millidegree by an encoder converting from edition 1 into a
		// microdegree field — so over many columns it can miss the corner;
		// the corners then define the grid, provided they agree with the
		// increment to within a millidegree.
		reach := math.Mod(g.Lon1+dir*di*float64(g.Ni-1)+720, 360)
		want := math.Mod(g.Lon2+720, 360)
		if diff := math.Abs(reach - want); diff > 1e-3 && math.Abs(diff-360) > 1e-3 {
			if math.Abs(derived-di) > incrementSlack(g.AngleUnit) {
				err = eb.Build().Float64("lon1", g.Lon1).Float64("lon2", g.Lon2).Float64("di", di).Uint32("ni", g.Ni).Errorf("i increment does not reach the last longitude: %w", ErrInconsistent)
				return
			}
			di = derived
		}
	}
	lon0 := g.Lon1
	ni := int(g.Ni)
	points = inst.walk(rows, func(j int) int { return ni }, func(i, j int) (lat, lon float64) {
		lat = rows[j]
		lon = math.Mod(lon0+dir*di*float64(i)+720, 360)
		return
	})
	return
}

// incrementSlack is how far a coded increment may sit from the one the
// corners imply before the two are a contradiction rather than a rounding:
// one and a half coding units, and never less than a millidegree.
func incrementSlack(unit float64) (slack float64) {
	slack = max(1.5*unit, 1.5e-3)
	return
}

// rowLatitudes returns the latitude of each row in stored order (row 0 is
// the first stored row), from the corners for lat/lon and from the Legendre
// roots for Gaussian grids.
func (inst *Grid) rowLatitudes() (rows []float64, err error) {
	g := inst.LatLon
	nj := int(g.Nj)
	rows = make([]float64, nj)
	if inst.Template == 40 {
		if g.N == 0 || int(g.N) > 1<<16 {
			err = eb.Build().Uint32("n", g.N).Errorf("gaussian N not credible: %w", ErrMalformed)
			return
		}
		all := gaussianLatitudes(int(g.N)) // north to south
		if nj == len(all) {
			copy(rows, all)
		} else {
			// A sub-area: the rows between Lat1 and Lat2, inclusive.
			hi, lo := math.Max(g.Lat1, g.Lat2), math.Min(g.Lat1, g.Lat2)
			sub := make([]float64, 0, nj)
			for _, lat := range all {
				if lat <= hi+1e-6 && lat >= lo-1e-6 {
					sub = append(sub, lat)
				}
			}
			if len(sub) != nj {
				err = eb.Build().Int("rowsBetweenCorners", len(sub)).Uint32("nj", g.Nj).Errorf("gaussian rows between the corners do not match Nj: %w", ErrInconsistent)
				return
			}
			copy(rows, sub)
		}
		// Stored order: rows run north to south unless JPositive.
		if inst.Scan.JPositive {
			for i, j := 0, nj-1; i < j; i, j = i+1, j-1 {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
		// The corners must agree with the roots they claim.
		if math.Abs(rows[0]-g.Lat1) > 1e-3 {
			err = eb.Build().Float64("lat1", g.Lat1).Float64("firstRow", rows[0]).Errorf("first latitude is not a gaussian latitude of N: %w", ErrInconsistent)
			return
		}
		return
	}
	dj := g.Dj
	dir := -1.0
	if inst.Scan.JPositive {
		dir = 1
	}
	var derived float64
	if nj >= 2 {
		derived = math.Abs(g.Lat2-g.Lat1) / float64(nj-1)
	}
	if !g.DjGiven || dj == 0 {
		dj = derived
	} else if nj > 1 {
		reach := g.Lat1 + dir*dj*float64(nj-1)
		if math.Abs(reach-g.Lat2) > 1e-3 {
			if math.Abs(derived-dj) > incrementSlack(g.AngleUnit) {
				err = eb.Build().Float64("lat1", g.Lat1).Float64("lat2", g.Lat2).Float64("dj", dj).Uint32("nj", g.Nj).Errorf("j increment does not reach the last latitude: %w", ErrInconsistent)
				return
			}
			dj = derived
		}
	}
	for j := range rows {
		rows[j] = g.Lat1 + dir*dj*float64(j)
	}
	return
}

// reducedPoints lays out a global reduced grid: row j has PL[j] points from
// Lon1 spaced 360/PL[j] in the coded i direction.
func (inst *Grid) reducedPoints(rows []float64) (points iter.Seq2[float64, float64]) {
	g := inst.LatLon
	dir := 1.0
	if inst.Scan.INegative {
		dir = -1
	}
	lon0 := g.Lon1
	points = inst.walk(rows, func(j int) int { return int(g.PL[j]) }, func(i, j int) (lat, lon float64) {
		lat = rows[j]
		lon = math.Mod(lon0+dir*360/float64(g.PL[j])*float64(i)+720, 360)
		return
	})
	return
}

// walk visits the grid in stored order, honouring the four scan flags: rows
// of rowLen(j) points, columns first when JConsecutive, every second row
// reversed when Alternating. The (i, j) passed to at are the coded
// positions, so at applies the i direction itself. Rotated grids are
// un-rotated on the way out.
func (inst *Grid) walk(rows []float64, rowLen func(j int) int, at func(i, j int) (lat, lon float64)) (points iter.Seq2[float64, float64]) {
	rot := inst.LatLon.Rotated
	nj := len(rows)
	emit := func(yield func(float64, float64) bool, i, j int) bool {
		lat, lon := at(i, j)
		if rot != nil {
			lat, lon = unrotate(lat, lon, rot)
		}
		return yield(lat, lon)
	}
	if inst.Scan.JConsecutive {
		// Column-major: only meaningful with a constant row length.
		ni := rowLen(0)
		points = func(yield func(float64, float64) bool) {
			for i := 0; i < ni; i++ {
				for j := 0; j < nj; j++ {
					jj := j
					if inst.Scan.Alternating && i%2 == 1 {
						jj = nj - 1 - j
					}
					if !emit(yield, i, jj) {
						return
					}
				}
			}
		}
		return
	}
	points = func(yield func(float64, float64) bool) {
		for j := 0; j < nj; j++ {
			ni := rowLen(j)
			for i := 0; i < ni; i++ {
				ii := i
				if inst.Scan.Alternating && j%2 == 1 {
					ii = ni - 1 - i
				}
				if !emit(yield, ii, j) {
					return
				}
			}
		}
	}
	return
}

// unrotate maps a point of a rotated lat/lon system to geographic
// coordinates (template 3.1). The rotated system's north pole is antipodal
// to the coded south pole, and its zero meridian is the great circle through
// both poles that passes the geographic meridian of the south pole: the
// rotated origin lies 90° north of the rotated south pole along that
// meridian. The formulas follow from that geometry; the fixture with a
// rotated grid holds them against the oracle's un-rotated coordinates.
func unrotate(rlat, rlon float64, rot *Rotation) (lat, lon float64) {
	const d2r = math.Pi / 180
	poleLat := -rot.SouthPoleLat * d2r
	poleLon := (rot.SouthPoleLon + 180) * d2r
	sinP, cosP := math.Sin(poleLat), math.Cos(poleLat)
	sinF, cosF := math.Sin(rlat*d2r), math.Cos(rlat*d2r)
	sinL, cosL := math.Sin(rlon*d2r), math.Cos(rlon*d2r)
	lat = math.Asin(math.Max(-1, math.Min(1, sinF*sinP+cosF*cosL*cosP))) / d2r
	lon = (poleLon - math.Atan2(cosF*sinL, sinF*cosP-cosF*cosL*sinP)) / d2r
	lon = math.Mod(lon+720, 360)
	return
}

// Raster returns the field's values re-ordered to a west-to-east,
// north-to-south raster of Nj rows of Ni values, with the scan flags
// applied exactly once (ADR-0292 §R2). It exists for grids with
// rectangular dimensions; reduced and unstructured grids are
// [ErrUnsupported]. The values themselves come from [Field.Values].
func (inst *Field) Raster(dst []float64) (raster []float64, ni, nj int, err error) {
	niU, njU, ok := inst.Grid.Dims()
	if !ok {
		err = unsupported("grid template 3." + strconv.Itoa(int(inst.Grid.Template)) + " raster")
		return
	}
	ni, nj = int(niU), int(njU)
	stored, err := inst.Values(nil)
	if err != nil {
		return
	}
	n := ni * nj
	if cap(dst) < n {
		dst = make([]float64, n)
	}
	raster = dst[:n]
	sc := inst.Grid.Scan
	k := 0
	// Stored index k corresponds to coded (i, j); the raster puts column i
	// counted west to east and row j counted north to south.
	place := func(i, j int, v float64) {
		if sc.INegative {
			i = ni - 1 - i
		}
		if sc.JPositive {
			j = nj - 1 - j
		}
		raster[j*ni+i] = v
	}
	if sc.JConsecutive {
		for i := 0; i < ni; i++ {
			for j := 0; j < nj; j++ {
				jj := j
				if sc.Alternating && i%2 == 1 {
					jj = nj - 1 - j
				}
				place(i, jj, stored[k])
				k++
			}
		}
		return
	}
	for j := 0; j < nj; j++ {
		for i := 0; i < ni; i++ {
			ii := i
			if sc.Alternating && j%2 == 1 {
				ii = ni - 1 - i
			}
			place(ii, j, stored[k])
			k++
		}
	}
	return
}

// LatLonRaster describes the raster [Field.Raster] returns for a regular,
// unrotated lat/lon grid (templates 3.0 and 3.40 with a constant row
// length): the longitude of its western column and the latitude of its
// northern row, the increments, and the dimensions. Longitudes are in the
// grid's own range (0…360 in edition 2), so a caller normalises a query
// longitude into [West, West+360) before dividing. Gaussian rows are not
// equally spaced; for them Dj is the mean spacing and Rows carries every
// row's latitude north to south.
type LatLonRaster struct {
	West, North float64
	Di, Dj      float64
	Ni, Nj      int
	Rows        []float64
}

// LatLonRaster returns the raster geometry, or [ErrUnsupported] for a
// grid that is not a regular unrotated lat/lon or Gaussian grid.
func (inst *Grid) LatLonRaster() (r LatLonRaster, err error) {
	g := inst.LatLon
	if g == nil || g.PL != nil || g.Rotated != nil || g.Ni < 1 || g.Nj < 1 {
		err = unsupported("grid template 3." + strconv.Itoa(int(inst.Template)) + " raster geometry")
		return
	}
	rows, err := inst.rowLatitudes()
	if err != nil {
		return
	}
	// rows are in stored order; the raster is north to south.
	r.Rows = make([]float64, len(rows))
	copy(r.Rows, rows)
	if inst.Scan.JPositive {
		for i, j := 0, len(r.Rows)-1; i < j; i, j = i+1, j-1 {
			r.Rows[i], r.Rows[j] = r.Rows[j], r.Rows[i]
		}
	}
	r.Ni, r.Nj = int(g.Ni), int(g.Nj)
	r.North = r.Rows[0]
	if r.Nj > 1 {
		r.Dj = (r.Rows[0] - r.Rows[r.Nj-1]) / float64(r.Nj-1)
	}
	// Columns: the western one is Lon1 when i runs east, else Lon2.
	di := g.Di
	if !g.DiGiven || di == 0 {
		if r.Ni > 1 {
			dir := 1.0
			if inst.Scan.INegative {
				dir = -1
			}
			di = math.Mod(dir*(g.Lon2-g.Lon1)+720, 360) / float64(r.Ni-1)
		}
	}
	r.Di = di
	r.West = g.Lon1
	if inst.Scan.INegative {
		r.West = g.Lon2
	}
	r.West = math.Mod(r.West+720, 360)
	return
}

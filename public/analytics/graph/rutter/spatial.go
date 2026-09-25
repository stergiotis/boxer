package rutter

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Polylines is a set of planar polylines in one flat vertex array:
// polyline i is the vertices [First[i], First[i+1]). Coordinates are
// float32 in whatever planar unit the caller uses — a country's extent in
// metres keeps a quarter-metre of precision, and a set of tens of millions
// of vertices is what the width is for; distances come back as float64 in
// the same unit.
type Polylines struct {
	First []int32
	X, Y  []float32
}

// NumPolylines is the polyline count.
func (inst Polylines) NumPolylines() int { return len(inst.First) - 1 }

// Snap is one answer of [Index.Nearest]: the polyline, the closest point on
// it, the distance to it, and how far along its length that point lies as
// a fraction in [0, 1].
type Snap struct {
	Polyline int32
	X, Y     float64
	Dist     float64
	Fraction float64
}

// Index is a fixed-cell grid over the polylines' bounding boxes (ADR-0256
// §SD6): each polyline is listed in every cell its box touches, and a
// nearest query scans the cells within the radius.
type Index struct {
	lines  Polylines
	cell   float64
	minX   float64
	minY   float64
	cols   int32
	rows   int32
	first  []int32 // per cell
	member []int32 // polyline indices
	stamp  []uint32
	gen    uint32
}

// NewIndexE builds the grid with the given cell size. A polyline with no
// vertex is refused, because it has no box.
func NewIndexE(lines Polylines, cell float64) (inst *Index, err error) {
	if len(lines.First) < 1 || int(lines.First[len(lines.First)-1]) != len(lines.X) || len(lines.X) != len(lines.Y) {
		err = eb.Build().Int("offsets", len(lines.First)).Int("x", len(lines.X)).Int("y", len(lines.Y)).Errorf("the polyline arrays do not describe a set")
		return
	}
	if !(cell > 0) {
		err = eb.Build().Float64("cell", cell).Errorf("the cell size must be positive")
		return
	}
	k := lines.NumPolylines()
	for i := range k {
		if lines.First[i] >= lines.First[i+1] {
			err = eb.Build().Int("polyline", i).Errorf("a polyline without a vertex")
			return
		}
	}
	inst = &Index{lines: lines, cell: cell, stamp: make([]uint32, k)}
	if len(lines.X) == 0 {
		inst.first = make([]int32, 1)
		return inst, nil
	}
	inst.minX, inst.minY = math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for i := range lines.X {
		inst.minX = min(inst.minX, float64(lines.X[i]))
		inst.minY = min(inst.minY, float64(lines.Y[i]))
		maxX = max(maxX, float64(lines.X[i]))
		maxY = max(maxY, float64(lines.Y[i]))
	}
	inst.cols = int32(math.Floor((maxX-inst.minX)/cell)) + 1
	inst.rows = int32(math.Floor((maxY-inst.minY)/cell)) + 1
	cells := int64(inst.cols) * int64(inst.rows)
	if cells > math.MaxInt32/2 {
		err = eb.Build().Int64("cells", cells).Errorf("the cell size is too small for the extent")
		return
	}
	inst.first = make([]int32, cells+1)
	boxes := make([][4]int32, k)
	for i := range k {
		lo, hi := lines.First[i], lines.First[i+1]
		bx0, by0, bx1, by1 := lines.X[lo], lines.Y[lo], lines.X[lo], lines.Y[lo]
		for p := lo + 1; p < hi; p++ {
			bx0, by0 = min(bx0, lines.X[p]), min(by0, lines.Y[p])
			bx1, by1 = max(bx1, lines.X[p]), max(by1, lines.Y[p])
		}
		c0, r0 := inst.cellOf(float64(bx0), float64(by0))
		c1, r1 := inst.cellOf(float64(bx1), float64(by1))
		boxes[i] = [4]int32{c0, r0, c1, r1}
		for r := r0; r <= r1; r++ {
			for c := c0; c <= c1; c++ {
				inst.first[inst.cellIndex(c, r)+1]++
			}
		}
	}
	for i := range cells {
		inst.first[i+1] += inst.first[i]
	}
	inst.member = make([]int32, inst.first[cells])
	fill := make([]int32, cells)
	copy(fill, inst.first[:cells])
	for i := range k {
		b := boxes[i]
		for r := b[1]; r <= b[3]; r++ {
			for c := b[0]; c <= b[2]; c++ {
				ci := inst.cellIndex(c, r)
				inst.member[fill[ci]] = int32(i)
				fill[ci]++
			}
		}
	}
	return inst, nil
}

func (inst *Index) cellOf(x, y float64) (c, r int32) {
	c = int32(math.Floor((x - inst.minX) / inst.cell))
	r = int32(math.Floor((y - inst.minY) / inst.cell))
	c = min(max(c, 0), inst.cols-1)
	r = min(max(r, 0), inst.rows-1)
	return
}

func (inst *Index) cellIndex(c, r int32) int64 { return int64(r)*int64(inst.cols) + int64(c) }

// Nearest is the polyline closest to (x, y) within radius, or ok=false when
// none is. Every candidate in the cells the radius touches is measured, so
// the answer is exact for the radius; a polyline is measured once per
// query however many cells list it.
func (inst *Index) Nearest(x, y, radius float64) (s Snap, ok bool) {
	return inst.NearestWhere(x, y, radius, nil)
}

// NearestWhere is [Index.Nearest] over the polylines accept admits; a nil
// accept admits all. A caller snapping under a profile passes the
// profile's passability, so a point beside a motorway snaps to the path
// beyond it.
func (inst *Index) NearestWhere(x, y, radius float64, accept func(polyline int32) bool) (s Snap, ok bool) {
	if len(inst.lines.X) == 0 {
		return s, false
	}
	inst.gen++
	if inst.gen == 0 {
		clear(inst.stamp)
		inst.gen = 1
	}
	c0, r0 := inst.cellOf(x-radius, y-radius)
	c1, r1 := inst.cellOf(x+radius, y+radius)
	best := radius
	for r := r0; r <= r1; r++ {
		for c := c0; c <= c1; c++ {
			ci := inst.cellIndex(c, r)
			for p := inst.first[ci]; p < inst.first[ci+1]; p++ {
				i := inst.member[p]
				if inst.stamp[i] == inst.gen {
					continue
				}
				inst.stamp[i] = inst.gen
				if accept != nil && !accept(i) {
					continue
				}
				cand := inst.measure(i, x, y)
				if cand.Dist <= best && (!ok || cand.Dist < s.Dist) {
					s, ok, best = cand, true, cand.Dist
				}
			}
		}
	}
	return s, ok
}

// measure is the closest point on polyline i to (x, y).
func (inst *Index) measure(i int32, x, y float64) (s Snap) {
	lo, hi := inst.lines.First[i], inst.lines.First[i+1]
	s.Polyline = i
	s.Dist = math.Inf(1)
	X, Y := inst.lines.X, inst.lines.Y
	total := 0.0
	for p := lo; p+1 < hi; p++ {
		total += math.Hypot(float64(X[p+1]-X[p]), float64(Y[p+1]-Y[p]))
	}
	if hi-lo == 1 || total == 0 {
		s.X, s.Y = float64(X[lo]), float64(Y[lo])
		s.Dist = math.Hypot(x-s.X, y-s.Y)
		return
	}
	walked := 0.0
	for p := lo; p+1 < hi; p++ {
		ax, ay, bx, by := float64(X[p]), float64(Y[p]), float64(X[p+1]), float64(Y[p+1])
		dx, dy := bx-ax, by-ay
		seg := math.Hypot(dx, dy)
		t := 0.0
		if seg > 0 {
			t = ((x-ax)*dx + (y-ay)*dy) / (seg * seg)
			t = min(max(t, 0), 1)
		}
		px, py := ax+t*dx, ay+t*dy
		d := math.Hypot(x-px, y-py)
		if d < s.Dist {
			s.X, s.Y, s.Dist = px, py, d
			// Clamped: the two sums are the same numbers added in another
			// order, and rounding can put the last vertex a hair past 1.
			s.Fraction = min(max((walked+t*seg)/total, 0), 1)
		}
		walked += seg
	}
	return
}

// Length is the planar length of polyline i.
func (inst Polylines) Length(i int32) (l float64) {
	lo, hi := inst.First[i], inst.First[i+1]
	for p := lo; p+1 < hi; p++ {
		l += math.Hypot(float64(inst.X[p+1]-inst.X[p]), float64(inst.Y[p+1]-inst.Y[p]))
	}
	return
}

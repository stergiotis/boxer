package graphview

import "math"

// Iso-line extraction for the aura grids (ADR-0224 §SD11): marching squares
// over the cell-centre samples with linear interpolation along cell edges,
// directed so every ring comes out with a consistent winding, then Chaikin
// smoothing. Sample (i, j) of a gw×gh grid sits at canvas point
// ((i+0.5)·cs, (j+0.5)·cs); samples outside the grid count as outside, so
// every ring closes.

// isoSourceI is what the tracer asks of a field.
type isoSourceI interface {
	// inside reports whether sample (i, j) belongs to the region.
	inside(i, j int32) bool
	// frac returns where on the edge from inside sample a to outside sample
	// b the boundary lies, as a fraction in (0, 1].
	frac(ai, aj, bi, bj int32) float32
	// saddle resolves a square whose diagonal corners are inside: true
	// joins them, false keeps them apart.
	saddle(i, j int32) bool
}

// rings is a set of closed polylines in canvas pixels, flattened: ring r is
// xs[start[r]:start[r+1]].
type rings struct {
	xs, ys []float32
	start  []int32
}

func (r *rings) reset() {
	r.xs, r.ys, r.start = r.xs[:0], r.ys[:0], r.start[:0]
}

func (r *rings) count() int {
	if len(r.start) == 0 {
		return 0
	}
	return len(r.start) - 1
}

func (r *rings) ring(i int) (xs, ys []float32) {
	return r.xs[r.start[i]:r.start[i+1]], r.ys[r.start[i]:r.start[i+1]]
}

// close ends the ring under construction; a ring under three points is
// dropped.
func (r *rings) close(startLen int) {
	if len(r.xs)-startLen < 3 {
		r.xs, r.ys = r.xs[:startLen], r.ys[:startLen]
		return
	}
	if len(r.start) == 0 {
		r.start = append(r.start, 0)
	}
	r.start = append(r.start, int32(len(r.xs)))
}

// tracer holds the marching-squares scratch. Boundary points are indexed by
// the edge they sit on — horizontal edges join (i, j) and (i+1, j), vertical
// edges join (i, j) and (i, j+1) — over the grid padded by one sample on
// every side so the outside ring is addressable.
type tracer struct {
	gw, gh  int32
	hIdx    []int32 // (gw+1)·(gh+2) entries: i ∈ [-1, gw-1], j ∈ [-1, gh]; -1 = no point
	vIdx    []int32 // (gw+2)·(gh+1) entries: i ∈ [-1, gw], j ∈ [-1, gh-1]
	px, py  []float32
	next    []int32
	visited []bool
	touched []int32 // set entries of hIdx (k) and vIdx (−k−1), for the cheap reset
}

func (t *tracer) setup(gw, gh int32) {
	nh := int((gw + 1) * (gh + 2))
	nv := int((gw + 2) * (gh + 1))
	if t.gw != gw || t.gh != gh || cap(t.hIdx) < nh || cap(t.vIdx) < nv {
		t.hIdx = growTo(t.hIdx, nh)
		t.vIdx = growTo(t.vIdx, nv)
		for i := range t.hIdx {
			t.hIdx[i] = -1
		}
		for i := range t.vIdx {
			t.vIdx[i] = -1
		}
	} else {
		for _, k := range t.touched {
			if k >= 0 {
				t.hIdx[k] = -1
			} else {
				t.vIdx[-k-1] = -1
			}
		}
	}
	t.gw, t.gh = gw, gh
	t.touched = t.touched[:0]
	t.px, t.py, t.next = t.px[:0], t.py[:0], t.next[:0]
}

func (t *tracer) hKey(i, j int32) int32 { return (j+1)*(t.gw+1) + (i + 1) }
func (t *tracer) vKey(i, j int32) int32 { return (j+1)*(t.gw+2) + (i + 1) }

// edgePoint returns the point on the edge between samples a and b — one
// inside, one outside — creating it at the interpolated position.
func (t *tracer) edgePoint(src isoSourceI, cs float32, ai, aj, bi, bj int32) int32 {
	var slot *int32
	var key int32
	if aj == bj {
		key = t.hKey(min(ai, bi), aj)
		slot = &t.hIdx[key]
	} else {
		k := t.vKey(ai, min(aj, bj))
		slot = &t.vIdx[k]
		key = -k - 1
	}
	if *slot >= 0 {
		return *slot
	}
	if !src.inside(ai, aj) {
		ai, aj, bi, bj = bi, bj, ai, aj
	}
	f := src.frac(ai, aj, bi, bj)
	idx := int32(len(t.px))
	t.px = append(t.px, (float32(ai)+0.5)*cs+f*float32(bi-ai)*cs)
	t.py = append(t.py, (float32(aj)+0.5)*cs+f*float32(bj-aj)*cs)
	t.next = append(t.next, -1)
	*slot = idx
	t.touched = append(t.touched, key)
	return idx
}

// squareCorners lists a square's corners clockwise from its top-left
// sample; edge k joins corner k and corner k+1.
var squareCorners = [4][2]int32{{0, 0}, {1, 0}, {1, 1}, {0, 1}}

// trace runs marching squares over the squares whose top-left sample lies
// in [i0−1, i1] × [j0−1, j1] — the inside samples' bbox, padded — and
// appends every closed ring to out. A single inside corner k emits the
// directed segment from edge k−1 to edge k, two adjacent inside corners k,
// k+1 emit edge k−1 to edge k+1, the complements reverse, and saddles
// follow the source's verdict. The winding that results gives an outer
// boundary a negative shoelace area in screen coordinates and a hole a
// positive one.
func (t *tracer) trace(src isoSourceI, cs float32, i0, j0, i1, j1 int32, out *rings) {
	seg := func(i, j int32, eFrom, eTo int) {
		a, b := squareCorners[(eFrom+4)%4], squareCorners[(eFrom+5)%4]
		p := t.edgePoint(src, cs, i+a[0], j+a[1], i+b[0], j+b[1])
		a, b = squareCorners[(eTo+4)%4], squareCorners[(eTo+5)%4]
		q := t.edgePoint(src, cs, i+a[0], j+a[1], i+b[0], j+b[1])
		t.next[p] = q
	}
	for j := j0 - 1; j <= j1; j++ {
		for i := i0 - 1; i <= i1; i++ {
			var m uint8
			for k, cc := range squareCorners {
				if src.inside(i+cc[0], j+cc[1]) {
					m |= 1 << k
				}
			}
			switch m {
			case 0, 15:
			case 1, 2, 4, 8:
				k := bitIndex(m)
				seg(i, j, k-1, k)
			case 14, 13, 11, 7:
				k := bitIndex(^m & 15)
				seg(i, j, k, k-1)
			case 3, 6, 12, 9:
				k := pairStart(m)
				seg(i, j, k-1, k+1)
			case 5: // corners 0 and 2
				if src.saddle(i, j) {
					seg(i, j, 1, 0) // complement of corner 1
					seg(i, j, 3, 2) // complement of corner 3
				} else {
					seg(i, j, 3, 0) // corner 0
					seg(i, j, 1, 2) // corner 2
				}
			case 10: // corners 1 and 3
				if src.saddle(i, j) {
					seg(i, j, 0, 3) // complement of corner 0
					seg(i, j, 2, 1) // complement of corner 2
				} else {
					seg(i, j, 0, 1) // corner 1
					seg(i, j, 2, 3) // corner 3
				}
			}
		}
	}
	n := len(t.px)
	t.visited = growTo(t.visited, n)
	clear(t.visited)
	for s := range n {
		if t.visited[s] || t.next[s] < 0 {
			continue
		}
		startLen := len(out.xs)
		for p := int32(s); p >= 0 && !t.visited[p]; p = t.next[p] {
			t.visited[p] = true
			out.xs = append(out.xs, t.px[p])
			out.ys = append(out.ys, t.py[p])
		}
		out.close(startLen)
	}
}

func bitIndex(m uint8) int {
	for k := range 4 {
		if m&(1<<k) != 0 {
			return k
		}
	}
	return 0
}

// pairStart returns k for a mask of the two adjacent corners k and k+1.
func pairStart(m uint8) int {
	switch m {
	case 3:
		return 0
	case 6:
		return 1
	case 12:
		return 2
	default: // 9: corners 3 and 0
		return 3
	}
}

// signedArea is the shoelace area of a closed ring; negative is the winding
// the tracer gives an outer boundary in screen coordinates.
func signedArea(xs, ys []float32) float32 {
	var a float64
	n := len(xs)
	for i := range n {
		j := (i + 1) % n
		a += float64(xs[i])*float64(ys[j]) - float64(xs[j])*float64(ys[i])
	}
	return float32(a / 2)
}

// chaikin appends one corner-cutting pass over the closed ring: every edge
// becomes the points a quarter and three quarters along it, so n points
// become 2n and every corner rounds off.
func chaikin(xs, ys []float32, dstX, dstY []float32) ([]float32, []float32) {
	n := len(xs)
	for i := range n {
		j := (i + 1) % n
		dstX = append(dstX, 0.75*xs[i]+0.25*xs[j], 0.25*xs[i]+0.75*xs[j])
		dstY = append(dstY, 0.75*ys[i]+0.25*ys[j], 0.25*ys[i]+0.75*ys[j])
	}
	return dstX, dstY
}

// smoothRingPoints is the vertex count above which a ring gets one Chaikin
// pass instead of two, so a huge aura does not become a huge polygon.
const smoothRingPoints = 400

// ringMinArea is the area, in square cells, below which a ring is noise and
// is not kept.
const ringMinArea = 0.5

// appendSmoothed adds the ring to out after one or two Chaikin passes; sx,
// sy are scratch and are returned grown.
func appendSmoothed(xs, ys []float32, out *rings, sx, sy []float32) ([]float32, []float32) {
	startLen := len(out.xs)
	if len(xs) > smoothRingPoints {
		out.xs, out.ys = chaikin(xs, ys, out.xs, out.ys)
	} else {
		sx, sy = chaikin(xs, ys, sx[:0], sy[:0])
		out.xs, out.ys = chaikin(sx, sy, out.xs, out.ys)
	}
	out.close(startLen)
	return sx, sy
}

func ringTooSmall(area, cs float32) bool {
	return float32(math.Abs(float64(area))) < ringMinArea*cs*cs
}

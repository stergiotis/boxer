package sqlfield

import (
	"math"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
)

const (
	// gutter is how many bins a window reaches beyond the requested bounds.
	gutter = 1
	// maxFactor stops the doubling for a request coarser than any grid.
	maxFactor = 1 << 30
	// periodTolerance is the slack, in cells, within which a grid's width is
	// taken to be the full circle.
	periodTolerance = 1e-3
)

// grid is a relation's native geometry, read once by the describe step.
type grid struct {
	west, north float64 // the position of node (0, 0)
	dLon, dLat  float64
	// cols and rows count the native nodes. For a periodic grid cols is the
	// period: a repeated last column is not counted.
	cols, rows int
	periodic   bool
}

func (inst grid) east() float64  { return inst.west + float64(inst.cols-1)*inst.dLon }
func (inst grid) south() float64 { return inst.north - float64(inst.rows-1)*inst.dLat }

// windowPlan is a request turned into what the window statement needs and
// what the reply is laid into. Native columns are addressed in an unwrapped
// frame — column c of turn n is c + n*cols — so a window across the seam of a
// periodic grid is contiguous.
type windowPlan struct {
	empty bool

	cols, rows int // the window, in bins
	factor     int // native nodes per bin along each axis
	level      int // log2 of factor

	colStart, colEnd int64 // unwrapped native columns, inclusive
	rowStart, rowEnd int64 // native rows, inclusive

	// turns are the offsets that carry a native column into the unwrapped
	// frame; one zero for a regional grid.
	turns []int64
	// lonRanges and latRange bound the native nodes in the relation's own
	// coordinates, half a cell wide of the outermost node. The second
	// longitude range is empty (west > east) unless the window crosses the
	// seam.
	lonRanges [2][2]float64
	latRange  [2]float64

	// west and north are the position of bin (0, 0) in the request's frame.
	west, north float64
	dLon, dLat  float64
}

// plan lays a request over the grid. Bins are aligned to the grid's origin
// and not to the request, so a pan re-reads the same samples, and the factor
// is a power of two, so a window is never finer than asked and less than
// twice as coarse — the band a renderer's hysteresis is built around.
func (inst grid) plan(req vectorfield.Request) (p windowPlan) {
	shift := 0.0
	if !inst.periodic {
		// A regional grid may be stored in the other longitude convention
		// than the request uses; move the request onto it.
		shift = 360 * math.Round(((inst.west+inst.east())/2-(req.West+req.East)/2)/360)
	}
	west, east := req.West+shift, req.East+shift

	wantLon := (east - west) / float64(req.MaxCols)
	wantLat := (req.North - req.South) / float64(req.MaxRows)
	f := 1
	for f < maxFactor && (float64(f)*inst.dLon < wantLon || float64(f)*inst.dLat < wantLat) {
		f *= 2
		p.level++
	}
	p.factor = f

	// Bin g covers native columns [g*f, g*f+f) and sits at their centre.
	centre := float64(f-1) / 2
	stepLon := float64(f) * inst.dLon
	stepLat := float64(f) * inst.dLat
	binLon0 := inst.west + centre*inst.dLon
	binLat0 := inst.north - centre*inst.dLat

	g0 := int64(math.Floor((west-binLon0)/stepLon)) - gutter
	g1 := int64(math.Ceil((east-binLon0)/stepLon)) + gutter
	r0 := int64(math.Floor((binLat0-req.North)/stepLat)) - gutter
	r1 := int64(math.Ceil((binLat0-req.South)/stepLat)) + gutter

	r0 = max(r0, 0)
	r1 = min(r1, int64(inst.rows-1)/int64(f))
	if inst.periodic {
		// One turn and its gutters; more would repeat itself.
		turn := int64(math.Ceil(360/stepLon)) + 2*gutter + 1
		if g1-g0+1 > turn {
			g1 = g0 + turn - 1
		}
	} else {
		g0 = max(g0, 0)
		g1 = min(g1, int64(inst.cols-1)/int64(f))
	}
	if g1 < g0 || r1 < r0 {
		p.empty = true
		return
	}

	p.cols = int(g1 - g0 + 1)
	p.rows = int(r1 - r0 + 1)
	p.colStart = g0 * int64(f)
	p.colEnd = (g1+1)*int64(f) - 1
	p.rowStart = r0 * int64(f)
	p.rowEnd = min((r1+1)*int64(f)-1, int64(inst.rows-1))
	p.west = binLon0 + float64(g0)*stepLon - shift
	p.north = binLat0 - float64(r0)*stepLat
	p.dLon = stepLon
	p.dLat = stepLat

	p.latRange = [2]float64{
		inst.north - (float64(p.rowEnd)+0.5)*inst.dLat,
		inst.north - (float64(p.rowStart)-0.5)*inst.dLat,
	}
	lonOf := func(col int64) float64 { return inst.west + float64(col)*inst.dLon }
	span := func(a, b int64) [2]float64 {
		return [2]float64{lonOf(a) - inst.dLon/2, lonOf(b) + inst.dLon/2}
	}
	none := [2]float64{1, 0}
	if !inst.periodic {
		p.colEnd = min(p.colEnd, int64(inst.cols-1))
		p.turns = []int64{0}
		p.lonRanges = [2][2]float64{span(p.colStart, p.colEnd), none}
		return
	}
	period := int64(inst.cols)
	n0, n1 := floorDiv(p.colStart, period), floorDiv(p.colEnd, period)
	for n := n0; n <= n1; n++ {
		p.turns = append(p.turns, n*period)
	}
	a, b := p.colStart-n0*period, p.colEnd-n1*period
	switch {
	case p.colEnd-p.colStart+1 >= period:
		p.lonRanges = [2][2]float64{span(0, period-1), none}
	case a <= b:
		p.lonRanges = [2][2]float64{span(a, b), none}
	default:
		p.lonRanges = [2][2]float64{span(a, period-1), span(0, b)}
	}
	return
}

// cellsIn is how many native nodes bin (wc, wr) of a plan covers inside the
// grid: fewer than factor² in the last row and column of a regional grid.
func (inst grid) cellsIn(p *windowPlan, wc, wr int) (cells int) {
	f := int64(p.factor)
	ny := min(f, int64(inst.rows)-(p.rowStart+int64(wr)*f))
	nx := f
	if !inst.periodic {
		nx = min(f, int64(inst.cols)-(p.colStart+int64(wc)*f))
	}
	if nx <= 0 || ny <= 0 {
		return 0
	}
	return int(nx * ny)
}

func floorDiv(a, b int64) (q int64) {
	q = a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return
}

// isFullCircle reports a width of cols cells of dLon that closes on itself.
func isFullCircle(cols int, dLon float64) bool {
	return math.Abs(float64(cols)*dLon-360) <= periodTolerance*dLon
}

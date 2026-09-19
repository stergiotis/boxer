package flowoverlay

import (
	"math"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// worldZoom is the zoom whose projected pixels are the layer's world units.
// Any fixed zoom would do; zero keeps the numbers small.
const worldZoom = 0

// projectionI is what the layer needs of a map view: the projection at a
// fixed zoom, in both directions. *portolan.View satisfies it.
type projectionI interface {
	ProjectAt(ll portolan.LatLng, zoom float64) portolan.Point
	UnprojectAt(p portolan.Point, zoom float64) portolan.LatLng
}

var _ projectionI = (*portolan.View)(nil)

// fieldGrid is a window resampled onto a grid regular in world units, with
// each vector carried through the projection. x grows east and y grows the
// way the projection's y does, which for a map is down the screen.
//
// vx and vy hold the direction of the vector mean as the projection shows it,
// scaled back to the vector mean's own length: direction goes through the
// projection, magnitude does not (ADR-0249 SD4). speed is the scalar mean.
// A missing sample is NaN in all three.
type fieldGrid struct {
	x0, y0     float64 // world position of sample (0, 0)
	dx, dy     float64 // both positive
	cols, rows int
	vx, vy     []float32
	speed      []float32
}

// latLimit keeps the finite-difference step off the poles, where east is not
// a direction.
const latLimit = 89.9

// newFieldGrid resamples win. The grid spans the window and takes the
// window's longitude spacing as its own, square in world units, so it is as
// fine as the window where the window is finest.
func newFieldGrid(win *vectorfield.Window, proj projectionI) (grid *fieldGrid) {
	if win.IsEmpty() || win.Cols < 2 || win.Rows < 2 {
		return nil
	}
	north := math.Min(win.North, latLimit)
	south := math.Max(win.South(), -latLimit)
	if !(south < north) {
		return nil
	}
	nw := proj.ProjectAt(portolan.LL(north, win.West), worldZoom)
	se := proj.ProjectAt(portolan.LL(south, win.East()), worldZoom)
	if !(se.X > nw.X) || !(se.Y > nw.Y) {
		return nil
	}
	cell := (se.X - nw.X) / float64(win.Cols-1)
	grid = &fieldGrid{x0: nw.X, y0: nw.Y, dx: cell, dy: cell}
	grid.cols = win.Cols
	grid.rows = int(math.Floor((se.Y-nw.Y)/cell)) + 1
	if grid.rows < 2 {
		return nil
	}
	n := grid.cols * grid.rows
	grid.vx = make([]float32, n)
	grid.vy = make([]float32, n)
	grid.speed = make([]float32, n)
	nan := float32(math.NaN())

	const eps = 1e-4 // degrees
	for r := range grid.rows {
		y := grid.y0 + float64(r)*grid.dy
		for c := range grid.cols {
			i := r*grid.cols + c
			p := portolan.Pt(grid.x0+float64(c)*grid.dx, y)
			ll := proj.UnprojectAt(p, worldZoom)
			// The grid's edge is the window's edge; rounding must not put a
			// node a hair outside it.
			ll.Lat = min(max(ll.Lat, south), north)
			ll.Lng = min(max(ll.Lng, win.West), win.East())
			u, v, s, ok := win.Sample(ll.Lng, ll.Lat)
			if !ok {
				grid.vx[i], grid.vy[i], grid.speed[i] = nan, nan, nan
				continue
			}
			// The projection's Jacobian by finite differences: where a step
			// east and a step north of equal ground length land in world
			// units. A degree of longitude is cos(lat) of a degree of latitude.
			cosLat := math.Cos(ll.Lat * math.Pi / 180)
			pe := proj.ProjectAt(portolan.LL(ll.Lat, ll.Lng+eps), worldZoom)
			pn := proj.ProjectAt(portolan.LL(ll.Lat+eps, ll.Lng), worldZoom)
			ex, ey := (pe.X-p.X)/cosLat, (pe.Y-p.Y)/cosLat
			nx, ny := pn.X-p.X, pn.Y-p.Y
			wx := float64(u)*ex + float64(v)*nx
			wy := float64(u)*ey + float64(v)*ny
			length := math.Hypot(wx, wy)
			mean := math.Hypot(float64(u), float64(v))
			if !(length > 0) {
				grid.vx[i], grid.vy[i], grid.speed[i] = 0, 0, s
				continue
			}
			grid.vx[i] = float32(wx / length * mean)
			grid.vy[i] = float32(wy / length * mean)
			grid.speed[i] = s
		}
	}
	return
}

// x1 and y1 are the world position of the last sample.
func (inst *fieldGrid) x1() float64 { return inst.x0 + float64(inst.cols-1)*inst.dx }
func (inst *fieldGrid) y1() float64 { return inst.y0 + float64(inst.rows-1)*inst.dy }

// sample interpolates bilinearly and strictly: a missing corner is a missing
// value.
func (inst *fieldGrid) sample(x, y float64) (vx, vy, speed float32, ok bool) {
	gx := (x - inst.x0) / inst.dx
	gy := (y - inst.y0) / inst.dy
	if !(gx >= 0 && gy >= 0 && gx <= float64(inst.cols-1) && gy <= float64(inst.rows-1)) {
		return
	}
	c := min(int(gx), inst.cols-2)
	r := min(int(gy), inst.rows-2)
	fx := float32(gx - float64(c))
	fy := float32(gy - float64(r))
	i00 := r*inst.cols + c
	i10, i01, i11 := i00+1, i00+inst.cols, i00+inst.cols+1
	a, b, d, e := inst.vx[i00], inst.vx[i10], inst.vx[i01], inst.vx[i11]
	if a != a || b != b || d != d || e != e {
		return
	}
	w00, w10, w01, w11 := (1-fx)*(1-fy), fx*(1-fy), (1-fx)*fy, fx*fy
	vx = w00*a + w10*b + w01*d + w11*e
	vy = w00*inst.vy[i00] + w10*inst.vy[i10] + w01*inst.vy[i01] + w11*inst.vy[i11]
	speed = w00*inst.speed[i00] + w10*inst.speed[i10] + w01*inst.speed[i01] + w11*inst.speed[i11]
	ok = true
	return
}

// field is what the simulation samples: one grid, or two blended in time.
// wrap, when positive, is the world width of a periodic source, and a
// position off the grid is folded by it before it is given up.
type field struct {
	a, b   *fieldGrid // b may be nil
	weight float32    // toward b
	wrap   float64
}

func (inst *field) isEmpty() bool { return inst.a == nil }

// sample blends the two grids linearly by component, and the scalar mean by
// itself so that colour does not take the dip the components take where the
// two steps disagree. Where only one grid has a value, that one is shown.
func (inst *field) sample(x, y float64) (vx, vy, speed float32, ok bool) {
	if inst.a == nil {
		return
	}
	if inst.wrap > 0 {
		x = foldInto(x, inst.a.x0, inst.a.x1(), inst.wrap)
	}
	vx, vy, speed, ok = inst.a.sample(x, y)
	if inst.b == nil || inst.weight <= 0 {
		return
	}
	bx, by, bs, bok := inst.b.sample(x, y)
	if !bok {
		return
	}
	if !ok {
		return bx, by, bs, true
	}
	w := inst.weight
	return vx + w*(bx-vx), vy + w*(by-vy), speed + w*(bs-speed), true
}

// foldInto moves x by whole periods toward [lo, hi]. It gives x back
// unchanged when no period brings it inside.
func foldInto(x, lo, hi, period float64) float64 {
	if x < lo {
		x += math.Ceil((lo-x)/period) * period
	} else if x > hi {
		x -= math.Ceil((x-hi)/period) * period
	}
	return x
}

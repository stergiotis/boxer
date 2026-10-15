package view

import (
	"math"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// Flattening bounds for a dashed edge's cubic pieces: about one sample per
// flattenStepPx of control-polygon length on screen, within
// [flattenMinSteps, flattenMaxSteps] per piece.
const (
	flattenStepPx   = 4.0
	flattenMinSteps = 8
	flattenMaxSteps = 64
)

// flattenEdge maps an edge's control points to the screen with tf and returns
// the polyline that approximates its route. Four or more points are read as
// Graphviz's spline — a start point, then three controls per cubic piece —
// and each piece is sampled by its on-screen size; fewer points are already a
// polyline.
func flattenEdge(pts []layeredgraph.Point, tf func(layeredgraph.Point) (float32, float32)) (xs, ys []float32) {
	if len(pts) < 4 {
		xs = make([]float32, len(pts))
		ys = make([]float32, len(pts))
		for i, p := range pts {
			xs[i], ys[i] = tf(p)
		}
		return
	}
	x0, y0 := tf(pts[0])
	xs, ys = append(xs, x0), append(ys, y0)
	for i := 0; i+3 < len(pts); i += 3 {
		var cx, cy [4]float64
		for k := range 4 {
			x, y := tf(pts[i+k])
			cx[k], cy[k] = float64(x), float64(y)
		}
		poly := math.Hypot(cx[1]-cx[0], cy[1]-cy[0]) +
			math.Hypot(cx[2]-cx[1], cy[2]-cy[1]) +
			math.Hypot(cx[3]-cx[2], cy[3]-cy[2])
		n := int(math.Ceil(poly / flattenStepPx))
		n = min(max(n, flattenMinSteps), flattenMaxSteps)
		for s := 1; s <= n; s++ {
			t := float64(s) / float64(n)
			u := 1 - t
			b0, b1, b2, b3 := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
			xs = append(xs, float32(b0*cx[0]+b1*cx[1]+b2*cx[2]+b3*cx[3]))
			ys = append(ys, float32(b0*cy[0]+b1*cy[1]+b2*cy[2]+b3*cy[3]))
		}
	}
	return
}

// dashSegments walks the polyline (xs, ys) and returns its dashes as segments:
// dash pixels drawn, gap pixels skipped, repeating from the start. The phase
// carries across the polyline's joints, so a dash that spans a joint comes
// back as two segments meeting there. A non-positive dash or gap returns the
// polyline's pieces undashed.
func dashSegments(xs, ys []float32, dash, gap float32) (x0s, y0s, x1s, y1s []float32) {
	n := min(len(xs), len(ys))
	if n < 2 {
		return
	}
	solid := dash <= 0 || gap <= 0
	period := dash + gap
	var pos float32 // distance into the current dash period
	for i := 0; i+1 < n; i++ {
		ax, ay, bx, by := xs[i], ys[i], xs[i+1], ys[i+1]
		l := float32(math.Hypot(float64(bx-ax), float64(by-ay)))
		if l <= 0 {
			continue
		}
		if solid {
			x0s, y0s, x1s, y1s = append(x0s, ax), append(y0s, ay), append(x1s, bx), append(y1s, by)
			continue
		}
		ux, uy := (bx-ax)/l, (by-ay)/l
		for t := float32(0); t < l; {
			on := pos < dash
			var step float32
			if on {
				step = dash - pos
			} else {
				step = period - pos
			}
			step = min(step, l-t)
			if step <= 1e-6 {
				break
			}
			if on {
				x0s, y0s = append(x0s, ax+ux*t), append(y0s, ay+uy*t)
				x1s, y1s = append(x1s, ax+ux*(t+step)), append(y1s, ay+uy*(t+step))
			}
			t += step
			pos += step
			if pos >= period {
				pos -= period
			}
		}
	}
	return
}

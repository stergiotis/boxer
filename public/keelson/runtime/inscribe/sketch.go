package inscribe

import (
	"hash/fnv"
	"math"
)

// Sketch strokes (ADR-0297 §SD6): every line of the overlay is a cubic
// Bézier nudged off its straight path and drawn twice, the way a pen goes
// over a line. The nudges come from a seed of the mark's identity, not its
// position, so a mark keeps its shape from frame to frame and as its window
// moves.

// pt is a point in viewport points.
type pt struct{ X, Y float32 }

// polyline is one stroke, sampled.
type polyline []pt

// sketchRng is a small deterministic generator: xorshift64*.
type sketchRng struct{ s uint64 }

func newSketchRng(seed uint64) *sketchRng {
	if seed == 0 {
		seed = 0x9e3779b97f4a7c15
	}
	return &sketchRng{s: seed}
}

// next is uniform in [-1, 1).
func (inst *sketchRng) next() float32 {
	inst.s ^= inst.s >> 12
	inst.s ^= inst.s << 25
	inst.s ^= inst.s >> 27
	v := inst.s * 2685821657736338717
	return float32(v>>40)/float32(1<<23) - 1
}

// seedOf is a seed from a mark's identity and a shape's index within it.
func seedOf(task string, id string, k int) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(task + "\x00" + id + "\x00"))
	_, _ = h.Write([]byte{byte(k), byte(k >> 8)})
	return h.Sum64()
}

// roughness is how far, in points, a stroke strays: a little more for a
// longer line, never much.
func roughness(length float32) float32 {
	return min(1.4+length*0.02, 5)
}

// sketchLine is a straight line drawn twice by hand: two Béziers whose ends
// and control points are nudged, sampled to polylines.
func sketchLine(x0, y0, x1, y1 float32, r *sketchRng) (out []polyline) {
	dx, dy := x1-x0, y1-y0
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l < 0.5 {
		return nil
	}
	rough := roughness(l)
	nx, ny := -dy/l, dx/l
	for pass := 0; pass < 2; pass++ {
		j := func(scale float32) float32 { return r.next() * rough * scale }
		a := pt{x0 + j(0.6), y0 + j(0.6)}
		b := pt{x1 + j(0.6), y1 + j(0.6)}
		bend := j(1.4)
		c1 := pt{x0 + dx*0.33 + nx*bend + j(0.4), y0 + dy*0.33 + ny*bend + j(0.4)}
		c2 := pt{x0 + dx*0.67 + nx*bend*0.7 + j(0.4), y0 + dy*0.67 + ny*bend*0.7 + j(0.4)}
		out = append(out, sampleCubic(a, c1, c2, b, samplesFor(l)))
	}
	return
}

// sketchBow is a gently bowed stroke from one point to another, drawn twice:
// an arrow's shaft.
func sketchBow(x0, y0, x1, y1 float32, r *sketchRng) (out []polyline) {
	dx, dy := x1-x0, y1-y0
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l < 0.5 {
		return nil
	}
	nx, ny := -dy/l, dx/l
	side := float32(1)
	if r.next() < 0 {
		side = -1
	}
	bow := min(l*0.08, 24) * side
	rough := roughness(l)
	for pass := 0; pass < 2; pass++ {
		j := func(scale float32) float32 { return r.next() * rough * scale }
		a := pt{x0 + j(0.5), y0 + j(0.5)}
		b := pt{x1 + j(0.3), y1 + j(0.3)}
		c1 := pt{x0 + dx*0.3 + nx*bow + j(0.6), y0 + dy*0.3 + ny*bow + j(0.6)}
		c2 := pt{x0 + dx*0.7 + nx*bow + j(0.6), y0 + dy*0.7 + ny*bow + j(0.6)}
		out = append(out, sampleCubic(a, c1, c2, b, samplesFor(l)))
	}
	return
}

// sketchRect is a rect drawn by hand: each side a sketched line, run a
// little past the corners.
func sketchRect(rc Rect, r *sketchRng) (out []polyline) {
	o := min(4, max(rc.W, rc.H)*0.05)
	x0, y0, x1, y1 := rc.X, rc.Y, rc.MaxX(), rc.MaxY()
	out = append(out, sketchLine(x0-o, y0, x1+o, y0, r)...)
	out = append(out, sketchLine(x1, y0-o, x1, y1+o, r)...)
	out = append(out, sketchLine(x1+o, y1, x0-o, y1, r)...)
	out = append(out, sketchLine(x0, y1+o, x0, y0-o, r)...)
	return
}

// sketchHead is an open arrowhead at the end of a stroke arriving along
// (ux, uy): two short sketched barbs.
func sketchHead(tipX, tipY, ux, uy float32, size float32, r *sketchRng) (out []polyline) {
	const spread = 0.48 // radians, about 28°
	for _, s := range []float64{spread, -spread} {
		cs, sn := float32(math.Cos(s)), float32(math.Sin(s))
		bx := -(ux*cs - uy*sn) * size
		by := -(ux*sn + uy*cs) * size
		out = append(out, sketchLine(tipX, tipY, tipX+bx, tipY+by, r)...)
	}
	return
}

func samplesFor(l float32) int { return int(min(max(l/10, 6), 48)) }

func sampleCubic(a, b, c, d pt, n int) (out polyline) {
	out = make(polyline, 0, n+1)
	for i := 0; i <= n; i++ {
		t := float32(i) / float32(n)
		u := 1 - t
		w0, w1, w2, w3 := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
		out = append(out, pt{w0*a.X + w1*b.X + w2*c.X + w3*d.X, w0*a.Y + w1*b.Y + w2*c.Y + w3*d.Y})
	}
	return
}

// dashes cuts a polyline into dashes of dash points with gaps of gap
// between them, as segments.
func dashes(p polyline, dash, gap float32) (x0s, y0s, x1s, y1s []float32) {
	on := true
	left := dash
	for i := 0; i+1 < len(p); i++ {
		a, b := p[i], p[i+1]
		seg := float32(math.Hypot(float64(b.X-a.X), float64(b.Y-a.Y)))
		pos := float32(0)
		for seg-pos > 1e-3 {
			step := min(left, seg-pos)
			t0, t1 := pos/seg, (pos+step)/seg
			if on {
				x0s = append(x0s, a.X+(b.X-a.X)*t0)
				y0s = append(y0s, a.Y+(b.Y-a.Y)*t0)
				x1s = append(x1s, a.X+(b.X-a.X)*t1)
				y1s = append(y1s, a.Y+(b.Y-a.Y)*t1)
			}
			pos += step
			left -= step
			if left <= 1e-3 {
				on = !on
				left = dash
				if !on {
					left = gap
				}
			}
		}
	}
	return
}

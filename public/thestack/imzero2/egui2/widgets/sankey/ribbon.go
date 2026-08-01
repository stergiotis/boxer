package sankey

import "sort"

// DefaultSamples is how many points each ribbon edge is sampled at. It is the
// renderer's default and the hit test's default, and they must agree — the
// point of sharing one sampler is that the region a pointer tests against is
// exactly the region that was drawn (ADR-0159 SD3).
const DefaultSamples = 24

// Ribbon is a sampled link: the shared vertical-extrusion geometry behind
// both fill routes and the hit test. Xs ascends from the source face to the
// target face; Top and Bot are the upper and lower edges at those x, in the
// same unit-box coordinates as the layout.
//
// The two edges never cross, because Bot is Top translated down by a
// constant Thickness — which is what lets the outline go to an ear-clipping
// tessellator as a single simple ring.
type Ribbon struct {
	Xs        []float64
	Top       []float64
	Bot       []float64
	Thickness float64
}

// Sample fills dst with samples+1 points along l and returns it. dst may be
// nil, and its buffers are reused when they are already large enough, so a
// renderer can keep one Ribbon per frame instead of allocating per link.
//
// The curve is a cubic Bézier with both control points on the x-midpoint —
// the shape that leaves a ribbon horizontal where it meets a node face. Its x
// control values are monotone, so Xs ascends and Contains can binary-search
// it.
func (l LinkLayout) Sample(samples int, dst *Ribbon) *Ribbon {
	if samples < 1 {
		samples = DefaultSamples
	}
	if dst == nil {
		dst = &Ribbon{}
	}
	n := samples + 1
	dst.Xs = resize(dst.Xs, n)
	dst.Top = resize(dst.Top, n)
	dst.Bot = resize(dst.Bot, n)
	dst.Thickness = l.SY1 - l.SY0

	xm := (l.SX + l.TX) / 2
	for i := range n {
		t := float64(i) / float64(samples)
		u := 1 - t
		// Cubic Bernstein weights.
		b0 := u * u * u
		b1 := 3 * u * u * t
		b2 := 3 * u * t * t
		b3 := t * t * t
		dst.Xs[i] = b0*l.SX + (b1+b2)*xm + b3*l.TX
		top := b0*l.SY1 + b1*l.SY1 + b2*l.TY1 + b3*l.TY1
		dst.Top[i] = top
		dst.Bot[i] = top - dst.Thickness
	}
	// The endpoints must land exactly on the faces: a float32-visible drift
	// here would show as a hairline gap against the node bar.
	dst.Xs[0], dst.Top[0], dst.Bot[0] = l.SX, l.SY1, l.SY0
	dst.Xs[n-1], dst.Top[n-1], dst.Bot[n-1] = l.TX, l.TY1, l.TY0
	return dst
}

// Contains reports whether (x, y) lies inside the sampled ribbon. It is the
// hit test the renderer exposes, and it tests the drawn polygon rather than a
// bounding box, so a pointer between two crossing ribbons picks the right one.
func (r *Ribbon) Contains(x float64, y float64) bool {
	n := len(r.Xs)
	if n < 2 || x < r.Xs[0] || x > r.Xs[n-1] {
		return false
	}
	// First index with Xs[i] >= x; the segment is [i-1, i].
	i := sort.SearchFloat64s(r.Xs, x)
	if i == 0 {
		return y <= r.Top[0] && y >= r.Bot[0]
	}
	if i >= n {
		i = n - 1
	}
	x0, x1 := r.Xs[i-1], r.Xs[i]
	f := 0.0
	if x1 > x0 {
		f = (x - x0) / (x1 - x0)
	}
	top := r.Top[i-1] + f*(r.Top[i]-r.Top[i-1])
	bot := r.Bot[i-1] + f*(r.Bot[i]-r.Bot[i-1])
	return y <= top && y >= bot
}

// NodeAt returns the index of the node bar containing (x, y), or -1. Stages
// do not overlap in x and nodes within a stage do not overlap in y, so at
// most one bar can match.
func (lay *Layout) NodeAt(x float64, y float64) int {
	for i := range lay.Nodes {
		n := &lay.Nodes[i]
		if x >= n.X0 && x <= n.X1 && y >= n.Y0 && y <= n.Y1 {
			return i
		}
	}
	return -1
}

// LinkAt returns the index of the topmost ribbon containing (x, y), or -1.
// Ribbons are drawn in slice order, so the last match is the one on top and
// is what a pointer should pick. scratch may be nil; pass the renderer's to
// avoid allocating.
func (lay *Layout) LinkAt(x float64, y float64, samples int, scratch *Ribbon) int {
	if scratch == nil {
		scratch = &Ribbon{}
	}
	found := -1
	for i := range lay.Links {
		lay.Links[i].Sample(samples, scratch)
		if scratch.Contains(x, y) {
			found = i
		}
	}
	return found
}

func resize(s []float64, n int) []float64 {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]float64, n)
}

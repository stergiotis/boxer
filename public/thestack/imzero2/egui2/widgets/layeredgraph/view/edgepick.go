package view

import (
	"math"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// edgePickTolPx is the least distance from an edge's stroke, in pixels, that
// still counts as on it, so a hairline stays hoverable.
const edgePickTolPx = 4

// edgeHitBox is an edge's pick extent on screen: the bounding box of its
// control points and head tip, which holds the whole drawn curve, widened by
// the pick tolerance.
type edgeHitBox struct {
	minX, minY, maxX, maxY float32
	tol                    float32
}

// newEdgeHitBox bounds e under tf for a stroke of strokeW pixels.
func newEdgeHitBox(e *layeredgraph.EdgeLayout, tf func(layeredgraph.Point) (float32, float32), strokeW float32) (b edgeHitBox, ok bool) {
	if len(e.Points) == 0 {
		return
	}
	b.minX, b.minY = float32(math.MaxFloat32), float32(math.MaxFloat32)
	b.maxX, b.maxY = -float32(math.MaxFloat32), -float32(math.MaxFloat32)
	grow := func(p layeredgraph.Point) {
		x, y := tf(p)
		b.minX, b.minY = min(b.minX, x), min(b.minY, y)
		b.maxX, b.maxY = max(b.maxX, x), max(b.maxY, y)
	}
	for _, p := range e.Points {
		grow(p)
	}
	if e.ArrowHead != nil {
		grow(*e.ArrowHead)
	}
	b.tol = max(strokeW, edgePickTolPx)
	b.minX, b.minY, b.maxX, b.maxY = b.minX-b.tol, b.minY-b.tol, b.maxX+b.tol, b.maxY+b.tol
	ok = true
	return
}

func (b edgeHitBox) mayContain(x, y float32) bool {
	return x >= b.minX && x <= b.maxX && y >= b.minY && y <= b.maxY
}

// edgeDistance is the distance from (x, y) to e as drawn under tf: its route,
// flattened as it is for dashing, and the shaft from the route's end to the
// head tip.
func edgeDistance(e *layeredgraph.EdgeLayout, tf func(layeredgraph.Point) (float32, float32), x, y float32) float32 {
	xs, ys := flattenEdge(e.Points, tf)
	if e.ArrowHead != nil && len(xs) > 0 {
		hx, hy := tf(*e.ArrowHead)
		xs, ys = append(xs, hx), append(ys, hy)
	}
	best := float32(math.MaxFloat32)
	switch len(xs) {
	case 0:
	case 1:
		best = float32(math.Hypot(float64(x-xs[0]), float64(y-ys[0])))
	default:
		for i := 0; i+1 < len(xs); i++ {
			best = min(best, distSegment(xs[i], ys[i], xs[i+1], ys[i+1], x, y))
		}
	}
	return best
}

// pickEdge returns the index of the edge nearest (x, y) within its pick
// tolerance, or -1. boxes[i] bounds edges[i]; an edge whose box misses the
// point is rejected before its route is flattened.
func pickEdge(edges []layeredgraph.EdgeLayout, boxes []edgeHitBox, ok []bool,
	tf func(layeredgraph.Point) (float32, float32), x, y float32) int {
	best, bestD := -1, float32(math.MaxFloat32)
	for i := range edges {
		if !ok[i] || !boxes[i].mayContain(x, y) {
			continue
		}
		d := edgeDistance(&edges[i], tf, x, y)
		if d <= boxes[i].tol && d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// pickAt resolves what the pointer at (x, y) is on: the topmost node, else
// the nearest edge, else nothing (-1 for each). A node covers the ends of the
// edges meeting it, so it keeps the pointer there; edges are only tested, and
// their routes only flattened, when no node is hit. widths are the edges'
// stroke widths in pixels.
func pickAt(boxes []nodeBox, edges []layeredgraph.EdgeLayout, widths []float32,
	tf func(layeredgraph.Point) (float32, float32), x, y float32) (node, edge int) {
	node, edge = pickNode(boxes, x, y), -1
	if node >= 0 || len(edges) == 0 {
		return
	}
	hitBoxes := make([]edgeHitBox, len(edges))
	ok := make([]bool, len(edges))
	for i := range edges {
		hitBoxes[i], ok[i] = newEdgeHitBox(&edges[i], tf, widths[i])
	}
	edge = pickEdge(edges, hitBoxes, ok, tf, x, y)
	return
}

// distSegment is the distance from (px, py) to the segment a–b.
func distSegment(ax, ay, bx, by, px, py float32) float32 {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	t := float32(0)
	if l2 > 0 {
		t = ((px-ax)*dx + (py-ay)*dy) / l2
		t = min(max(t, 0), 1)
	}
	cx, cy := ax+t*dx, ay+t*dy
	return float32(math.Hypot(float64(px-cx), float64(py-cy)))
}

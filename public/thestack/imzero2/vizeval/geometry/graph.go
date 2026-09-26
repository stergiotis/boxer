package geometry

import "math"

// Graph metric names (ADR-0257 §SD6). They are measured only for a graph
// sink's drawing, where every stroked line or curve is an edge and every
// filled circle a node; in a chart the grid would count as edges.
const (
	// MetricGraphNodes counts filled circles in the area.
	MetricGraphNodes = "graph.nodes"
	// MetricGraphEdges counts stroked lines and curves in the area.
	MetricGraphEdges = "graph.edges"
	// MetricGraphEdgeCrossings counts pairs of edges that cross, not
	// counting pairs that meet at a shared end.
	MetricGraphEdgeCrossings = "graph.edge_crossings"
	// MetricGraphLabelNodeOverlaps counts labels drawn over a node other than
	// the one nearest them — a label hiding a neighbour.
	MetricGraphLabelNodeOverlaps = "graph.label_node_overlaps"
)

// bezierSamples is how finely a curved edge is followed.
const bezierSamples = 12

// endTolerance is how near two ends must be to count as one node.
const endTolerance = 1.5

// MeasureGraph adds the graph metrics of the part of d inside area to m.
func MeasureGraph(d Drawing, area Rect, m map[string]float64) {
	var edges [][]Point
	var nodes []Rect
	for _, mk := range d.Marks {
		if mk.Box.Intersect(mk.Clip).Intersect(area).Empty() {
			continue
		}
		switch {
		case mk.Kind == MarkKindCircle && mk.Fill.A > 0:
			nodes = append(nodes, mk.Box)
		case mk.Kind == MarkKindLine && mk.Stroke.A > 0 && len(mk.Poly) >= 2:
			edges = append(edges, mk.Poly)
		case mk.Kind == MarkKindPath && mk.Cubic && mk.Stroke.A > 0 && mk.Fill.A == 0:
			edges = append(edges, sampleCubic(mk.Poly))
		}
	}
	m[MetricGraphNodes] = float64(len(nodes))
	m[MetricGraphEdges] = float64(len(edges))
	var crossings int
	for i := range edges {
		for j := i + 1; j < len(edges); j++ {
			if shareEnd(edges[i], edges[j]) {
				continue
			}
			if polylinesCross(edges[i], edges[j]) {
				crossings++
			}
		}
	}
	m[MetricGraphEdgeCrossings] = float64(crossings)
	var overlaps int
	for _, r := range d.Runs {
		vb := r.Box.Intersect(r.Clip).Intersect(area)
		if vb.Empty() {
			continue
		}
		cx, cy := vb.Center()
		nearest, best := -1, math.Inf(1)
		for k, n := range nodes {
			nx, ny := n.Center()
			if dd := math.Hypot(nx-cx, ny-cy); dd < best {
				nearest, best = k, dd
			}
		}
		for k, n := range nodes {
			x := vb.Intersect(n)
			if k != nearest && x.Area() > overlapMinArea {
				overlaps++
			}
		}
	}
	m[MetricGraphLabelNodeOverlaps] = float64(overlaps)
}

func sampleCubic(p []Point) (out []Point) {
	out = make([]Point, 0, bezierSamples+1)
	for i := 0; i <= bezierSamples; i++ {
		t := float64(i) / bezierSamples
		u := 1 - t
		a, b, c, dd := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
		out = append(out, Point{
			X: a*p[0].X + b*p[1].X + c*p[2].X + dd*p[3].X,
			Y: a*p[0].Y + b*p[1].Y + c*p[2].Y + dd*p[3].Y,
		})
	}
	return out
}

func near(a, b Point) bool { return math.Hypot(a.X-b.X, a.Y-b.Y) <= endTolerance }

// shareEnd reports whether two edges meet at a node: an end of one at an end
// of the other. Edges drawn to a node's rim rather than its centre stop short
// of each other, so this is a tolerance on the ends, not an exact match.
func shareEnd(a, b []Point) bool {
	ea := [2]Point{a[0], a[len(a)-1]}
	eb := [2]Point{b[0], b[len(b)-1]}
	for _, p := range ea {
		for _, q := range eb {
			if near(p, q) {
				return true
			}
		}
	}
	return false
}

func polylinesCross(a, b []Point) bool {
	for i := 0; i+1 < len(a); i++ {
		for j := 0; j+1 < len(b); j++ {
			if segmentsCross(a[i], a[i+1], b[j], b[j+1]) {
				return true
			}
		}
	}
	return false
}

// segmentsCross is a proper crossing: each segment's ends strictly on
// opposite sides of the other's line. Touching and collinear overlap do not
// count — an edge running along another is a different fault.
func segmentsCross(p1, p2, q1, q2 Point) bool {
	o := func(a, b, c Point) float64 { return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X) }
	d1, d2 := o(q1, q2, p1), o(q1, q2, p2)
	d3, d4 := o(p1, p2, q1), o(p1, p2, q2)
	return ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0))
}

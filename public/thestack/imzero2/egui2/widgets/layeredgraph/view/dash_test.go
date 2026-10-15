package view

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

func segLengths(x0s, y0s, x1s, y1s []float32) (sum float64, each []float64) {
	for i := range x0s {
		l := math.Hypot(float64(x1s[i]-x0s[i]), float64(y1s[i]-y0s[i]))
		each = append(each, l)
		sum += l
	}
	return
}

// 100 px at 5 on / 3 off: dashes start every 8 px, twelve whole ones and a
// thirteenth cut to the 4 px that remain.
func TestDashSegmentsStraightLine(t *testing.T) {
	x0s, y0s, x1s, y1s := dashSegments([]float32{0, 100}, []float32{0, 0}, 5, 3)
	if len(x0s) != 13 {
		t.Fatalf("got %d dashes, want 13", len(x0s))
	}
	sum, each := segLengths(x0s, y0s, x1s, y1s)
	if math.Abs(sum-64) > 1e-3 {
		t.Errorf("dashed length %v, want 64", sum)
	}
	if math.Abs(each[12]-4) > 1e-3 {
		t.Errorf("last dash %v, want 4", each[12])
	}
	for i := range x0s {
		if x0s[i] != float32(8*i) {
			t.Errorf("dash %d starts at %v, want %v", i, x0s[i], 8*i)
		}
	}
}

// The phase carries across a joint: the same 100 px split at 50 draws the
// same dashes, the one spanning the joint (48..53) as two segments meeting at
// it.
func TestDashSegmentsCarryThePhaseAcrossJoints(t *testing.T) {
	x0s, y0s, x1s, y1s := dashSegments([]float32{0, 50, 100}, []float32{0, 0, 0}, 5, 3)
	if len(x0s) != 14 {
		t.Fatalf("got %d segments, want 14", len(x0s))
	}
	sum, _ := segLengths(x0s, y0s, x1s, y1s)
	if math.Abs(sum-64) > 1e-3 {
		t.Errorf("dashed length %v, want 64 as for the unsplit line", sum)
	}
	if x0s[6] != 48 || x1s[6] != 50 || x0s[7] != 50 || x1s[7] != 53 {
		t.Errorf("the dash over the joint is %v..%v then %v..%v, want 48..50 then 50..53",
			x0s[6], x1s[6], x0s[7], x1s[7])
	}
}

// A dash that turns a corner follows it rather than cutting across.
func TestDashSegmentsFollowACorner(t *testing.T) {
	x0s, y0s, x1s, y1s := dashSegments([]float32{0, 2, 2}, []float32{0, 0, 10}, 5, 3)
	if len(x0s) < 2 {
		t.Fatalf("got %d segments, want the first dash split at the corner", len(x0s))
	}
	if x1s[0] != 2 || y1s[0] != 0 || x0s[1] != 2 || y0s[1] != 0 || y1s[1] != 3 {
		t.Errorf("first dash %v,%v→%v,%v then %v,%v→%v,%v; want 0,0→2,0 then 2,0→2,3",
			x0s[0], y0s[0], x1s[0], y1s[0], x0s[1], y0s[1], x1s[1], y1s[1])
	}
}

// A pattern that cannot dash draws the polyline as it is, and a polyline
// with fewer than two points draws nothing.
func TestDashSegmentsDegenerate(t *testing.T) {
	for _, p := range [][2]float32{{0, 3}, {5, 0}, {-1, 3}} {
		x0s, _, _, _ := dashSegments([]float32{0, 10, 20}, []float32{0, 0, 0}, p[0], p[1])
		if len(x0s) != 2 {
			t.Errorf("dash %v gap %v gave %d segments, want the 2 undashed pieces", p[0], p[1], len(x0s))
		}
	}
	if x0s, _, _, _ := dashSegments([]float32{1}, []float32{1}, 5, 3); len(x0s) != 0 {
		t.Errorf("a single point gave %d segments", len(x0s))
	}
	if x0s, _, _, _ := dashSegments([]float32{1, 1}, []float32{1, 1}, 5, 3); len(x0s) != 0 {
		t.Errorf("a zero-length piece gave %d segments", len(x0s))
	}
}

// A spline flattens to a polyline that starts and ends on its end points,
// lies on a straight spline's line, and is sampled within the step bounds.
func TestFlattenEdgeStraightSpline(t *testing.T) {
	identity := func(p layeredgraph.Point) (float32, float32) { return float32(p.X), float32(p.Y) }
	pts := []layeredgraph.Point{{X: 0, Y: 0}, {X: 30, Y: 0}, {X: 60, Y: 0}, {X: 90, Y: 0},
		{X: 120, Y: 0}, {X: 150, Y: 0}, {X: 400, Y: 0}}
	xs, ys := flattenEdge(pts, identity)
	if xs[0] != 0 || xs[len(xs)-1] != 400 {
		t.Errorf("polyline runs %v..%v, want 0..400", xs[0], xs[len(xs)-1])
	}
	for i, y := range ys {
		if y != 0 {
			t.Fatalf("point %d left the line: y=%v", i, y)
		}
	}
	// Piece one: 90 px of control polygon → 23 steps; piece two: 310 px →
	// capped at 64.
	if want := 1 + 23 + flattenMaxSteps; len(xs) != want {
		t.Errorf("got %d points, want %d", len(xs), want)
	}
	for i := 1; i < len(xs); i++ {
		if xs[i] < xs[i-1] {
			t.Fatalf("polyline doubles back at %d", i)
		}
	}
}

// Fewer than four points are a polyline already and pass through mapped.
func TestFlattenEdgeShortIsPolyline(t *testing.T) {
	double := func(p layeredgraph.Point) (float32, float32) { return float32(2 * p.X), float32(2 * p.Y) }
	xs, ys := flattenEdge([]layeredgraph.Point{{X: 1, Y: 2}, {X: 3, Y: 4}}, double)
	if len(xs) != 2 || xs[0] != 2 || ys[0] != 4 || xs[1] != 6 || ys[1] != 8 {
		t.Errorf("got %v %v", xs, ys)
	}
}

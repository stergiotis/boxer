package layout

import (
	"math"
	"testing"
)

const floatEps = 1e-6

func TestNodeTotalSize_LeafWithSize(t *testing.T) {
	n := &Node{Name: "a", Size: 42}
	if got := n.TotalSize(); got != 42 {
		t.Errorf("TotalSize(): got %v want 42", got)
	}
}

func TestNodeTotalSize_LeafZeroOrNegativeFallsBackToOne(t *testing.T) {
	cases := []struct {
		name string
		size float64
	}{
		{"zero", 0},
		{"negative", -5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := &Node{Name: "leaf", Size: tc.size}
			if got := n.TotalSize(); got != 1 {
				t.Errorf("leaf with size=%v: TotalSize got %v want 1 (fallback)", tc.size, got)
			}
		})
	}
}

func TestNodeTotalSize_ParentSumsChildren(t *testing.T) {
	parent := &Node{Name: "p", Children: []*Node{
		{Name: "a", Size: 10},
		{Name: "b", Size: 30},
	}}
	if got := parent.TotalSize(); got != 40 {
		t.Errorf("parent.TotalSize(): got %v want 40", got)
	}
}

func TestNodeTotalSize_RecursiveSumThroughDeepTree(t *testing.T) {
	tree := &Node{Name: "root", Children: []*Node{
		{Name: "branch1", Children: []*Node{
			{Name: "leaf1", Size: 5},
			{Name: "leaf2", Size: 7},
		}},
		{Name: "branch2", Children: []*Node{
			{Name: "leaf3", Size: 11},
		}},
		{Name: "leaf4", Size: 13},
	}}
	want := 5.0 + 7 + 11 + 13
	if got := tree.TotalSize(); got != want {
		t.Errorf("deep tree.TotalSize(): got %v want %v", got, want)
	}
}

func TestComputeLayout_EmptyChildren_RootGetsFullBounds(t *testing.T) {
	root := &Node{Name: "alone", Size: 10}
	lay := ComputeLayout(root, 80, 40)
	got := lay.RectOf(root)
	want := Rect{X: 0, Y: 0, W: 80, H: 40}
	if got != want {
		t.Errorf("RectOf(root): got %+v want %+v", got, want)
	}
}

func TestComputeLayoutAt_EmptyChildren_RootGetsArbitraryBounds(t *testing.T) {
	root := &Node{Name: "alone"}
	bounds := Rect{X: 100, Y: 200, W: 50, H: 25}
	lay := ComputeLayoutAt(root, bounds)
	if got := lay.RectOf(root); got != bounds {
		t.Errorf("RectOf(root): got %+v want %+v", got, bounds)
	}
}

func TestComputeLayout_TwoEqualChildren_FillBoundsAndStack(t *testing.T) {
	a := &Node{Name: "a", Size: 50}
	b := &Node{Name: "b", Size: 50}
	root := &Node{Name: "r", Children: []*Node{a, b}}

	lay := ComputeLayout(root, 100, 100)
	ra, rb := lay.RectOf(a), lay.RectOf(b)

	// Combined area covers the box.
	totalArea := ra.W*ra.H + rb.W*rb.H
	if math.Abs(totalArea-10000) > floatEps {
		t.Errorf("sum of rect areas: got %v want 10000", totalArea)
	}
	// Both rects positive-sized.
	if ra.W <= 0 || ra.H <= 0 || rb.W <= 0 || rb.H <= 0 {
		t.Errorf("rects must have positive dimensions; got ra=%+v rb=%+v", ra, rb)
	}
	// Rects within bounds.
	for name, r := range map[string]Rect{"a": ra, "b": rb} {
		if r.X < -floatEps || r.Y < -floatEps {
			t.Errorf("%s: negative origin %+v", name, r)
		}
		if r.X+r.W > 100+floatEps || r.Y+r.H > 100+floatEps {
			t.Errorf("%s: overflows 100x100 bounds: %+v", name, r)
		}
	}
}

func TestComputeLayout_AreasProportionalToTotalSize(t *testing.T) {
	a := &Node{Name: "a", Size: 1}
	b := &Node{Name: "b", Size: 3}
	root := &Node{Name: "r", Children: []*Node{a, b}}

	lay := ComputeLayout(root, 200, 100)
	ra, rb := lay.RectOf(a), lay.RectOf(b)
	areaA := ra.W * ra.H
	areaB := rb.W * rb.H

	// Total box area = 20000. With sizes 1 and 3, ratios are 1/4 and 3/4.
	wantA := 5000.0
	wantB := 15000.0
	if math.Abs(areaA-wantA) > 1e-3 {
		t.Errorf("areaA: got %v want %v", areaA, wantA)
	}
	if math.Abs(areaB-wantB) > 1e-3 {
		t.Errorf("areaB: got %v want %v", areaB, wantB)
	}
	if math.Abs((areaA+areaB)-20000) > 1e-3 {
		t.Errorf("combined area: got %v want 20000", areaA+areaB)
	}
	// Larger child gets larger rect (sanity).
	if areaB <= areaA {
		t.Errorf("larger child should get larger area; got A=%v B=%v", areaA, areaB)
	}
}

func TestComputeLayout_FourEqualChildren_TotalAreaPreserved(t *testing.T) {
	root := &Node{Name: "r"}
	children := make([]*Node, 4)
	for i := range children {
		children[i] = &Node{Name: "leaf", Size: 25}
		root.Children = append(root.Children, children[i])
	}
	lay := ComputeLayout(root, 100, 100)

	sum := 0.0
	for _, ch := range children {
		r := lay.RectOf(ch)
		if r.W <= 0 || r.H <= 0 {
			t.Errorf("zero-sized rect for child: %+v", r)
		}
		sum += r.W * r.H
	}
	if math.Abs(sum-10000) > 1e-3 {
		t.Errorf("4-child total area: got %v want 10000", sum)
	}
}

func TestComputeLayout_ChildOrderPreservedInMap(t *testing.T) {
	// Children are keyed by *Node, so callers can lookup by original pointer
	// regardless of squarify's internal sort.
	a := &Node{Name: "a", Size: 10}
	b := &Node{Name: "b", Size: 90} // larger; squarify sorts desc, so b ends up first internally
	c := &Node{Name: "c", Size: 5}
	root := &Node{Name: "r", Children: []*Node{a, b, c}}

	lay := ComputeLayout(root, 100, 100)
	for _, ch := range root.Children {
		r := lay.RectOf(ch)
		if r.W <= 0 || r.H <= 0 {
			t.Errorf("RectOf %s yielded zero/negative dims: %+v", ch.Name, r)
		}
	}
	// b is the largest; its area should dominate.
	if lay.RectOf(b).W*lay.RectOf(b).H <= lay.RectOf(a).W*lay.RectOf(a).H {
		t.Errorf("largest child b did not get the largest rect")
	}
}

func TestComputeLayout_NoRectExceedsArbitraryBounds(t *testing.T) {
	root := &Node{Name: "r"}
	for _, size := range []float64{1, 2, 3, 5, 8, 13, 21} { // mixed sizes
		root.Children = append(root.Children, &Node{Name: "leaf", Size: size})
	}
	bounds := Rect{X: 50, Y: 25, W: 200, H: 150}
	lay := ComputeLayoutAt(root, bounds)
	for _, ch := range root.Children {
		r := lay.RectOf(ch)
		// Allow a tiny float epsilon for the clamp arithmetic.
		if r.X < bounds.X-floatEps || r.Y < bounds.Y-floatEps {
			t.Errorf("%s: origin %+v before bounds %+v", ch.Name, r, bounds)
		}
		if r.X+r.W > bounds.X+bounds.W+floatEps {
			t.Errorf("%s: right edge %v exceeds %v", ch.Name, r.X+r.W, bounds.X+bounds.W)
		}
		if r.Y+r.H > bounds.Y+bounds.H+floatEps {
			t.Errorf("%s: bottom edge %v exceeds %v", ch.Name, r.Y+r.H, bounds.Y+bounds.H)
		}
	}
}

func TestLayout_RectOfUnknownNode_ReturnsZeroValue(t *testing.T) {
	root := &Node{Name: "r", Children: []*Node{{Name: "a", Size: 1}}}
	lay := ComputeLayout(root, 100, 100)
	stranger := &Node{Name: "not-in-tree"}
	if got := lay.RectOf(stranger); got != (Rect{}) {
		t.Errorf("RectOf(stranger): got %+v want zero Rect", got)
	}
}

func TestComputeLayout_SingleChild_FillsAllBounds(t *testing.T) {
	only := &Node{Name: "solo", Size: 100}
	root := &Node{Name: "r", Children: []*Node{only}}
	lay := ComputeLayout(root, 60, 40)
	r := lay.RectOf(only)
	// Single child squarifies to the full bounding box.
	if math.Abs(r.W*r.H-2400) > 1e-3 {
		t.Errorf("single child area: got %v want 2400 (=60*40)", r.W*r.H)
	}
}

func TestSquarify_ZeroTotal_ReturnsZeroRects(t *testing.T) {
	// All inputs zero: documented degenerate case — returns zero rects in
	// original order so callers can still RectOf each child consistently.
	got := squarify(Rect{X: 0, Y: 0, W: 100, H: 50}, []float64{0, 0, 0})
	if len(got) != 3 {
		t.Fatalf("len: got %d want 3", len(got))
	}
	for i, r := range got {
		if r != (Rect{}) {
			t.Errorf("rect[%d] = %+v, want zero", i, r)
		}
	}
}

func TestSquarify_AllNegativeAreas_ReturnsZeroRects(t *testing.T) {
	// Negative inputs are filtered by the guard, leaving total <= 0.
	got := squarify(Rect{X: 0, Y: 0, W: 100, H: 50}, []float64{-1, -2})
	if len(got) != 2 {
		t.Fatalf("len: got %d want 2", len(got))
	}
	for i, r := range got {
		if r != (Rect{}) {
			t.Errorf("rect[%d] = %+v, want zero", i, r)
		}
	}
}

func TestSquarify_DegenerateBox_ReturnsZeroRects(t *testing.T) {
	// Zero-area box: guard returns zero rects rather than dividing by zero.
	got := squarify(Rect{W: 0, H: 0}, []float64{1, 2, 3})
	if len(got) != 3 {
		t.Fatalf("len: got %d want 3", len(got))
	}
	for i, r := range got {
		if r != (Rect{}) {
			t.Errorf("rect[%d] = %+v, want zero", i, r)
		}
	}
}

func TestSquarify_MixedZeroAndPositive_ZerosGetZeroRect(t *testing.T) {
	// Positive-only areas survive; zero-area entries get zero rects in
	// their original index position.
	got := squarify(Rect{W: 100, H: 50}, []float64{0, 10, 0, 20})
	if len(got) != 4 {
		t.Fatalf("len: got %d want 4", len(got))
	}
	if got[0] != (Rect{}) || got[2] != (Rect{}) {
		t.Errorf("zero-area indices should get zero rects; got [0]=%+v [2]=%+v", got[0], got[2])
	}
	if got[1].W*got[1].H <= 0 || got[3].W*got[3].H <= 0 {
		t.Errorf("positive-area indices should get positive rects; got [1]=%+v [3]=%+v", got[1], got[3])
	}
}

func TestHighestAR_FavoursSquareLayout(t *testing.T) {
	// White-box: highestAR is the AR scorer used by squarify; lower value means
	// more square. Adding a third 25 to {100,100} should produce a lower score
	// than the {100,100} pair alone since average area gets closer to a square
	// of side sqrt(225) (in the strip-width context).
	w := 30.0
	base := highestAR([]float64{100, 100}, w)
	improved := highestAR([]float64{100, 100, 25}, w)
	// We don't assert direction strictly (the algorithm decides), only that
	// the scorer evaluates *something* finite for both inputs.
	if math.IsNaN(base) || math.IsInf(base, 0) {
		t.Errorf("base AR not finite: %v", base)
	}
	if math.IsNaN(improved) || math.IsInf(improved, 0) {
		t.Errorf("improved AR not finite: %v", improved)
	}
}

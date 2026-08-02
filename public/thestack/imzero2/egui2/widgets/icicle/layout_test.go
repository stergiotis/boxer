package icicle

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// eps is the slack every geometric assertion below allows. The layout sums
// float64 values, so a few ULP of drift per summed term is expected; anything
// larger is a real defect.
const eps = 1e-9

// testProfile is a small call tree with self time at every level, which is
// what distinguishes this layout from a treemap's:
//
//	main(1)
//	├── parse(2) ── lex(4), ast(3)
//	└── eval(1)  ── walk(5), emit(2)
//
// Totals: lex 4, ast 3, parse 9; walk 5, emit 2, eval 8; main 18.
func testProfile() Tree {
	return Tree{
		Labels:  []string{"main", "parse", "lex", "ast", "eval", "walk", "emit"},
		Parents: []int32{-1, 0, 1, 1, 0, 4, 4},
		Self:    []float64{1, 2, 4, 3, 1, 5, 2},
	}
}

func mustCompute(t *testing.T, tr Tree, o Options) *Layout {
	t.Helper()
	lay, err := Compute(tr, o)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	return lay
}

// byLabel finds a laid-out node by its label. Every label in the fixtures is
// unique, so this is unambiguous.
func byLabel(t *testing.T, lay *Layout, label string) *Node {
	t.Helper()
	for i := range lay.Nodes {
		if lay.Nodes[i].Label == label {
			return &lay.Nodes[i]
		}
	}
	t.Fatalf("no node labelled %q in the layout", label)
	return nil
}

func TestComputeHandPlacement(t *testing.T) {
	lay := mustCompute(t, testProfile(), Options{})
	want := map[string][2]float64{
		"main":  {0, 18},
		"parse": {0, 9},
		"lex":   {0, 4},
		"ast":   {4, 7},
		"eval":  {9, 17},
		"walk":  {9, 14},
		"emit":  {14, 16},
	}
	for label, span := range want {
		n := byLabel(t, lay, label)
		if math.Abs(n.X0-span[0]) > eps || math.Abs(n.X1-span[1]) > eps {
			t.Errorf("%s x = [%v,%v], want [%v,%v]", label, n.X0, n.X1, span[0], span[1])
		}
	}
	if got := lay.Report.Total; math.Abs(got-18) > eps {
		t.Errorf("total = %v, want 18", got)
	}
	if lay.Report.Rows != 3 || lay.Report.MaxDepth != 2 {
		t.Errorf("rows = %d / maxDepth = %d, want 3 / 2", lay.Report.Rows, lay.Report.MaxDepth)
	}
}

// The load-bearing invariant of the form: a parent's width is its own value
// plus its children's, and the part no child covers is exactly its self value.
func TestSelfIsTheUncoveredRemainder(t *testing.T) {
	lay := mustCompute(t, testProfile(), Options{})
	covered := make([]float64, len(lay.Nodes))
	for i := range lay.Nodes {
		if p := lay.Nodes[i].Parent; p >= 0 {
			covered[p] += lay.Nodes[i].Total
		}
	}
	for i := range lay.Nodes {
		n := &lay.Nodes[i]
		if got := n.X1 - n.X0; math.Abs(got-n.Total) > eps {
			t.Errorf("%s width %v != total %v", n.Label, got, n.Total)
		}
		if got := n.Total - covered[i]; math.Abs(got-n.Self) > eps {
			t.Errorf("%s uncovered %v != self %v", n.Label, got, n.Self)
		}
	}
}

// Siblings abut without overlapping and never leave their parent's span.
func TestSiblingsTileInsideTheParent(t *testing.T) {
	lay := mustCompute(t, testProfile(), Options{})
	kids := make(map[int32][]int32)
	for i := range lay.Nodes {
		if p := lay.Nodes[i].Parent; p >= 0 {
			kids[p] = append(kids[p], int32(i))
		}
	}
	for parent, group := range kids {
		pn := &lay.Nodes[parent]
		prev := pn.X0
		for _, k := range group {
			n := &lay.Nodes[k]
			if math.Abs(n.X0-prev) > eps {
				t.Errorf("%s starts at %v, want %v (a gap or an overlap with its left sibling)", n.Label, n.X0, prev)
			}
			if n.X0 < pn.X0-eps || n.X1 > pn.X1+eps {
				t.Errorf("%s [%v,%v] escapes parent %s [%v,%v]", n.Label, n.X0, n.X1, pn.Label, pn.X0, pn.X1)
			}
			prev = n.X1
		}
	}
}

func TestOrderModes(t *testing.T) {
	cases := []struct {
		order      OrderE
		want1      []string // the depth-1 row, left to right
		want2      []string // the depth-2 row, left to right
		wantReason string
	}{
		{OrderValueDesc, []string{"parse", "eval"}, []string{"lex", "ast", "walk", "emit"},
			"parse(9) outweighs eval(8), and within each the wider child leads"},
		{OrderLabel, []string{"eval", "parse"}, []string{"emit", "walk", "ast", "lex"},
			"eval sorts before parse, so its children occupy the left of row 2"},
		{OrderInput, []string{"parse", "eval"}, []string{"lex", "ast", "walk", "emit"},
			"the order the columns list them in"},
	}
	for _, tc := range cases {
		lay := mustCompute(t, testProfile(), Options{Order: tc.order})
		for d, want := range [][]string{tc.want1, tc.want2} {
			var got []string
			for _, ni := range lay.Rows[d+1] {
				got = append(got, lay.Nodes[ni].Label)
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("order %d: row %d = %v, want %v (%s)", tc.order, d+1, got, want, tc.wantReason)
			}
		}
	}
}

func TestOrientationSign(t *testing.T) {
	ice := mustCompute(t, testProfile(), Options{Orientation: OrientIcicle})
	flame := mustCompute(t, testProfile(), Options{Orientation: OrientFlame})

	root := byLabel(t, ice, "main")
	if root.Y0 != -1 || root.Y1 != 0 {
		t.Errorf("icicle root y = [%v,%v], want [-1,0] (root at the top)", root.Y0, root.Y1)
	}
	root = byLabel(t, flame, "main")
	if root.Y0 != 0 || root.Y1 != 1 {
		t.Errorf("flame root y = [%v,%v], want [0,1] (root at the bottom)", root.Y0, root.Y1)
	}
	// x is orientation-independent.
	for _, label := range []string{"main", "parse", "lex", "emit"} {
		a, b := byLabel(t, ice, label), byLabel(t, flame, label)
		if a.X0 != b.X0 || a.X1 != b.X1 {
			t.Errorf("%s x differs between orientations: %v vs %v", label, a.X0, b.X0)
		}
	}
}

func TestDepthAtRoundTrip(t *testing.T) {
	for _, orient := range []OrientationE{OrientIcicle, OrientFlame} {
		lay := mustCompute(t, testProfile(), Options{Orientation: orient})
		for i := range lay.Nodes {
			n := &lay.Nodes[i]
			mid := (n.Y0 + n.Y1) / 2
			d, ok := lay.DepthAt(mid)
			if !ok || d != int(n.Depth) {
				t.Errorf("orient %d: DepthAt(%v) = (%d,%v), want (%d,true)", orient, mid, d, ok, n.Depth)
			}
		}
		// Past either end of the rows there is no depth.
		if _, ok := lay.DepthAt(99); ok {
			t.Errorf("orient %d: DepthAt reported a row far above the plot", orient)
		}
		if _, ok := lay.DepthAt(-99); ok {
			t.Errorf("orient %d: DepthAt reported a row far below the plot", orient)
		}
	}
}

// RowDist is the sign rule read backwards, so it has to invert rowSpan
// exactly — every node's own span must map back onto its own depth, in both
// orientations, with no off-by-one at the row boundaries.
func TestRowDistInvertsRowSpan(t *testing.T) {
	for _, orient := range []OrientationE{OrientIcicle, OrientFlame} {
		lay := mustCompute(t, testProfile(), Options{Orientation: orient})
		for i := range lay.Nodes {
			n := &lay.Nodes[i]
			// The root-side edge of a row is at exactly its depth, and the
			// far edge one short of the next.
			for _, y := range []float64{n.Y0, n.Y1, (n.Y0 + n.Y1) / 2} {
				d := lay.RowDist(y)
				if d < float64(n.Depth) || d > float64(n.Depth)+1 {
					t.Errorf("orient %d: RowDist(%v) = %v, outside row %d", orient, y, d, n.Depth)
				}
			}
			mid := (n.Y0 + n.Y1) / 2
			if got := math.Floor(lay.RowDist(mid)); got != float64(n.Depth) {
				t.Errorf("orient %d: floor(RowDist(%v)) = %v, want depth %d", orient, mid, got, n.Depth)
			}
		}
		// It is the same rule DepthAt applies, not a second copy of it.
		for _, y := range []float64{-2.5, -0.5, 0, 0.5, 2.5} {
			d, ok := lay.DepthAt(y)
			dist := lay.RowDist(y)
			wantOK := dist >= 0 && dist < float64(len(lay.Rows))
			if ok != wantOK || (ok && d != int(dist)) {
				t.Errorf("orient %d: DepthAt(%v) = (%d,%v) but RowDist says %v", orient, y, d, ok, dist)
			}
		}
	}
	// Nil-safe, like every other method on Layout.
	var nilLay *Layout
	if got := nilLay.RowDist(3); got != 3 {
		t.Errorf("nil layout RowDist = %v", got)
	}
}

// A plot-space coordinate is whatever the transform produced, and a degenerate
// transform produces non-finite values. They have to be rejected rather than
// converted: int() of a float outside the int range is implementation-defined,
// and the value amd64 picks is INT_MIN — which is below every upper bound and
// so would sail through a check placed after the conversion.
//
// The sign rule means the two orientations fail on opposite infinities, so
// both are swept.
func TestDepthAtRejectsNonFinite(t *testing.T) {
	nan := math.NaN()
	for _, orient := range []OrientationE{OrientIcicle, OrientFlame} {
		lay := mustCompute(t, testProfile(), Options{Orientation: orient})
		for _, y := range []float64{nan, math.Inf(1), math.Inf(-1)} {
			if d, ok := lay.DepthAt(y); ok {
				t.Errorf("orient %d: DepthAt(%v) = (%d,true), want no depth", orient, y, d)
			}
		}
		// The same values reach NodeAt through both of its arguments; a NaN x
		// is rejected by the row search rather than the depth guard, so it is
		// worth pinning separately.
		for _, p := range [][2]float64{
			{nan, nan}, {0, nan}, {0, math.Inf(1)}, {0, math.Inf(-1)},
			{nan, 0}, {math.Inf(1), 0}, {math.Inf(-1), 0},
		} {
			if got := lay.NodeAt(p[0], p[1]); got != -1 {
				t.Errorf("orient %d: NodeAt(%v,%v) = %d, want -1", orient, p[0], p[1], got)
			}
		}
	}
}

// A probe anywhere inside a node's rectangle must return that node, and the
// self-time gap under a parent must return nothing.
func TestNodeAtSweep(t *testing.T) {
	for _, orient := range []OrientationE{OrientIcicle, OrientFlame} {
		lay := mustCompute(t, testProfile(), Options{Orientation: orient})
		for i := range lay.Nodes {
			n := &lay.Nodes[i]
			y := (n.Y0 + n.Y1) / 2
			for _, f := range []float64{0.0, 0.01, 0.5, 0.99} {
				x := n.X0 + f*(n.X1-n.X0)
				if got := lay.NodeAt(x, y); got != i {
					t.Errorf("orient %d: NodeAt(%v,%v) = %d, want %d (%s)", orient, x, y, got, i, n.Label)
				}
			}
		}
		// main's self time is [17,18] at depth 0, so depth 1 is empty there.
		row1Y := (byLabel(t, lay, "parse").Y0 + byLabel(t, lay, "parse").Y1) / 2
		if got := lay.NodeAt(17.5, row1Y); got != -1 {
			t.Errorf("orient %d: NodeAt in the uncovered gap = %d, want -1", orient, got)
		}
		// Past the right edge of the whole plot.
		if got := lay.NodeAt(18.5, row1Y); got != -1 {
			t.Errorf("orient %d: NodeAt past the total = %d, want -1", orient, got)
		}
	}
}

// The hit test binary-searches a row, which is only correct if pre-order
// emission really does leave each row sorted by x.
func TestRowsAreSortedByX(t *testing.T) {
	lay := mustCompute(t, testProfile(), Options{})
	for d, row := range lay.Rows {
		for i := 1; i < len(row); i++ {
			prev, cur := &lay.Nodes[row[i-1]], &lay.Nodes[row[i]]
			if cur.X0 < prev.X0 {
				t.Errorf("row %d is not sorted: %s at %v follows %s at %v", d, cur.Label, cur.X0, prev.Label, prev.X0)
			}
			if cur.X0 < prev.X1-eps {
				t.Errorf("row %d overlaps: %s [%v,%v] and %s [%v,%v]", d, prev.Label, prev.X0, prev.X1, cur.Label, cur.X0, cur.X1)
			}
		}
	}
}

func TestPathTo(t *testing.T) {
	lay := mustCompute(t, testProfile(), Options{})
	var emit int
	for i := range lay.Nodes {
		if lay.Nodes[i].Label == "emit" {
			emit = i
		}
	}
	got := lay.PathTo(emit)
	var labels []string
	for _, ni := range got {
		labels = append(labels, lay.Nodes[ni].Label)
	}
	want := "main,eval,emit"
	if strings.Join(labels, ",") != want {
		t.Errorf("PathTo(emit) = %v, want %s", labels, want)
	}
	if lay.PathTo(-1) != nil || lay.PathTo(len(lay.Nodes)) != nil {
		t.Error("PathTo returned a path for an out-of-range index")
	}
}

// A forest is the natural shape of a per-thread profile: roots sit side by
// side and the total is their sum.
func TestForestRootsSideBySide(t *testing.T) {
	tr := Tree{
		Labels:  []string{"g1", "g2", "work"},
		Parents: []int32{-1, -1, 0},
		Self:    []float64{1, 4, 3},
	}
	lay := mustCompute(t, tr, Options{})
	if math.Abs(lay.Report.Total-8) > eps {
		t.Errorf("total = %v, want 8", lay.Report.Total)
	}
	// g1 totals 4 and g2 totals 4; the tie falls back to input order.
	g1, g2 := byLabel(t, lay, "g1"), byLabel(t, lay, "g2")
	if g1.X0 != 0 || math.Abs(g1.X1-4) > eps {
		t.Errorf("g1 x = [%v,%v], want [0,4]", g1.X0, g1.X1)
	}
	if math.Abs(g2.X0-4) > eps || math.Abs(g2.X1-8) > eps {
		t.Errorf("g2 x = [%v,%v], want [4,8]", g2.X0, g2.X1)
	}
	if g1.Parent != -1 || g2.Parent != -1 {
		t.Error("a root reported a parent")
	}
}

// A producer cannot always promise parents come first, so the layout must not
// depend on it.
func TestParentsMayFollowChildren(t *testing.T) {
	// Same shape as testProfile's parse subtree, listed leaf-first.
	tr := Tree{
		Labels:  []string{"lex", "ast", "parse"},
		Parents: []int32{2, 2, -1},
		Self:    []float64{4, 3, 2},
	}
	lay := mustCompute(t, tr, Options{})
	parse := byLabel(t, lay, "parse")
	if math.Abs(parse.Total-9) > eps {
		t.Errorf("parse total = %v, want 9", parse.Total)
	}
	if parse.Depth != 0 {
		t.Errorf("parse depth = %d, want 0", parse.Depth)
	}
	if got := byLabel(t, lay, "lex").Depth; got != 1 {
		t.Errorf("lex depth = %d, want 1", got)
	}
}

// Pruning must be purely subtractive: a node that survives keeps the exact
// position it had when nothing was pruned.
func TestPruningIsSubtractive(t *testing.T) {
	tr := testProfile()
	// A speck under eval: 0.5% of the 18-unit total once added.
	tr.Labels = append(tr.Labels, "speck")
	tr.Parents = append(tr.Parents, 4)
	tr.Self = append(tr.Self, 0.05)

	full := mustCompute(t, tr, Options{})
	pruned := mustCompute(t, tr, Options{MinFraction: 0.01})

	if full.Report.Pruned != 0 {
		t.Errorf("unpruned layout reported %d pruned nodes", full.Report.Pruned)
	}
	if pruned.Report.Pruned != 1 {
		t.Fatalf("pruned %d nodes, want 1", pruned.Report.Pruned)
	}
	if math.Abs(pruned.Report.PrunedValue-0.05) > eps {
		t.Errorf("pruned value = %v, want 0.05", pruned.Report.PrunedValue)
	}
	if pruned.Report.Nodes != full.Report.Nodes-1 {
		t.Errorf("pruned layout has %d nodes, want %d", pruned.Report.Nodes, full.Report.Nodes-1)
	}
	// Every surviving node keeps its span, and the total is untouched.
	for _, label := range []string{"main", "parse", "lex", "ast", "eval", "walk", "emit"} {
		a, b := byLabel(t, full, label), byLabel(t, pruned, label)
		if math.Abs(a.X0-b.X0) > eps || math.Abs(a.X1-b.X1) > eps {
			t.Errorf("%s moved when pruning was enabled: [%v,%v] -> [%v,%v]", label, a.X0, a.X1, b.X0, b.X1)
		}
	}
	if math.Abs(full.Report.Total-pruned.Report.Total) > eps {
		t.Error("pruning changed the reported total")
	}
}

// A pruned subtree is counted whole, not just at its root.
func TestPruningCountsWholeSubtree(t *testing.T) {
	tr := Tree{
		Labels:  []string{"root", "big", "tiny", "tinyKid", "tinyGrandkid"},
		Parents: []int32{-1, 0, 0, 2, 3},
		Self:    []float64{0, 100, 1, 1, 1},
	}
	lay := mustCompute(t, tr, Options{MinFraction: 0.1})
	if lay.Report.Pruned != 3 {
		t.Errorf("pruned = %d, want 3 (tiny and its two descendants)", lay.Report.Pruned)
	}
	if lay.Report.Nodes != 2 {
		t.Errorf("nodes = %d, want 2", lay.Report.Nodes)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		tree Tree
		want string
	}{
		{"empty", Tree{}, "no nodes"},
		{"ragged columns", Tree{
			Labels: []string{"a", "b"}, Parents: []int32{-1}, Self: []float64{1, 1},
		}, "column lengths disagree"},
		{"self parent", Tree{
			Labels: []string{"a"}, Parents: []int32{0}, Self: []float64{1},
		}, "its own parent"},
		{"parent out of range", Tree{
			Labels: []string{"a"}, Parents: []int32{7}, Self: []float64{1},
		}, "out of range"},
		{"negative parent", Tree{
			Labels: []string{"a"}, Parents: []int32{-2}, Self: []float64{1},
		}, "out of range"},
		{"negative self", Tree{
			Labels: []string{"a"}, Parents: []int32{-1}, Self: []float64{-1},
		}, "finite and >= 0"},
		{"NaN self", Tree{
			Labels: []string{"a"}, Parents: []int32{-1}, Self: []float64{math.NaN()},
		}, "finite and >= 0"},
		{"infinite self", Tree{
			Labels: []string{"a"}, Parents: []int32{-1}, Self: []float64{math.Inf(1)},
		}, "finite and >= 0"},
		{"two-node cycle", Tree{
			Labels: []string{"a", "b"}, Parents: []int32{1, 0}, Self: []float64{1, 1},
		}, "cycle"},
		{"cycle off a root", Tree{
			Labels: []string{"r", "a", "b", "c"}, Parents: []int32{-1, 3, 1, 2}, Self: []float64{1, 1, 1, 1},
		}, "cycle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.tree.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
			if _, cerr := Compute(tc.tree, Options{}); cerr == nil {
				t.Error("Compute accepted what Validate rejected")
			}
		})
	}
}

// A tree of nothing but zeros has no width to draw; that is a host-visible
// state, not something to render as a degenerate strip.
func TestZeroTotalRejected(t *testing.T) {
	tr := Tree{Labels: []string{"a", "b"}, Parents: []int32{-1, 0}, Self: []float64{0, 0}}
	if err := tr.Validate(); err != nil {
		t.Fatalf("Validate rejected a well-formed zero tree: %v", err)
	}
	_, err := Compute(tr, Options{})
	if err == nil {
		t.Fatal("Compute accepted a zero-total tree")
	}
	if !strings.Contains(err.Error(), "nothing to lay out") {
		t.Errorf("error %q does not explain the zero total", err)
	}
}

func TestComputeCopiesItsInput(t *testing.T) {
	tr := testProfile()
	lay := mustCompute(t, tr, Options{})
	tr.Labels[0] = "clobbered"
	tr.Self[0] = 999
	if got := byLabel(t, lay, "main"); got == nil {
		t.Fatal("the layout followed a mutation of the input labels")
	}
}

func TestDeepChainDoesNotRecurse(t *testing.T) {
	const depth = 20000
	tr := Tree{
		Labels:  make([]string, depth),
		Parents: make([]int32, depth),
		Self:    make([]float64, depth),
	}
	for i := range depth {
		tr.Labels[i] = fmt.Sprintf("f%d", i)
		tr.Parents[i] = int32(i - 1) // -1 at i == 0
		tr.Self[i] = 1
	}
	lay := mustCompute(t, tr, Options{})
	if lay.Report.Rows != depth {
		t.Errorf("rows = %d, want %d", lay.Report.Rows, depth)
	}
	if math.Abs(lay.Report.Total-depth) > 1e-6 {
		t.Errorf("total = %v, want %d", lay.Report.Total, depth)
	}
}

// nz folds negative zero onto zero so a golden never diffs on the sign of a
// value that is not there.
func nz(v float64) float64 {
	r := math.Round(v*1e6) / 1e6
	if r == 0 {
		return 0
	}
	return r
}

// dump renders a layout as the stable text the golden files hold.
func dump(lay *Layout) string {
	var b strings.Builder
	fmt.Fprintf(&b, "orientation %d rows %d nodes %d\n", lay.Orientation, lay.Report.Rows, lay.Report.Nodes)
	for i := range lay.Nodes {
		n := &lay.Nodes[i]
		fmt.Fprintf(&b, "node %-6s depth=%d parent=%d idx=%d x=[%.6f,%.6f] y=[%.6f,%.6f] self=%.3f total=%.3f\n",
			n.Label, n.Depth, n.Parent, n.Index, nz(n.X0), nz(n.X1), nz(n.Y0), nz(n.Y1), n.Self, n.Total)
	}
	for d, row := range lay.Rows {
		fmt.Fprintf(&b, "row %d:", d)
		for _, ni := range row {
			fmt.Fprintf(&b, " %s", lay.Nodes[ni].Label)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "report total=%.3f unit=%q pruned=%d prunedValue=%.3f maxDepth=%d\n",
		lay.Report.Total, lay.Report.Unit, lay.Report.Pruned, nz(lay.Report.PrunedValue), lay.Report.MaxDepth)
	return b.String()
}

func checkGolden(t *testing.T, name string, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(want) != got {
		t.Errorf("layout differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func TestGoldenIcicle(t *testing.T) {
	checkGolden(t, "profile_icicle.golden",
		dump(mustCompute(t, testProfile(), Options{Unit: "ms"})))
}

func TestGoldenFlame(t *testing.T) {
	checkGolden(t, "profile_flame.golden",
		dump(mustCompute(t, testProfile(), Options{Orientation: OrientFlame, Order: OrderLabel, Unit: "ms"})))
}

package sankey

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// eps is the slack every geometric assertion below allows. The layout is
// float64 arithmetic on values around 1, so a few ULP of drift per summed
// term is expected; anything larger is a real defect.
const eps = 1e-9

// testEnergy is an energy-balance-shaped diagram: three primary sources fan
// into two conversion stages and end in useful work and losses. It exercises
// a link that skips a stage (solar straight to grid), a node whose inflow and
// outflow balance, and one that deliberately does not.
func testEnergy() Diagram {
	return Diagram{
		Unit: "PJ",
		Nodes: []Node{
			{ID: "coal", Label: "coal"},
			{ID: "gas", Label: "gas"},
			{ID: "solar", Label: "solar"},
			{ID: "thermal", Label: "thermal plant"},
			{ID: "grid", Label: "grid"},
			{ID: "industry", Label: "industry"},
			{ID: "homes", Label: "homes"},
			{ID: "losses", Label: "losses"},
		},
		Links: []Link{
			{Source: "coal", Target: "thermal", Value: 40},
			{Source: "gas", Target: "thermal", Value: 25},
			{Source: "solar", Target: "grid", Value: 15},
			{Source: "thermal", Target: "grid", Value: 39},
			{Source: "thermal", Target: "losses", Value: 26},
			{Source: "grid", Target: "industry", Value: 30},
			{Source: "grid", Target: "homes", Value: 20},
			{Source: "grid", Target: "losses", Value: 4},
		},
	}
}

// testCohort is an alluvial-shaped diagram: the same population re-labelled
// at three checkpoints, with caller-fixed stages and order.
func testCohort() Diagram {
	return Diagram{
		Mode: ModeAlluvial,
		Unit: "accounts",
		Nodes: []Node{
			{ID: "t0.free", Label: "free", Stage: 0, Order: 0},
			{ID: "t0.paid", Label: "paid", Stage: 0, Order: 1},
			{ID: "t1.free", Label: "free", Stage: 1, Order: 0},
			{ID: "t1.paid", Label: "paid", Stage: 1, Order: 1},
			{ID: "t1.gone", Label: "churned", Stage: 1, Order: 2},
			{ID: "t2.free", Label: "free", Stage: 2, Order: 0},
			{ID: "t2.paid", Label: "paid", Stage: 2, Order: 1},
			{ID: "t2.gone", Label: "churned", Stage: 2, Order: 2},
		},
		Links: []Link{
			{Source: "t0.free", Target: "t1.free", Value: 620},
			{Source: "t0.free", Target: "t1.paid", Value: 90},
			{Source: "t0.free", Target: "t1.gone", Value: 190},
			{Source: "t0.paid", Target: "t1.paid", Value: 260},
			{Source: "t0.paid", Target: "t1.gone", Value: 40},
			{Source: "t1.free", Target: "t2.free", Value: 500},
			{Source: "t1.free", Target: "t2.paid", Value: 60},
			{Source: "t1.free", Target: "t2.gone", Value: 60},
			{Source: "t1.paid", Target: "t2.paid", Value: 320},
			{Source: "t1.paid", Target: "t2.gone", Value: 30},
			{Source: "t1.gone", Target: "t2.gone", Value: 230},
		},
	}
}

func mustCompute(t *testing.T, d Diagram, o Options) *Layout {
	t.Helper()
	lay, err := Compute(d, o)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	return lay
}

// nz folds negative zero onto zero. Accumulating link faces can land a
// hair below the bar edge, and "-0.000000" in a golden would be a diff
// waiting to happen for no geometric reason.
func nz(v float64) float64 {
	r := math.Round(v*1e6) / 1e6 // the golden prints 6 decimals anyway
	if r == 0 {
		return 0
	}
	return r
}

// dump renders a layout as the stable text the golden files hold.
func dump(lay *Layout) string {
	var b strings.Builder
	fmt.Fprintf(&b, "stages %d scale %.6f pad %.6f width %.6f\n",
		lay.Stages, lay.Scale, lay.NodePad, lay.NodeWidth)
	for i := range lay.Nodes {
		n := &lay.Nodes[i]
		fmt.Fprintf(&b, "node %-10s stage=%d idx=%d x=[%.6f,%.6f] y=[%.6f,%.6f] v=%.3f in=%.3f out=%.3f\n",
			n.ID, n.Stage, n.Index, nz(n.X0), nz(n.X1), nz(n.Y0), nz(n.Y1), n.Value, n.In, n.Out)
	}
	for i := range lay.Links {
		l := &lay.Links[i]
		fmt.Fprintf(&b, "link %-10s -> %-10s v=%.3f s=(%.6f,%.6f,%.6f) t=(%.6f,%.6f,%.6f)\n",
			lay.Nodes[l.Source].ID, lay.Nodes[l.Target].ID, l.Value,
			nz(l.SX), nz(l.SY0), nz(l.SY1), nz(l.TX), nz(l.TY0), nz(l.TY1))
	}
	fmt.Fprintf(&b, "report total=%.3f unit=%q thin=%d nonconserving=%v\n",
		lay.Report.Total, lay.Report.Unit, lay.Report.ThinLinks, lay.Report.NonConserving)
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

func TestGoldenEnergy(t *testing.T) {
	checkGolden(t, "energy.golden", dump(mustCompute(t, testEnergy(), Options{})))
}

func TestGoldenCohort(t *testing.T) {
	checkGolden(t, "cohort.golden", dump(mustCompute(t, testCohort(), Options{})))
}

func TestDeterminism(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    Diagram
	}{{"energy", testEnergy()}, {"cohort", testCohort()}} {
		t.Run(tc.name, func(t *testing.T) {
			a := mustCompute(t, tc.d, Options{})
			b := mustCompute(t, tc.d, Options{})
			if !reflect.DeepEqual(a, b) {
				t.Error("two runs on equal input are not deep-equal")
			}
		})
	}
}

// TestGlobalScale pins the property that makes the diagram readable: one
// factor converts value to height everywhere. A per-stage scale would let a
// ribbon change width between its two ends.
func TestGlobalScale(t *testing.T) {
	lay := mustCompute(t, testEnergy(), Options{})
	for i := range lay.Nodes {
		n := &lay.Nodes[i]
		if got, want := n.Y1-n.Y0, n.Value*lay.Scale; math.Abs(got-want) > eps {
			t.Errorf("node %s height %.9f, want value*scale %.9f", n.ID, got, want)
		}
	}
	for i := range lay.Links {
		l := &lay.Links[i]
		want := l.Value * lay.Scale
		if math.Abs(l.SY1-l.SY0-want) > eps {
			t.Errorf("link %d source face %.9f, want %.9f", i, l.SY1-l.SY0, want)
		}
		if math.Abs(l.TY1-l.TY0-want) > eps {
			t.Errorf("link %d target face %.9f, want %.9f", i, l.TY1-l.TY0, want)
		}
	}
}

// TestFacesTileTheBar checks that the links at a node exactly subdivide its
// bar, top-down, with no gap and no overlap — the reason vertical extrusion
// was chosen over a perpendicular stroke (ADR-0159 SD2).
func TestFacesTileTheBar(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    Diagram
	}{{"energy", testEnergy()}, {"cohort", testCohort()}} {
		t.Run(tc.name, func(t *testing.T) {
			lay := mustCompute(t, tc.d, Options{})
			type face struct{ y0, y1 float64 }
			out := make(map[int][]face)
			in := make(map[int][]face)
			for i := range lay.Links {
				l := &lay.Links[i]
				out[l.Source] = append(out[l.Source], face{l.SY0, l.SY1})
				in[l.Target] = append(in[l.Target], face{l.TY0, l.TY1})
			}
			check := func(side string, faces map[int][]face) {
				for ni, fs := range faces {
					n := &lay.Nodes[ni]
					slices.SortFunc(fs, func(a, b face) int {
						switch {
						case a.y1 > b.y1:
							return -1
						case a.y1 < b.y1:
							return 1
						}
						return 0
					})
					// The stack starts at the bar's top edge.
					if math.Abs(fs[0].y1-n.Y1) > eps {
						t.Errorf("%s %s: first face top %.9f, want bar top %.9f", side, n.ID, fs[0].y1, n.Y1)
					}
					for k := 1; k < len(fs); k++ {
						if math.Abs(fs[k].y1-fs[k-1].y0) > eps {
							t.Errorf("%s %s: face %d starts at %.9f, previous ended at %.9f",
								side, n.ID, k, fs[k].y1, fs[k-1].y0)
						}
					}
					// And ends no lower than the bar does.
					if last := fs[len(fs)-1].y0; last < n.Y0-eps {
						t.Errorf("%s %s: faces overflow the bar bottom %.9f > %.9f", side, n.ID, n.Y0, last)
					}
				}
			}
			check("out", out)
			check("in", in)
		})
	}
}

// TestFarEndOrdering pins the rule that does most of the crossing reduction:
// at a node face, links are stacked in the order of their far end's centre.
func TestFarEndOrdering(t *testing.T) {
	lay := mustCompute(t, testEnergy(), Options{})
	centre := func(i int) float64 { return (lay.Nodes[i].Y0 + lay.Nodes[i].Y1) / 2 }
	type entry struct {
		faceTop float64
		farY    float64
	}
	out := make(map[int][]entry)
	for i := range lay.Links {
		l := &lay.Links[i]
		out[l.Source] = append(out[l.Source], entry{l.SY1, centre(l.Target)})
	}
	for ni, es := range out {
		slices.SortFunc(es, func(a, b entry) int {
			switch {
			case a.faceTop > b.faceTop:
				return -1
			case a.faceTop < b.faceTop:
				return 1
			}
			return 0
		})
		for k := 1; k < len(es); k++ {
			// Higher on the bar must mean a higher (or equal) far end.
			if es[k].farY > es[k-1].farY+eps {
				t.Errorf("node %s: face %d sits below face %d but its target is higher (%.6f > %.6f)",
					lay.Nodes[ni].ID, k, k-1, es[k].farY, es[k-1].farY)
			}
		}
	}
}

// TestNoOverlapWithinStage checks collision resolution left the stage tidy
// and inside the box.
func TestNoOverlapWithinStage(t *testing.T) {
	lay := mustCompute(t, testEnergy(), Options{})
	byStage := make(map[int][]*NodeLayout)
	for i := range lay.Nodes {
		n := &lay.Nodes[i]
		byStage[n.Stage] = append(byStage[n.Stage], n)
		if n.Y0 < -eps || n.Y1 > 1+eps {
			t.Errorf("node %s escapes the unit box: y=[%.6f,%.6f]", n.ID, n.Y0, n.Y1)
		}
	}
	for s, nodes := range byStage {
		slices.SortFunc(nodes, func(a, b *NodeLayout) int { return a.Index - b.Index })
		for k := 1; k < len(nodes); k++ {
			gap := nodes[k-1].Y0 - nodes[k].Y1
			if gap < lay.NodePad-eps {
				t.Errorf("stage %d: %s and %s are %.9f apart, want >= %.9f",
					s, nodes[k-1].ID, nodes[k].ID, gap, lay.NodePad)
			}
		}
	}
}

func TestStagesDerivedAndAligned(t *testing.T) {
	d := testEnergy()
	for _, tc := range []struct {
		align Align
		name  string
		// losses is a sink reached from both thermal (stage 1) and grid
		// (stage 2), so alignment is visible on it.
		wantLosses int
	}{
		{AlignJustify, "justify", 3},
		{AlignLeft, "left", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lay := mustCompute(t, d, Options{Align: tc.align})
			for i := range lay.Nodes {
				if lay.Nodes[i].ID == "losses" && lay.Nodes[i].Stage != tc.wantLosses {
					t.Errorf("losses at stage %d, want %d", lay.Nodes[i].Stage, tc.wantLosses)
				}
			}
			// A link must always point rightwards.
			for i := range lay.Links {
				l := &lay.Links[i]
				if lay.Nodes[l.Source].Stage >= lay.Nodes[l.Target].Stage {
					t.Errorf("link %d goes from stage %d to %d", i,
						lay.Nodes[l.Source].Stage, lay.Nodes[l.Target].Stage)
				}
			}
		})
	}
}

// TestAlluvialKeepsCallerOrder checks the mode's whole point: a category stays
// in the same relative slot at every checkpoint.
func TestAlluvialKeepsCallerOrder(t *testing.T) {
	lay := mustCompute(t, testCohort(), Options{})
	want := map[string]int{"free": 0, "paid": 1, "churned": 2}
	for i := range lay.Nodes {
		n := &lay.Nodes[i]
		if got := want[n.Label]; got != n.Index {
			t.Errorf("node %s (label %q) at index %d, want %d", n.ID, n.Label, n.Index, got)
		}
	}
}

func TestReportFlagsNonConservingNode(t *testing.T) {
	lay := mustCompute(t, testEnergy(), Options{})
	// thermal takes 65 and emits 65; grid takes 54 and emits 54; every other
	// node is a source or a sink. Nothing should be flagged.
	if len(lay.Report.NonConserving) != 0 {
		t.Errorf("balanced diagram flagged %v", lay.Report.NonConserving)
	}
	d := testEnergy()
	d.Links[3].Value = 30 // thermal -> grid, breaking thermal's balance
	lay = mustCompute(t, d, Options{})
	if !slices.Contains(lay.Report.NonConserving, "thermal") {
		t.Errorf("unbalanced thermal not reported; got %v", lay.Report.NonConserving)
	}
	if lay.Report.Unit != "PJ" {
		t.Errorf("unit %q not carried into the report", lay.Report.Unit)
	}
}

func TestReportCountsThinLinks(t *testing.T) {
	d := testEnergy()
	d.Nodes = append(d.Nodes, Node{ID: "trace"})
	d.Links = append(d.Links, Link{Source: "grid", Target: "trace", Value: 0.001})
	lay := mustCompute(t, d, Options{})
	if lay.Report.ThinLinks != 1 {
		t.Errorf("ThinLinks = %d, want 1", lay.Report.ThinLinks)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		d    Diagram
		want string
	}{
		{"no nodes", Diagram{}, "no nodes"},
		{"empty id", Diagram{Nodes: []Node{{ID: ""}}}, "empty id"},
		{"duplicate id", Diagram{Nodes: []Node{{ID: "a"}, {ID: "a"}}}, "duplicate node id"},
		{"unknown source", Diagram{
			Nodes: []Node{{ID: "a"}},
			Links: []Link{{Source: "z", Target: "a", Value: 1}},
		}, "unknown source"},
		{"unknown target", Diagram{
			Nodes: []Node{{ID: "a"}},
			Links: []Link{{Source: "a", Target: "z", Value: 1}},
		}, "unknown target"},
		{"self link", Diagram{
			Nodes: []Node{{ID: "a"}},
			Links: []Link{{Source: "a", Target: "a", Value: 1}},
		}, "self-link"},
		{"zero value", Diagram{
			Nodes: []Node{{ID: "a"}, {ID: "b"}},
			Links: []Link{{Source: "a", Target: "b", Value: 0}},
		}, "values must be finite"},
		{"nan value", Diagram{
			Nodes: []Node{{ID: "a"}, {ID: "b"}},
			Links: []Link{{Source: "a", Target: "b", Value: math.NaN()}},
		}, "values must be finite"},
		{"cycle", Diagram{
			Nodes: []Node{{ID: "a"}, {ID: "b"}, {ID: "c"}},
			Links: []Link{
				{Source: "a", Target: "b", Value: 1},
				{Source: "b", Target: "c", Value: 1},
				{Source: "c", Target: "a", Value: 1},
			},
		}, "cycle"},
		{"alluvial skips a stage", Diagram{
			Mode:  ModeAlluvial,
			Nodes: []Node{{ID: "a", Stage: 0}, {ID: "b", Stage: 2}},
			Links: []Link{{Source: "a", Target: "b", Value: 1}},
		}, "adjacent stages"},
		{"alluvial negative stage", Diagram{
			Mode:  ModeAlluvial,
			Nodes: []Node{{ID: "a", Stage: -1}},
		}, "stages must be >= 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.d.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
			if _, cErr := Compute(tc.d, Options{}); cErr == nil {
				t.Error("Compute accepted what Validate rejected")
			}
		})
	}
}

func TestValidateAcceptsCyclesNever(t *testing.T) {
	// A cycle is rejected in Sankey mode; alluvial mode cannot express one,
	// because links must go from stage n to n+1.
	d := Diagram{
		Mode:  ModeAlluvial,
		Nodes: []Node{{ID: "a", Stage: 0}, {ID: "b", Stage: 1}},
		Links: []Link{{Source: "a", Target: "b", Value: 1}, {Source: "b", Target: "a", Value: 1}},
	}
	if err := d.Validate(); err == nil {
		t.Error("alluvial accepted a backwards link")
	}
}

func TestSingleStageDiagram(t *testing.T) {
	d := Diagram{Nodes: []Node{{ID: "only"}}}
	lay := mustCompute(t, d, Options{})
	if lay.Stages != 1 {
		t.Errorf("Stages = %d, want 1", lay.Stages)
	}
	// A lone node has no value, so it has no height; it must still be placed
	// inside the box rather than at NaN.
	n := lay.Nodes[0]
	if math.IsNaN(n.X0) || math.IsNaN(n.Y0) {
		t.Errorf("lone node placed at NaN: %+v", n)
	}
}

// TestPadClampedToFit guards the stage that is too crowded for its padding:
// the pad shrinks rather than driving the available height negative.
func TestPadClampedToFit(t *testing.T) {
	d := Diagram{Nodes: []Node{{ID: "src"}}}
	for i := range 200 {
		id := fmt.Sprintf("leaf%03d", i)
		d.Nodes = append(d.Nodes, Node{ID: id})
		d.Links = append(d.Links, Link{Source: "src", Target: id, Value: 1})
	}
	lay := mustCompute(t, d, Options{})
	if lay.NodePad >= DefaultNodePad {
		t.Errorf("NodePad %.6f was not clamped below the default %.6f", lay.NodePad, DefaultNodePad)
	}
	if lay.Scale <= 0 || math.IsInf(lay.Scale, 0) || math.IsNaN(lay.Scale) {
		t.Errorf("Scale = %v", lay.Scale)
	}
}

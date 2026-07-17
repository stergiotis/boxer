package pipelineview

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// testPipeline is the canned shell-style pipeline the ADR-0119 M2 demo also
// draws: a four-column spine with one parallel group, a north config file, a
// south stderr file + artifact, an east store sink, a skip edge and a
// feedback edge.
func testPipeline() Pipeline {
	return Pipeline{
		Root: Group{Children: []Element{
			Stage{ID: "fetch", Label: "fetch (curl)", Ports: []Port{{Name: "auth", Class: PortConfig}}},
			Stage{ID: "decompress", Label: "gunzip"},
			Group{Par: true, Children: []Element{
				Stage{ID: "transform", Label: "transform (jq)", Ports: []Port{
					{Name: "stderr", Class: PortDiagnostic},
					{Name: "rejects", Class: PortArtifact},
				}},
				Stage{ID: "stats", Label: "stats (awk)"},
			}},
			Stage{ID: "load", Label: "load (ch-client)", Ports: []Port{{Name: "stderr", Class: PortDiagnostic}}},
		}},
		Endpoints: []Endpoint{
			{ID: "netrc", Label: "~/.netrc", Kind: EndpointFile},
			{ID: "errlog", Label: "errors.log", Kind: EndpointFile},
			{ID: "rejfile", Label: "rejects.jsonl", Kind: EndpointFile},
			{ID: "journald", Label: "journald", Kind: EndpointStream},
			{ID: "warehouse", Label: "warehouse", Sublabel: "localhost:9000", Kind: EndpointStore},
		},
		Edges: []Edge{
			{From: Ref{Endpoint: "netrc"}, To: Ref{Stage: "fetch", Port: "auth"}},
			{From: Ref{Stage: "transform", Port: "stderr"}, To: Ref{Endpoint: "errlog"}},
			{From: Ref{Stage: "transform", Port: "rejects"}, To: Ref{Endpoint: "rejfile"}, Label: "rejected rows"},
			{From: Ref{Stage: "load", Port: "stderr"}, To: Ref{Endpoint: "journald"}},
			{From: Ref{Stage: "load"}, To: Ref{Endpoint: "warehouse"}, Volume: 1 << 30},
			{From: Ref{Stage: "fetch"}, To: Ref{Stage: "load"}, Label: "manifest"},
			{From: Ref{Stage: "load"}, To: Ref{Stage: "fetch"}, Label: "retry"},
		},
	}
}

func mustCompute(t *testing.T, p Pipeline) *Layout {
	t.Helper()
	lay, err := Compute(p, LayoutOpts{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	return lay
}

func nodeByID(t *testing.T, lay *Layout, id string) NodeLayout {
	t.Helper()
	for _, n := range lay.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %q not in layout", id)
	return NodeLayout{}
}

func edgeBetween(t *testing.T, lay *Layout, from, to string) EdgeLayout {
	t.Helper()
	for _, e := range lay.Edges {
		if e.From.Key() == from && e.To.Key() == to {
			return e
		}
	}
	t.Fatalf("edge %s -> %s not in layout", from, to)
	return EdgeLayout{}
}

// debugString serialises a layout compactly for the golden comparison. One
// decimal is enough to pin the geometry while staying stable.
func debugString(lay *Layout) string {
	var b strings.Builder
	fmt.Fprintf(&b, "size %.1f x %.1f font %.1f\n", lay.Width, lay.Height, lay.FontSize)
	kind := map[NodeKind]string{NodeStage: "stage", NodeEndpoint: "endpoint"}
	for _, n := range lay.Nodes {
		fmt.Fprintf(&b, "node %-8s %-10s %q c=(%.1f,%.1f) w=%.1f h=%.1f\n",
			kind[n.Kind], n.ID, n.Label, n.Center.X, n.Center.Y, n.W, n.H)
	}
	for _, p := range lay.Pins {
		fmt.Fprintf(&b, "pin  %s.%s class=%d (%.1f,%.1f)\n", p.Stage, p.Port, p.Class, p.Pos.X, p.Pos.Y)
	}
	ek := map[EdgeKind]string{EdgeSpine: "spine", EdgeSide: "side", EdgeSkip: "skip", EdgeFeedback: "feedback"}
	for _, e := range lay.Edges {
		fmt.Fprintf(&b, "edge %-8s %s->%s", ek[e.Kind], e.From.Key(), e.To.Key())
		if e.Label != "" {
			fmt.Fprintf(&b, " %q", e.Label)
		}
		if e.Dashed {
			b.WriteString(" dashed")
		}
		for _, pt := range e.Points {
			fmt.Fprintf(&b, " (%.1f,%.1f)", pt.X, pt.Y)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestComputeGolden(t *testing.T) {
	lay := mustCompute(t, testPipeline())
	got := debugString(lay)
	golden := filepath.Join("testdata", "shell_pipeline.golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if got != string(want) {
		t.Errorf("layout differs from golden (run with -update to rewrite):\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestComputeDeterministic(t *testing.T) {
	a := mustCompute(t, testPipeline())
	b := mustCompute(t, testPipeline())
	if !reflect.DeepEqual(a, b) {
		t.Error("two Compute runs over the same model differ")
	}
}

func TestSpineIsStraight(t *testing.T) {
	lay := mustCompute(t, testPipeline())
	fetch := nodeByID(t, lay, "fetch")
	decompress := nodeByID(t, lay, "decompress")
	load := nodeByID(t, lay, "load")
	if fetch.Center.Y != decompress.Center.Y || decompress.Center.Y != load.Center.Y {
		t.Errorf("spine stages not on one line: %v %v %v", fetch.Center.Y, decompress.Center.Y, load.Center.Y)
	}
	// Columns advance left to right.
	if !(fetch.Center.X < decompress.Center.X && decompress.Center.X < load.Center.X) {
		t.Errorf("spine columns not ordered: %v %v %v", fetch.Center.X, decompress.Center.X, load.Center.X)
	}
}

func TestParallelBranchesStack(t *testing.T) {
	lay := mustCompute(t, testPipeline())
	tr := nodeByID(t, lay, "transform")
	st := nodeByID(t, lay, "stats")
	if tr.Center.X != st.Center.X {
		t.Errorf("parallel branches not column-aligned: %v vs %v", tr.Center.X, st.Center.X)
	}
	if tr.Center.Y >= st.Center.Y {
		t.Errorf("tree order not preserved vertically: transform %v should be above stats %v", tr.Center.Y, st.Center.Y)
	}
	// No overlap, including transform's south shelf (errlog + rejfile).
	errlog := nodeByID(t, lay, "errlog")
	if errlog.Center.Y+errlog.H/2 > st.Center.Y-st.H/2 {
		t.Errorf("transform's south endpoints overlap the stats branch: errlog bottom %v vs stats top %v",
			errlog.Center.Y+errlog.H/2, st.Center.Y-st.H/2)
	}
	// The two shelf endpoints must not overlap each other either.
	rejfile := nodeByID(t, lay, "rejfile")
	if errlog.Center.X+errlog.W/2 > rejfile.Center.X-rejfile.W/2 {
		t.Errorf("south-shelf endpoints overlap: errlog right %v vs rejfile left %v",
			errlog.Center.X+errlog.W/2, rejfile.Center.X-rejfile.W/2)
	}
}

func TestPortSidesAreSemantic(t *testing.T) {
	lay := mustCompute(t, testPipeline())
	fetch := nodeByID(t, lay, "fetch")
	netrc := nodeByID(t, lay, "netrc")
	if netrc.Center.Y >= fetch.Center.Y-fetch.H/2 {
		t.Errorf("config endpoint not above its stage: netrc %v vs fetch top %v", netrc.Center.Y, fetch.Center.Y-fetch.H/2)
	}
	tr := nodeByID(t, lay, "transform")
	errlog := nodeByID(t, lay, "errlog")
	if errlog.Center.Y <= tr.Center.Y+tr.H/2 {
		t.Errorf("diagnostic endpoint not below its stage: errlog %v vs transform bottom %v", errlog.Center.Y, tr.Center.Y+tr.H/2)
	}
	load := nodeByID(t, lay, "load")
	wh := nodeByID(t, lay, "warehouse")
	if wh.Center.X-wh.W/2 <= load.Center.X+load.W/2 {
		t.Errorf("axial sink not east of its stage: warehouse left %v vs load right %v", wh.Center.X-wh.W/2, load.Center.X+load.W/2)
	}
	if wh.Center.Y != load.Center.Y {
		t.Errorf("axial sink not on the stage line: %v vs %v", wh.Center.Y, load.Center.Y)
	}
	// Artifact pin sits east of the diagnostic pin on transform's south edge.
	var diagX, artX float64
	for _, p := range lay.Pins {
		if p.Stage == "transform" && p.Port == "stderr" {
			diagX = p.Pos.X
		}
		if p.Stage == "transform" && p.Port == "rejects" {
			artX = p.Pos.X
		}
	}
	if !(diagX < artX) {
		t.Errorf("artifact pin not east of diagnostic pin: stderr %v rejects %v", diagX, artX)
	}
}

func TestSkipAndFeedbackLanes(t *testing.T) {
	lay := mustCompute(t, testPipeline())
	minTop, maxBottom := math.Inf(1), math.Inf(-1)
	for _, n := range lay.Nodes {
		minTop = math.Min(minTop, n.Center.Y-n.H/2)
		maxBottom = math.Max(maxBottom, n.Center.Y+n.H/2)
	}
	skip := edgeBetween(t, lay, "fetch", "load")
	if skip.Kind != EdgeSkip || skip.Dashed {
		t.Errorf("fetch->load should be a solid skip edge, got kind=%d dashed=%v", skip.Kind, skip.Dashed)
	}
	skipLaneY := skip.Points[2].Y
	if skipLaneY >= minTop {
		t.Errorf("skip lane not above the content: lane %v vs topmost node %v", skipLaneY, minTop)
	}
	fb := edgeBetween(t, lay, "load", "fetch")
	if fb.Kind != EdgeFeedback || !fb.Dashed {
		t.Errorf("load->fetch should be a dashed feedback edge, got kind=%d dashed=%v", fb.Kind, fb.Dashed)
	}
	fbLaneY := fb.Points[2].Y
	if fbLaneY <= maxBottom {
		t.Errorf("feedback lane not below the content: lane %v vs bottommost node %v", fbLaneY, maxBottom)
	}
	for _, e := range []EdgeLayout{skip, fb} {
		if len(e.Points) != 6 {
			t.Errorf("lane edge %s->%s: want 6 points, got %d", e.From.Key(), e.To.Key(), len(e.Points))
		}
	}
}

func TestTrackSeparation(t *testing.T) {
	// decompress fans out to two branch heads through the same gap; the two
	// vertical segments must not overlap.
	lay := mustCompute(t, testPipeline())
	toTransform := edgeBetween(t, lay, "decompress", "transform")
	toStats := edgeBetween(t, lay, "decompress", "stats")
	if len(toTransform.Points) != 4 || len(toStats.Points) != 4 {
		t.Fatalf("fan-out edges should be 4-point orthogonals, got %d and %d points",
			len(toTransform.Points), len(toStats.Points))
	}
	x1 := toTransform.Points[1].X
	x2 := toStats.Points[1].X
	if x1 == x2 {
		t.Errorf("fan-out edges share a vertical track at x=%v", x1)
	}
	if math.Abs(x1-x2) < trackSep-1e-9 {
		t.Errorf("tracks closer than trackSep: |%v-%v| < %v", x1, x2, trackSep)
	}
}

func TestExplicitAdjacentReplacesImplied(t *testing.T) {
	p := Pipeline{
		Root: Group{Children: []Element{
			Stage{ID: "a"}, Stage{ID: "b"},
		}},
		Edges: []Edge{{From: Ref{Stage: "a"}, To: Ref{Stage: "b"}, Label: "stdout"}},
	}
	lay := mustCompute(t, p)
	n := 0
	for _, e := range lay.Edges {
		if e.From.Key() == "a" && e.To.Key() == "b" {
			n++
			if e.Label != "stdout" {
				t.Errorf("surviving a->b edge lost its label: %+v", e)
			}
			if e.Kind != EdgeSpine {
				t.Errorf("adjacent explicit edge should route as spine, got kind=%d", e.Kind)
			}
		}
	}
	if n != 1 {
		t.Errorf("want exactly one a->b edge (explicit replaces implied), got %d", n)
	}
}

func TestValidateRejects(t *testing.T) {
	seq := func(els ...Element) Element { return Group{Children: els} }
	cases := []struct {
		name string
		p    Pipeline
		want string
	}{
		{"nil root", Pipeline{}, "nil Root"},
		{"empty group", Pipeline{Root: Group{}}, "empty group"},
		{"dup stage", Pipeline{Root: seq(Stage{ID: "x"}, Stage{ID: "x"})}, "duplicate stage"},
		{"stage endpoint collision", Pipeline{Root: seq(Stage{ID: "x"}),
			Endpoints: []Endpoint{{ID: "x"}}}, "collides"},
		{"unknown stage ref", Pipeline{Root: seq(Stage{ID: "x"}),
			Edges: []Edge{{From: Ref{Stage: "y"}, To: Ref{Stage: "x"}}}}, "unknown stage"},
		{"unknown port", Pipeline{Root: seq(Stage{ID: "x"}),
			Endpoints: []Endpoint{{ID: "e"}},
			Edges:     []Edge{{From: Ref{Stage: "x", Port: "nope"}, To: Ref{Endpoint: "e"}}}}, "no port"},
		{"config port as source", Pipeline{Root: seq(Stage{ID: "x", Ports: []Port{{Name: "cfg", Class: PortConfig}}}),
			Endpoints: []Endpoint{{ID: "e"}},
			Edges:     []Edge{{From: Ref{Stage: "x", Port: "cfg"}, To: Ref{Endpoint: "e"}}}}, "not an output"},
		{"diag port as target", Pipeline{Root: seq(Stage{ID: "x", Ports: []Port{{Name: "err", Class: PortDiagnostic}}}),
			Endpoints: []Endpoint{{ID: "e"}},
			Edges:     []Edge{{From: Ref{Endpoint: "e"}, To: Ref{Stage: "x", Port: "err"}}}}, "not an input"},
		{"endpoint to endpoint", Pipeline{Root: seq(Stage{ID: "x"}),
			Endpoints: []Endpoint{{ID: "e"}, {ID: "f"}},
			Edges:     []Edge{{From: Ref{Endpoint: "e"}, To: Ref{Endpoint: "f"}}}}, "endpoint-to-endpoint"},
		{"named stage to stage", Pipeline{Root: seq(
			Stage{ID: "x", Ports: []Port{{Name: "out", Class: PortArtifact}}},
			Stage{ID: "y", Ports: []Port{{Name: "in", Class: PortConfig}}}),
			Edges: []Edge{{From: Ref{Stage: "x", Port: "out"}, To: Ref{Stage: "y", Port: "in"}}}}, "not supported"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate() = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestComputeRejectsUnreferencedEndpoint(t *testing.T) {
	p := Pipeline{
		Root:      Group{Children: []Element{Stage{ID: "a"}}},
		Endpoints: []Endpoint{{ID: "orphan"}},
	}
	if _, err := Compute(p, LayoutOpts{}); err == nil || !strings.Contains(err.Error(), "not referenced") {
		t.Errorf("Compute() = %v, want unreferenced-endpoint error", err)
	}
}

func TestToGraphModel(t *testing.T) {
	p := testPipeline()
	m := p.ToGraphModel()
	if len(m.Nodes) != 10 { // 5 stages + 5 endpoints
		t.Errorf("ToGraphModel nodes = %d, want 10", len(m.Nodes))
	}
	// 5 implied spine pairs + 7 explicit edges.
	if len(m.Edges) != 12 {
		t.Errorf("ToGraphModel edges = %d, want 12", len(m.Edges))
	}
}

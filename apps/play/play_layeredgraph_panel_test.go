package play

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flakyExecutor errors on its first failUntil calls, then returns a record —
// the wrong-endpoint-then-corrected sequence.
type flakyExecutor struct {
	mu        sync.Mutex
	calls     int
	failUntil int
}

func (inst *flakyExecutor) execute(ctx context.Context, c compiledNode, alloc memory.Allocator) (rec arrow.RecordBatch, schema *arrow.Schema, summary Summary, err error) {
	inst.mu.Lock()
	inst.calls++
	n := inst.calls
	inst.mu.Unlock()
	if n <= inst.failUntil {
		err = eh.Errorf("simulated endpoint failure (call %d)", n)
		return
	}
	rec = int64Rec("n", 1)
	schema = rec.Schema()
	return
}

func (inst *flakyExecutor) callCount() int {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.calls
}

// The endpoint-switch bug (ADR-0129): a demand against a bad endpoint memoises
// the error keyed on the SQL; a re-Run leaves the SQL unchanged, so without
// forgetLanes the demand memo-hits the stored error and the graph never
// recovers though the main result does. forgetLanes (the executeRun Run hook)
// clears the memo so the re-Run re-executes.
func TestNetworkForgetLanesRecoversFromError(t *testing.T) {
	exec := &flakyExecutor{failUntil: 1}
	d := &NetworkDriver{edgesLane: newNodeLane(exec, memory.NewGoAllocator(), 0)}
	defer d.edgesLane.close()

	cn := compiledNode{SQL: "SELECT a AS source, b AS target FROM edges"}

	// First demand → the error lands and is memoised.
	d.edgesLane.demand(cn)
	require.Eventually(t, func() bool {
		v := d.edgesLane.demand(cn)
		if v.rec != nil {
			v.rec.Release()
		}
		return !v.loading && v.err != nil
	}, 2*time.Second, time.Millisecond, "the first demand memoises the error")

	// A same-SQL re-demand memo-hits the stored error — no retry (the bug).
	before := exec.callCount()
	v := d.edgesLane.demand(cn)
	if v.rec != nil {
		v.rec.Release()
	}
	require.Equal(t, before, exec.callCount(), "same SQL memo-hits the stored error without re-executing")

	// forgetLanes clears the memo → the next demand re-executes → success.
	d.forgetLanes()
	require.Eventually(t, func() bool {
		v := d.edgesLane.demand(cn)
		ok := !v.loading && v.err == nil && v.rec != nil
		if v.rec != nil {
			v.rec.Release()
		}
		return ok
	}, 2*time.Second, time.Millisecond, "forgetLanes makes the re-Run re-execute and recover")
	require.Greater(t, exec.callCount(), before, "forgetLanes forced a re-execution")
}

// ADR-0129 §Validation: the by-name column contract (§SD2), endpoint inference
// and synthesis (§SD1/§SD2), vertex de-duplication, parallel-edge collapse, and
// the scale cap (§SD5) — all exercised on the pure row→GraphModel mapping,
// without the widget harness (as the kanban fold tests do).

func netStrArr(t *testing.T, vs []string) arrow.Array {
	t.Helper()
	b := array.NewStringBuilder(memory.NewGoAllocator())
	defer b.Release()
	b.AppendValues(vs, nil)
	return b.NewStringArray()
}

// netEdges builds an edges record: source + target (+ label when non-nil).
func netEdges(t *testing.T, src, tgt, label []string) arrow.RecordBatch {
	t.Helper()
	fields := []arrow.Field{strField("source"), strField("target")}
	cols := []arrow.Array{netStrArr(t, src), netStrArr(t, tgt)}
	if label != nil {
		fields = append(fields, strField("label"))
		cols = append(cols, netStrArr(t, label))
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(len(src)))
}

// netVerts builds a vertices record: id (+ label/group/shape when non-nil).
func netVerts(t *testing.T, id, label, group, shape []string) arrow.RecordBatch {
	t.Helper()
	fields := []arrow.Field{strField("id")}
	cols := []arrow.Array{netStrArr(t, id)}
	add := func(name string, vs []string) {
		if vs != nil {
			fields = append(fields, strField(name))
			cols = append(cols, netStrArr(t, vs))
		}
	}
	add("label", label)
	add("group", group)
	add("shape", shape)
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(len(id)))
}

func nodesByID(m layeredgraph.GraphModel) map[string]layeredgraph.Node {
	out := make(map[string]layeredgraph.Node, len(m.Nodes))
	for _, n := range m.Nodes {
		out[n.ID] = n
	}
	return out
}

func edgeSet(m layeredgraph.GraphModel) map[[2]string]string {
	out := make(map[[2]string]string, len(m.Edges))
	for _, e := range m.Edges {
		out[[2]string{e.From, e.To}] = e.Label
	}
	return out
}

// noVerts is the zero vertices claim (no vertices CTE): idCol -1 disables the
// vertex pass, so buildNetworkModel infers nodes from the edge endpoints.
func noVerts() networkVerticesClaim {
	return networkVerticesClaim{idCol: -1, labelCol: -1, groupCol: -1, shapeCol: -1}
}

func TestNetworkAcceptEdgesContract(t *testing.T) {
	p := layeredGraphPanel{driver: NewNetworkDriver(nil, nil)}

	_, reason := p.AcceptForChannel(chEdges, nil, sigNone())
	assert.NotEmpty(t, reason, "nil schema is rejected")

	// A missing endpoint names itself and the reject teaches the contract.
	_, reason = p.AcceptForChannel(chEdges, arrow.NewSchema([]arrow.Field{strField("a"), strField("target")}, nil), sigNone())
	require.NotEmpty(t, reason)
	assert.Contains(t, reason, "`source`")
	assert.NotContains(t, reason, "`target`", "only the missing column is named")
	assert.Contains(t, reason, "AS source", "the reject shows a query that satisfies it")

	claim, reason := p.AcceptForChannel(chEdges, arrow.NewSchema([]arrow.Field{strField("source"), strField("target"), strField("label")}, nil), sigNone())
	require.Empty(t, reason)
	ec := claim.(networkEdgesClaim)
	assert.Equal(t, 0, ec.srcCol)
	assert.Equal(t, 1, ec.tgtCol)
	assert.Equal(t, 2, ec.labelCol)
}

func TestNetworkAcceptVerticesContract(t *testing.T) {
	p := layeredGraphPanel{driver: NewNetworkDriver(nil, nil)}

	// The vertices channel needs only an id; missing it rejects (and, being
	// optional, the panel then draws from the edges alone).
	_, reason := p.AcceptForChannel(chVertices, arrow.NewSchema([]arrow.Field{strField("label")}, nil), sigNone())
	assert.Contains(t, reason, "`id`")

	claim, reason := p.AcceptForChannel(chVertices,
		arrow.NewSchema([]arrow.Field{strField("id"), strField("group"), strField("shape")}, nil), sigNone())
	require.Empty(t, reason)
	vc := claim.(networkVerticesClaim)
	assert.Equal(t, 0, vc.idCol)
	assert.Equal(t, -1, vc.labelCol, "absent optional column is -1")
	assert.Equal(t, 1, vc.groupCol)
	assert.Equal(t, 2, vc.shapeCol)
}

func TestNetworkBuildInfersVerticesFromEdges(t *testing.T) {
	// No vertices CTE: the nodes are the union of the endpoints, each drawn
	// once, id-labelled.
	er := netEdges(t, []string{"a", "b", "a"}, []string{"b", "c", "c"}, nil)
	ec, reason := resolveNetworkEdges(er.Schema())
	require.Empty(t, reason)

	b := buildNetworkModel(er, ec, nil, noVerts())

	nodes := nodesByID(b.model)
	assert.ElementsMatch(t, []string{"a", "b", "c"}, keysOf(nodes))
	assert.Equal(t, "a", nodes["a"].Label, "an inferred node is labelled by its id")
	assert.Empty(t, b.fillOf)
	assert.False(t, b.capped)

	edges := edgeSet(b.model)
	assert.Len(t, edges, 3)
	assert.Contains(t, edges, [2]string{"a", "b"})
	assert.Contains(t, edges, [2]string{"b", "c"})
	assert.Contains(t, edges, [2]string{"a", "c"})
}

func TestNetworkBuildDecoratesVertices(t *testing.T) {
	vr := netVerts(t,
		[]string{"a", "b", "c"},
		[]string{"Alpha", "Beta", "Gamma"},
		[]string{"x", "y", "x"}, // a and c share a group
		[]string{"box", "ellipse", "circle"})
	er := netEdges(t, []string{"a", "b"}, []string{"b", "c"}, []string{"to-b", "to-c"})
	ec, _ := resolveNetworkEdges(er.Schema())
	vc, reason := resolveNetworkVertices(vr.Schema())
	require.Empty(t, reason)

	b := buildNetworkModel(er, ec, vr, vc)
	nodes := nodesByID(b.model)

	assert.Equal(t, "Beta", nodes["b"].Label)
	assert.Equal(t, layeredgraph.NodeShapeEllipse, nodes["b"].Shape)
	assert.Equal(t, layeredgraph.NodeShapeCircle, nodes["c"].Shape)
	assert.Equal(t, layeredgraph.NodeShapeBox, nodes["a"].Shape)

	// group → categorical fill: same group shares a colour, different groups differ.
	assert.Equal(t, b.fillOf["a"], b.fillOf["c"], "same group, same fill")
	assert.NotEqual(t, b.fillOf["a"], b.fillOf["b"], "different group, different fill")

	// edge labels carried through.
	assert.Equal(t, "to-c", edgeSet(b.model)[[2]string{"b", "c"}])
}

func TestNetworkBuildDeDupesVertices(t *testing.T) {
	// Two rows share an id: one node, first row wins the label; the widget's
	// unique-id invariant is upheld by the panel.
	vr := netVerts(t, []string{"a", "a", "b"}, []string{"first", "second", "B"}, nil, nil)
	er := netEdges(t, []string{"a"}, []string{"b"}, nil)
	ec, _ := resolveNetworkEdges(er.Schema())
	vc, _ := resolveNetworkVertices(vr.Schema())

	b := buildNetworkModel(er, ec, vr, vc)
	nodes := nodesByID(b.model)
	assert.Len(t, b.model.Nodes, 2, "the duplicate id collapses to one node")
	assert.Equal(t, "first", nodes["a"].Label, "first row wins")
}

func TestNetworkBuildCollapsesParallelEdges(t *testing.T) {
	// Two rows name the same ordered pair: one arc (the widget's no-multigraph
	// contract), first label wins.
	er := netEdges(t, []string{"a", "a", "b"}, []string{"b", "b", "c"}, []string{"first", "second", "bc"})
	ec, _ := resolveNetworkEdges(er.Schema())

	b := buildNetworkModel(er, ec, nil, noVerts())
	assert.Len(t, b.model.Edges, 2, "the parallel (a,b) edge collapses")
	assert.Equal(t, "first", edgeSet(b.model)[[2]string{"a", "b"}], "first label wins")
}

func TestNetworkBuildSynthesizesMissingEndpoints(t *testing.T) {
	// The vertices CTE names only 'a'; the edge references 'z', which is
	// synthesised so the edge still draws.
	vr := netVerts(t, []string{"a"}, nil, nil, nil)
	er := netEdges(t, []string{"a"}, []string{"z"}, nil)
	ec, _ := resolveNetworkEdges(er.Schema())
	vc, _ := resolveNetworkVertices(vr.Schema())

	b := buildNetworkModel(er, ec, vr, vc)
	nodes := nodesByID(b.model)
	assert.ElementsMatch(t, []string{"a", "z"}, keysOf(nodes))
	assert.Equal(t, "z", nodes["z"].Label, "synthesised node is id-labelled")
	assert.Contains(t, edgeSet(b.model), [2]string{"a", "z"})
}

func TestNetworkBuildCaps(t *testing.T) {
	// Vertex cap: more distinct vertices than the ceiling → capped, drawn count
	// is the cap.
	ids := make([]string, networkMaxVertices+5)
	for i := range ids {
		ids[i] = fmt.Sprintf("v%d", i)
	}
	vr := netVerts(t, ids, nil, nil, nil)
	vc, _ := resolveNetworkVertices(vr.Schema())
	er0 := netEdges(t, []string{}, []string{}, nil)
	ec0, _ := resolveNetworkEdges(er0.Schema())
	b := buildNetworkModel(er0, ec0, vr, vc)
	assert.True(t, b.capped)
	assert.Len(t, b.model.Nodes, networkMaxVertices)

	// Edge cap: more distinct edges than the ceiling among a small node pool
	// (so the vertex cap is not hit first) → capped, drawn count is the cap.
	const pool = 40 // 40*39 = 1560 possible directed pairs > networkMaxEdges
	var src, tgt []string
	for a := 0; a < pool && len(src) < networkMaxEdges+20; a++ {
		for bb := 0; bb < pool && len(src) < networkMaxEdges+20; bb++ {
			if a == bb {
				continue
			}
			src = append(src, fmt.Sprintf("n%d", a))
			tgt = append(tgt, fmt.Sprintf("n%d", bb))
		}
	}
	er := netEdges(t, src, tgt, nil)
	ec, _ := resolveNetworkEdges(er.Schema())
	b = buildNetworkModel(er, ec, nil, noVerts())
	assert.True(t, b.capped)
	assert.Len(t, b.model.Edges, networkMaxEdges)
	assert.LessOrEqual(t, len(b.model.Nodes), pool, "the small node pool stays under the vertex cap")
}

func keysOf(m map[string]layeredgraph.Node) (out []string) {
	for k := range m {
		out = append(out, k)
	}
	return
}

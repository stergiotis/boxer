package play

import (
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sankey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sankeyTestCol is one column of a fixture record: a string column when str is
// non-nil, a float64 one otherwise. The two cover the contract — ids and tones
// are read as text, values and stages as numbers — and the numeric-as-text case
// gets its own test.
type sankeyTestCol struct {
	name string
	str  []string
	num  []float64
}

func sankeyTestRec(t *testing.T, cols ...sankeyTestCol) arrow.RecordBatch {
	t.Helper()
	mem := memory.NewGoAllocator()
	fields := make([]arrow.Field, 0, len(cols))
	arrs := make([]arrow.Array, 0, len(cols))
	rows := 0
	for _, col := range cols {
		if col.str != nil {
			fields = append(fields, arrow.Field{Name: col.name, Type: arrow.BinaryTypes.String})
			b := array.NewStringBuilder(mem)
			b.AppendValues(col.str, nil)
			arrs = append(arrs, b.NewArray())
			b.Release()
			rows = len(col.str)
			continue
		}
		fields = append(fields, arrow.Field{Name: col.name, Type: arrow.PrimitiveTypes.Float64})
		b := array.NewFloat64Builder(mem)
		b.AppendValues(col.num, nil)
		arrs = append(arrs, b.NewArray())
		b.Release()
		rows = len(col.num)
	}
	rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), arrs, int64(rows))
	for _, a := range arrs {
		a.Release()
	}
	return rec
}

// flowsFixture is the canonical two-stage flow table.
func flowsFixture(t *testing.T) (rec arrow.RecordBatch, fc sankeyFlowsClaim) {
	t.Helper()
	rec = sankeyTestRec(t,
		sankeyTestCol{name: "source", str: []string{"a", "a", "b"}},
		sankeyTestCol{name: "target", str: []string{"c", "d", "c"}},
		sankeyTestCol{name: "value", num: []float64{3, 1, 2}},
	)
	fc, reason := resolveSankeyFlows(rec.Schema())
	require.Empty(t, reason)
	return rec, fc
}

func noNodesClaim() sankeyNodesClaim {
	return sankeyNodesClaim{idCol: -1, labelCol: -1, stageCol: -1, orderCol: -1, groupCol: -1, toneCol: -1}
}

// The flow contract is by name and names every missing column, since the reason
// is the pane's whole empty state.
func TestResolveSankeyFlows(t *testing.T) {
	rec, fc := flowsFixture(t)
	defer rec.Release()
	assert.Equal(t, 0, fc.srcCol)
	assert.Equal(t, 1, fc.tgtCol)
	assert.Equal(t, 2, fc.valCol)
	assert.Equal(t, -1, fc.labelCol, "absent optional column is -1")
	assert.Equal(t, -1, fc.toneCol)

	partial := sankeyTestRec(t, sankeyTestCol{name: "source", str: []string{"a"}})
	defer partial.Release()
	_, reason := resolveSankeyFlows(partial.Schema())
	assert.Contains(t, reason, "`target`")
	assert.Contains(t, reason, "`value`")
	assert.NotContains(t, reason, "a `source`", "a column that IS present is not asked for")

	// A value column alone is not a flow table either — the reject has to hold
	// for every missing member, not just the pair the Network panel shares.
	noVal := sankeyTestRec(t,
		sankeyTestCol{name: "source", str: []string{"a"}},
		sankeyTestCol{name: "target", str: []string{"b"}},
	)
	defer noVal.Release()
	_, reason = resolveSankeyFlows(noVal.Schema())
	assert.Contains(t, reason, "`value`")
}

// The node contract needs only `id`; everything else decorates.
func TestResolveSankeyNodes(t *testing.T) {
	rec := sankeyTestRec(t,
		sankeyTestCol{name: "id", str: []string{"a"}},
		sankeyTestCol{name: "label", str: []string{"A"}},
		sankeyTestCol{name: "stage", num: []float64{0}},
		sankeyTestCol{name: "order", num: []float64{2}},
		sankeyTestCol{name: "group", str: []string{"g"}},
		sankeyTestCol{name: "tone", str: []string{"warning"}},
	)
	defer rec.Release()
	nc, reason := resolveSankeyNodes(rec.Schema())
	require.Empty(t, reason)
	assert.Equal(t, 0, nc.idCol)
	assert.Equal(t, 1, nc.labelCol)
	assert.Equal(t, 2, nc.stageCol)
	assert.Equal(t, 3, nc.orderCol)
	assert.Equal(t, 4, nc.groupCol)
	assert.Equal(t, 5, nc.toneCol)

	idless := sankeyTestRec(t, sankeyTestCol{name: "label", str: []string{"A"}})
	defer idless.Release()
	_, reason = resolveSankeyNodes(idless.Schema())
	assert.Contains(t, reason, "`id`")
}

// Endpoint synthesis: with no `nodes` CTE the vertex set comes from the flows,
// each node labelled by its own id.
func TestBuildSankeyDiagramSynthesizesEndpoints(t *testing.T) {
	rec, fc := flowsFixture(t)
	defer rec.Release()

	b := buildSankeyDiagram(rec, fc, nil, noNodesClaim(), sankeyChoiceAuto)
	require.Len(t, b.diagram.Nodes, 4)
	require.Len(t, b.diagram.Links, 3)
	ids := make([]string, 0, 4)
	for _, n := range b.diagram.Nodes {
		ids = append(ids, n.ID)
		assert.Equal(t, n.ID, n.Label, "a synthesised node labels itself")
	}
	assert.Equal(t, []string{"a", "c", "d", "b"}, ids, "insertion order is row order, so the build is deterministic")
	assert.False(t, b.stats.stagesGiven, "synthesised nodes carry no stage")
	assert.Equal(t, sankey.ModeSankey, b.diagram.Mode)
	assert.NoError(t, b.diagram.Validate())
}

// Duplicate (source,target) rows are SUMMED, not dropped and not drawn twice:
// two rows carrying a quantity are two quantities, and keeping only the first
// would understate the total the diagram is scaled against.
func TestBuildSankeyDiagramSumsDuplicateFlows(t *testing.T) {
	rec := sankeyTestRec(t,
		sankeyTestCol{name: "source", str: []string{"a", "a", "a"}},
		sankeyTestCol{name: "target", str: []string{"b", "b", "c"}},
		sankeyTestCol{name: "value", num: []float64{2, 5, 1}},
	)
	defer rec.Release()
	fc, reason := resolveSankeyFlows(rec.Schema())
	require.Empty(t, reason)

	b := buildSankeyDiagram(rec, fc, nil, noNodesClaim(), sankeyChoiceAuto)
	require.Len(t, b.diagram.Links, 2)
	assert.Equal(t, float64(7), b.diagram.Links[0].Value, "the a→b rows are summed")
	assert.Equal(t, float64(1), b.diagram.Links[1].Value)
	assert.Equal(t, 1, b.stats.collapsed)
}

// Rows a stage-ordered diagram cannot draw are dropped and counted rather than
// failing the whole diagram — Validate would reject the lot over any one of
// them.
func TestBuildSankeyDiagramDropsUndrawableRows(t *testing.T) {
	rec := sankeyTestRec(t,
		sankeyTestCol{name: "source", str: []string{"a", "b", "c", "", "d"}},
		sankeyTestCol{name: "target", str: []string{"a", "x", "y", "z", "e"}},
		sankeyTestCol{name: "value", num: []float64{5, 0, -3, 1, 4}},
	)
	defer rec.Release()
	fc, reason := resolveSankeyFlows(rec.Schema())
	require.Empty(t, reason)

	b := buildSankeyDiagram(rec, fc, nil, noNodesClaim(), sankeyChoiceAuto)
	require.Len(t, b.diagram.Links, 1, "only d→e survives")
	assert.Equal(t, "d", b.diagram.Links[0].Source)
	assert.Equal(t, 1, b.stats.droppedSelf, "a→a")
	assert.Equal(t, 3, b.stats.droppedValue, "zero, negative, and the missing endpoint")
	assert.NoError(t, b.diagram.Validate())
}

// Node decoration: label, stage, order and the colour precedence (tone over
// group over the widget's own palette).
func TestBuildSankeyDiagramDecoratesNodes(t *testing.T) {
	flows, fc := flowsFixture(t)
	defer flows.Release()
	nodes := sankeyTestRec(t,
		sankeyTestCol{name: "id", str: []string{"a", "b", "c", "d"}},
		sankeyTestCol{name: "label", str: []string{"Alpha", "", "Gamma", "Delta"}},
		sankeyTestCol{name: "stage", num: []float64{0, 0, 1, 1}},
		sankeyTestCol{name: "order", num: []float64{1, 2, 1, 2}},
		sankeyTestCol{name: "group", str: []string{"g1", "g2", "g1", ""}},
		sankeyTestCol{name: "tone", str: []string{"", "", "error", "nonsense"}},
	)
	defer nodes.Release()
	nc, reason := resolveSankeyNodes(nodes.Schema())
	require.Empty(t, reason)

	b := buildSankeyDiagram(flows, fc, nodes, nc, sankeyChoiceAuto)
	require.Len(t, b.diagram.Nodes, 4)
	byID := make(map[string]sankey.Node, 4)
	for _, n := range b.diagram.Nodes {
		byID[n.ID] = n
	}
	assert.Equal(t, "Alpha", byID["a"].Label)
	assert.Equal(t, "b", byID["b"].Label, "an empty label falls back to the id")
	assert.Equal(t, 1, byID["c"].Stage)
	assert.Equal(t, float64(2), byID["d"].Order)

	assert.Equal(t, styletokens.QualitativeCycle(0).AsHex(), byID["a"].Color, "first group takes the first hue")
	assert.Equal(t, styletokens.QualitativeCycle(1).AsHex(), byID["b"].Color)
	assert.Equal(t, styletokens.ErrorDefault.AsHex(), byID["c"].Color, "tone wins over the group palette")
	assert.Zero(t, byID["d"].Color, "an unknown tone and no group defers to the widget's own cycle")

	// Every node carries a stage, so the auto mode reads the data as alluvial.
	assert.True(t, b.stats.stagesGiven)
	assert.Equal(t, sankey.ModeAlluvial, b.diagram.Mode)
}

// A `nodes` CTE that covers only part of the graph leaves the synthesised
// endpoints stageless, so the alluvial reading is not offered — the mode has to
// hold for the whole diagram or not at all.
func TestBuildSankeyDiagramPartialStagesStaySankey(t *testing.T) {
	flows, fc := flowsFixture(t)
	defer flows.Release()
	nodes := sankeyTestRec(t,
		sankeyTestCol{name: "id", str: []string{"a", "b"}},
		sankeyTestCol{name: "stage", num: []float64{0, 0}},
	)
	defer nodes.Release()
	nc, reason := resolveSankeyNodes(nodes.Schema())
	require.Empty(t, reason)

	b := buildSankeyDiagram(flows, fc, nodes, nc, sankeyChoiceAuto)
	assert.False(t, b.stats.stagesGiven, "c and d were synthesised without one")
	assert.Equal(t, sankey.ModeSankey, b.diagram.Mode)
}

// The mode control overrides the data's answer in both directions.
func TestSankeyModeChoiceOverrides(t *testing.T) {
	assert.Equal(t, sankey.ModeAlluvial, sankeyMode(sankeyChoiceAuto, true))
	assert.Equal(t, sankey.ModeSankey, sankeyMode(sankeyChoiceAuto, false))
	assert.Equal(t, sankey.ModeSankey, sankeyMode(sankeyChoiceSankey, true), "a staged diagram can still be read as a Sankey")
	assert.Equal(t, sankey.ModeAlluvial, sankeyMode(sankeyChoiceAlluvial, false),
		"a forced alluvial choice is honoured so Compute can report why it cannot be drawn")
}

// Alluvial requires adjacent stages; a diagram whose stages are given but whose
// links skip one falls back to the Sankey reading and says which link proved
// it, rather than rendering nothing.
func TestComputeSankeyLayoutFallsBackFromAlluvial(t *testing.T) {
	d := sankey.Diagram{
		Mode: sankey.ModeAlluvial,
		Nodes: []sankey.Node{
			{ID: "a", Stage: 0}, {ID: "b", Stage: 1}, {ID: "c", Stage: 2},
		},
		Links: []sankey.Link{
			{Source: "a", Target: "b", Value: 1},
			{Source: "a", Target: "c", Value: 2}, // spans two stages
		},
	}
	lay, used, fallback, err := computeSankeyLayout(d)
	require.NoError(t, err)
	require.NotNil(t, lay)
	assert.Equal(t, sankey.ModeSankey, used)
	assert.Contains(t, fallback, "adjacent stages")
	assert.NotContains(t, fallback, "sankey: ", "the package prefix is trimmed for the status line")

	// A well-formed alluvial diagram is laid out as asked, with nothing to say.
	d.Links = []sankey.Link{{Source: "a", Target: "b", Value: 1}, {Source: "b", Target: "c", Value: 1}}
	lay, used, fallback, err = computeSankeyLayout(d)
	require.NoError(t, err)
	require.NotNil(t, lay)
	assert.Equal(t, sankey.ModeAlluvial, used)
	assert.Empty(t, fallback)
}

// A cycle has nowhere to fall back to: Sankey mode rejects it too, so the error
// reaches the pane.
func TestComputeSankeyLayoutReportsCycle(t *testing.T) {
	d := sankey.Diagram{
		Nodes: []sankey.Node{{ID: "a"}, {ID: "b"}},
		Links: []sankey.Link{{Source: "a", Target: "b", Value: 1}, {Source: "b", Target: "a", Value: 1}},
	}
	lay, _, _, err := computeSankeyLayout(d)
	assert.Nil(t, lay)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

// The layout cache key covers everything the geometry depends on and nothing
// else: recolouring must not re-run the layout, and a changed quantity must.
func TestSankeyDiagramKeyTracksGeometryOnly(t *testing.T) {
	base := sankey.Diagram{
		Nodes: []sankey.Node{{ID: "a"}, {ID: "b"}},
		Links: []sankey.Link{{Source: "a", Target: "b", Value: 1}},
	}
	key := sankeyDiagramKey(base)

	recoloured := sankey.Diagram{
		Nodes: []sankey.Node{{ID: "a", Color: 0xff0000ff}, {ID: "b"}},
		Links: []sankey.Link{{Source: "a", Target: "b", Value: 1, Color: 0x00ff00ff, Label: "x"}},
	}
	assert.Equal(t, key, sankeyDiagramKey(recoloured), "colour and link label are draw-time only")

	for _, changed := range []sankey.Diagram{
		{Nodes: base.Nodes, Links: []sankey.Link{{Source: "a", Target: "b", Value: 2}}},
		{Nodes: []sankey.Node{{ID: "a", Stage: 1}, {ID: "b"}}, Links: base.Links},
		{Nodes: []sankey.Node{{ID: "a", Order: 3}, {ID: "b"}}, Links: base.Links},
		{Nodes: base.Nodes, Links: base.Links, Mode: sankey.ModeAlluvial},
	} {
		assert.NotEqual(t, key, sankeyDiagramKey(changed), "geometry input %v must re-key", changed)
	}
}

// The value column is read through the numeric arrays first and the formatted
// cell second, so a quantity that arrives as text — which is how ClickHouse
// Decimal reaches Arrow here — still draws instead of dropping every row.
func TestSankeyCellValueReadsTextualNumbers(t *testing.T) {
	rec := sankeyTestRec(t,
		sankeyTestCol{name: "source", str: []string{"a", "b"}},
		sankeyTestCol{name: "target", str: []string{"b", "c"}},
		sankeyTestCol{name: "value", str: []string{"12.5", "not a number"}},
	)
	defer rec.Release()
	fc, reason := resolveSankeyFlows(rec.Schema())
	require.Empty(t, reason)

	b := buildSankeyDiagram(rec, fc, nil, noNodesClaim(), sankeyChoiceAuto)
	require.Len(t, b.diagram.Links, 1)
	assert.Equal(t, 12.5, b.diagram.Links[0].Value)
	assert.Equal(t, 1, b.stats.droppedValue, "the unparseable row is dropped and counted")
}

// The node cap drops flows whose endpoints cannot be added rather than leaving
// a link pointing at a node the diagram does not contain — which Validate would
// reject, taking the whole drawing down with it.
func TestBuildSankeyDiagramCapKeepsDiagramValid(t *testing.T) {
	n := sankeyMaxNodes + 20
	src := make([]string, 0, n)
	tgt := make([]string, 0, n)
	val := make([]float64, 0, n)
	for i := range n {
		src = append(src, "s"+strconv.Itoa(i))
		tgt = append(tgt, "t"+strconv.Itoa(i))
		val = append(val, 1)
	}
	rec := sankeyTestRec(t,
		sankeyTestCol{name: "source", str: src},
		sankeyTestCol{name: "target", str: tgt},
		sankeyTestCol{name: "value", num: val},
	)
	defer rec.Release()
	fc, reason := resolveSankeyFlows(rec.Schema())
	require.Empty(t, reason)

	b := buildSankeyDiagram(rec, fc, nil, noNodesClaim(), sankeyChoiceAuto)
	assert.True(t, b.stats.capped)
	assert.LessOrEqual(t, len(b.diagram.Nodes), sankeyMaxNodes)
	assert.NoError(t, b.diagram.Validate(), "a capped diagram is still a valid one")
}

// sankeySnippetSQL is the shape the help corpus teaches: the two contract CTEs
// alongside a third they both build on, and a final SELECT of the user's own.
const sankeySnippetSQL = `WITH
  paths AS (
    SELECT [a.status, r.lang, splitByChar('/', r.path)[1]] AS p
    FROM coderef AS r
    INNER JOIN adr AS a ON a.num = r.num
    WHERE r.lang != '' AND r.path != ''
  ),
  flows AS (
    SELECT e.1 AS source, e.2 AS target, count() AS value
    FROM (
      SELECT arrayJoin(arrayMap(
               i -> (concat(toString(i - 1), ':', p[i]), concat(toString(i), ':', p[i + 1])),
               range(1, length(p)))) AS e
      FROM paths
    )
    GROUP BY source, target
  ),
  nodes AS (
    SELECT concat(toString(x.1), ':', x.2) AS id,
           x.2                             AS label,
           x.1                             AS stage
    FROM (
      SELECT arrayJoin(arrayMap((i, v) -> (i - 1, v), arrayEnumerate(p), p)) AS x
      FROM paths
    )
    GROUP BY id, label, stage
  )
SELECT * FROM flows ORDER BY value DESC`

// Both channels bind to CTEs of the user's own query, so the split has to
// recognise them by name and the fuse has to carry their shared dependency into
// each lane's statement. A `flows` CTE that fuses without `paths` would run as
// SQL and fail at the server, which is the failure this pins down.
func TestSankeySnippetSplitsIntoBothLanes(t *testing.T) {
	res, err := splitGraph(sankeySnippetSQL)
	require.NoError(t, err)

	for _, id := range []NodeID{sankeyFlowsNodeID, sankeyNodesNodeID} {
		node, ok := findSplitNode(res, id)
		require.True(t, ok, "the split must expose %q as a node", id)
		assert.Equal(t, id, node.ID)

		fused := fuseNode(res, id)
		require.NotEmpty(t, fused, "%q must fuse to an executable statement", id)
		assert.Contains(t, fused, "paths AS", "the fuse carries the CTE both contract nodes build on")
	}
}

// The endpoint-switch bug again (ADR-0129): a demand against a bad endpoint
// memoises the error keyed on the SQL, so without the Run hook a re-Run
// memo-hits it and the diagram never recovers though the main result does.
func TestSankeyForgetLanesRecoversFromError(t *testing.T) {
	exec := &flakyExecutor{failUntil: 1}
	d := &SankeyDriver{flowsLane: newNodeLane(exec, memory.NewGoAllocator(), 0)}
	defer d.flowsLane.close()

	cn := compiledNode{SQL: "SELECT a AS source, b AS target, c AS value FROM t"}

	// First demand → the error lands and is memoised.
	d.flowsLane.demand(cn)
	require.Eventually(t, func() bool {
		v := d.flowsLane.demand(cn)
		if v.rec != nil {
			v.rec.Release()
		}
		return !v.loading && v.err != nil
	}, 2*time.Second, time.Millisecond, "the first demand memoises the error")

	// A same-SQL re-demand memo-hits the stored error — no retry (the bug).
	before := exec.callCount()
	v := d.flowsLane.demand(cn)
	if v.rec != nil {
		v.rec.Release()
	}
	require.Equal(t, before, exec.callCount(), "same SQL memo-hits the stored error without re-executing")

	// forgetLanes clears the memo → the next demand re-executes → success.
	d.forgetLanes()
	require.Eventually(t, func() bool {
		v := d.flowsLane.demand(cn)
		ok := !v.loading && v.err == nil && v.rec != nil
		if v.rec != nil {
			v.rec.Release()
		}
		return ok
	}, 2*time.Second, time.Millisecond, "forgetLanes makes the re-Run re-execute and recover")
	require.Greater(t, exec.callCount(), before, "forgetLanes forced a re-execution")
}

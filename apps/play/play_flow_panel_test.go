package play

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// Tests for the Flow tab driver (play_flow_panel.go): the derivation memo and
// the widget-model projection. Headless — ensure/flowToModel never touch the
// UI; render is exercised live (ADR-0153 M4).

func TestFlowDriverMemoStableAcrossFrames(t *testing.T) {
	d := newFlowDriver(nil, nil)
	node := splitNode{ID: "main", SQL: "SELECT a FROM t WHERE a > 0"}
	d.ensure(node)
	require.NoError(t, d.graphErr)
	require.NotEmpty(t, d.graph.Nodes)
	first := &d.graph.Nodes[0]
	d.ensure(node) // same key ⇒ memo hit, no rebuild
	require.Same(t, first, &d.graph.Nodes[0])
}

func TestFlowDriverMemoRebuildOnSQLChange(t *testing.T) {
	d := newFlowDriver(nil, nil)
	d.ensure(splitNode{ID: "main", SQL: "SELECT 1"})
	require.NoError(t, d.graphErr)
	n1 := len(d.graph.Nodes)
	d.ensure(splitNode{ID: "main", SQL: "SELECT a FROM t WHERE a > 0"})
	require.NoError(t, d.graphErr)
	require.NotEqual(t, n1, len(d.graph.Nodes), "SQL change must re-derive")
}

// The sibling set is part of the memo key: deleting a sibling CTE
// re-classifies a source without changing the node's own SQL.
func TestFlowDriverMemoRebuildOnDepsChange(t *testing.T) {
	d := newFlowDriver(nil, nil)
	d.ensure(splitNode{ID: "by_kind", SQL: "SELECT * FROM recent"})
	require.Equal(t, flowSourceTable, d.graph.Nodes[0].Kind)
	d.ensure(splitNode{ID: "by_kind", SQL: "SELECT * FROM recent", DependsOn: []NodeID{"recent"}})
	require.Equal(t, flowSourceCTE, d.graph.Nodes[0].Kind, "deps change must re-derive")
}

func TestFlowDriverSelectionSurvivesAndClears(t *testing.T) {
	d := newFlowDriver(nil, nil)
	d.ensure(splitNode{ID: "main", SQL: "SELECT a FROM t WHERE a > 0"})
	d.selectedID = "q:where"
	d.ensure(splitNode{ID: "main", SQL: "SELECT b FROM u WHERE b > 1"})
	require.Equal(t, "q:where", d.selectedID, "selection survives while the node id exists")
	d.ensure(splitNode{ID: "main", SQL: "SELECT 1"})
	require.Empty(t, d.selectedID, "selection clears when its node vanishes")
}

func TestFlowDriverErrorThenRecover(t *testing.T) {
	d := newFlowDriver(nil, nil)
	d.ensure(splitNode{ID: "main", SQL: "SELECT * FROM"})
	require.Error(t, d.graphErr)
	require.Empty(t, d.graph.Nodes)
	d.ensure(splitNode{ID: "main", SQL: "SELECT 1"})
	require.NoError(t, d.graphErr)
	require.NotEmpty(t, d.graph.Nodes)
}

// The caret picks the node whose body contains it — a CTE body under the
// caret shows that CTE — with the sink as the fallback everywhere else.
func TestCaretFlowNode(t *testing.T) {
	const stmt = "WITH a AS (SELECT 1 AS x), b AS (SELECT 2 AS y) SELECT * FROM a, b"
	res, err := splitGraph(stmt)
	require.NoError(t, err)

	na, ok := findSplitNode(res, NodeID("a"))
	require.True(t, ok)
	require.GreaterOrEqual(t, na.SrcOff, 0)
	require.Equal(t, NodeID("a"), caretFlowNode(res, na.SrcOff+2))

	nb, ok := findSplitNode(res, NodeID("b"))
	require.True(t, ok)
	require.Equal(t, NodeID("b"), caretFlowNode(res, nb.SrcOff+2))

	require.Equal(t, res.Sink, caretFlowNode(res, len(stmt)-3), "the outer SELECT is the sink's")
	require.Equal(t, res.Sink, caretFlowNode(res, -5), "out of range falls back to the sink")
}

func TestFlowDriverEnsureLiveMemo(t *testing.T) {
	d := newFlowDriver(nil, nil)
	d.ensureLive("WITH d AS (SELECT 1 AS x) SELECT * FROM d")
	require.NoError(t, d.liveErr)
	require.Len(t, d.liveSplit.Nodes, 2)
	first := &d.liveSplit.Nodes[0]
	d.ensureLive("WITH d AS (SELECT 1 AS x) SELECT * FROM d")
	require.Same(t, first, &d.liveSplit.Nodes[0], "unchanged text ⇒ no re-split")

	d.ensureLive("SELECT * FROM")
	require.Error(t, d.liveErr)
	require.Empty(t, d.liveSplit.Nodes)

	d.ensureLive("   ")
	require.NoError(t, d.liveErr)
	require.Empty(t, d.liveSplit.Nodes)
}

// flowFeed in caret mode follows the caret across statements and into CTE
// bodies of the CURRENT buffer; run mode keeps the last Run's split.
func TestFlowFeedCaretMode(t *testing.T) {
	inst := tabsTestApp()
	inst.sql = "SELECT 7 AS q;\nWITH d AS (SELECT 1 AS x) SELECT * FROM d"
	inst.flow.srcMode = flowSrcCaret

	inst.caretByte = 3 // inside the first statement
	split, active, srcErr := inst.flowFeed()
	require.NoError(t, srcErr)
	require.Equal(t, split.Sink, active)
	sink, ok := findSplitNode(split, active)
	require.True(t, ok)
	require.Equal(t, "SELECT 7 AS q", sink.SQL)

	// Inside d's body in the second statement.
	inst.caretByte = strings.Index(inst.sql, "SELECT 1 AS x") + 3
	split, active, srcErr = inst.flowFeed()
	require.NoError(t, srcErr)
	require.Equal(t, NodeID("d"), active)
	require.Len(t, split.Nodes, 2)

	// Run mode is untouched by the caret.
	inst.flow.srcMode = flowSrcRun
	_, active, _ = inst.flowFeed()
	require.Equal(t, inst.currentSplit.Sink, active)
}

func TestFlowToModelShapes(t *testing.T) {
	g := flowGraph{
		Nodes: []flowNode{
			{ID: "a", Kind: flowSourceTable, Label: "t"},
			{ID: "b", Kind: flowUnion, Label: "UNION ALL"},
			{ID: "c", Kind: flowResult, Label: "main"},
		},
		Edges: []flowEdge{{From: "a", To: "b", Label: "l"}, {From: "b", To: "c"}},
	}
	m := flowToModel(g)
	require.Equal(t, layeredgraph.NodeShapeBox, m.Nodes[0].Shape)
	require.Equal(t, layeredgraph.NodeShapeEllipse, m.Nodes[1].Shape)
	require.Equal(t, layeredgraph.NodeShapeEllipse, m.Nodes[2].Shape)
	require.Equal(t, layeredgraph.Edge{From: "a", To: "b", Label: "l"}, m.Edges[0])
	require.Equal(t, layeredgraph.Edge{From: "b", To: "c"}, m.Edges[1])
}

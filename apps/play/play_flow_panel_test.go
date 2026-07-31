package play

import (
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

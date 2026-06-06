package goccyengine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
)

// trafficLight is a small directed FSM with a cycle and a self-loop — the
// canonical directed-flow graph the layered engine targets.
func trafficLight() layeredgraph.GraphModel {
	return layeredgraph.GraphModel{
		Nodes: []layeredgraph.Node{
			{ID: "red", Label: "Red"},
			{ID: "green", Label: "Green"},
			{ID: "yellow", Label: "Yellow"},
		},
		Edges: []layeredgraph.Edge{
			{From: "red", To: "green", Label: "go"},
			{From: "green", To: "yellow", Label: "caution"},
			{From: "yellow", To: "red", Label: "stop"},
			{From: "red", To: "red", Label: "wait"}, // self-loop
		},
	}
}

func newEngine(t *testing.T) *goccyengine.Engine {
	t.Helper()
	e, err := goccyengine.New(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func TestLayout_TrafficLightFSM(t *testing.T) {
	e := newEngine(t)
	m := trafficLight()

	lay, err := e.Layout(context.Background(), m, layeredgraph.LayoutOpts{})
	require.NoError(t, err)
	require.NotNil(t, lay)

	// Overall bounding box is real.
	assert.Greater(t, lay.Width, 0.0)
	assert.Greater(t, lay.Height, 0.0)

	// Every node is placed, sized, and inside the bounding box.
	require.Len(t, lay.Nodes, 3)
	byID := map[string]layeredgraph.NodeLayout{}
	for _, n := range lay.Nodes {
		byID[n.ID] = n
		assert.Greater(t, n.W, 0.0, "node %s width", n.ID)
		assert.Greater(t, n.H, 0.0, "node %s height", n.ID)
		assert.GreaterOrEqual(t, n.Center.X, 0.0, "node %s x in bounds", n.ID)
		assert.LessOrEqual(t, n.Center.X, lay.Width, "node %s x in bounds", n.ID)
		assert.GreaterOrEqual(t, n.Center.Y, 0.0, "node %s y in bounds", n.ID)
		assert.LessOrEqual(t, n.Center.Y, lay.Height, "node %s y in bounds", n.ID)
	}
	for _, id := range []string{"red", "green", "yellow"} {
		_, ok := byID[id]
		assert.Truef(t, ok, "node %q present", id)
	}
	assert.Equal(t, "Red", byID["red"].Label, "label carried from model")

	// All four edges (incl. the self-loop) routed, with a spline and a head.
	require.Len(t, lay.Edges, 4)
	for _, ed := range lay.Edges {
		assert.NotEmpty(t, ed.Points, "edge %s->%s has a spline", ed.From, ed.To)
		assert.NotNilf(t, ed.ArrowHead, "edge %s->%s has an arrow head", ed.From, ed.To)
	}
}

// Graphviz `dot` is deterministic and goccy pins the embedded version, so the
// same input must produce byte-identical geometry — the property that lets the
// screenshot tour drop NonDeterministic (ADR-0069).
func TestLayout_Deterministic(t *testing.T) {
	e := newEngine(t)
	m := trafficLight()

	a, err := e.Layout(context.Background(), m, layeredgraph.LayoutOpts{})
	require.NoError(t, err)
	b, err := e.Layout(context.Background(), m, layeredgraph.LayoutOpts{})
	require.NoError(t, err)

	require.Equal(t, a, b, "layout must be reproducible for identical input")
}

// RankDir must actually steer the flow axis: a 3-node chain is tall under
// top-bottom and wide under left-right.
func TestLayout_RankDir(t *testing.T) {
	e := newEngine(t)
	chain := layeredgraph.GraphModel{
		Nodes: []layeredgraph.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		Edges: []layeredgraph.Edge{{From: "a", To: "b"}, {From: "b", To: "c"}},
	}

	tb, err := e.Layout(context.Background(), chain, layeredgraph.LayoutOpts{RankDir: layeredgraph.RankDirTopBottom})
	require.NoError(t, err)
	lr, err := e.Layout(context.Background(), chain, layeredgraph.LayoutOpts{RankDir: layeredgraph.RankDirLeftRight})
	require.NoError(t, err)

	assert.Greater(t, tb.Height, tb.Width, "top-bottom chain is taller than wide")
	assert.Greater(t, lr.Width, lr.Height, "left-right chain is wider than tall")
}

func TestLayout_UnknownEdgeNodeErrors(t *testing.T) {
	e := newEngine(t)
	m := layeredgraph.GraphModel{
		Nodes: []layeredgraph.Node{{ID: "a"}},
		Edges: []layeredgraph.Edge{{From: "a", To: "ghost"}},
	}
	_, err := e.Layout(context.Background(), m, layeredgraph.LayoutOpts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost")
}

// Nodes that share an ID (e.g. fsmview states with the same label) must merge
// into one node, not fail the whole layout — see the code-review #1 regression.
func TestLayout_DuplicateNodeIDMerges(t *testing.T) {
	e := newEngine(t)
	m := layeredgraph.GraphModel{
		Nodes: []layeredgraph.Node{
			{ID: "x", Label: "X"},
			{ID: "x", Label: "X again"},
			{ID: "y", Label: "Y"},
		},
		Edges: []layeredgraph.Edge{{From: "x", To: "y"}},
	}
	lay, err := e.Layout(context.Background(), m, layeredgraph.LayoutOpts{})
	require.NoError(t, err)
	require.NotNil(t, lay)
	require.Len(t, lay.Nodes, 2, "duplicate id collapses to one node")
	ids := map[string]bool{}
	for _, n := range lay.Nodes {
		ids[n.ID] = true
	}
	assert.True(t, ids["x"] && ids["y"], "both distinct ids present")
}

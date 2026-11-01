//go:build !(js || wasip1)

package goccyengine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// unevenPaths is two paths of different length into one sink: a→x→sink and
// b→y1→y2→sink. Left to itself the engine ranks a and x as late as their
// edges allow, so a sits one layer after b; pinned, a sits beside b and x
// beside y2.
func unevenPaths(pin bool) layeredgraph.GraphModel {
	rank := func(r int) int {
		if pin {
			return r
		}
		return 0
	}
	return layeredgraph.GraphModel{
		Nodes: []layeredgraph.Node{
			{ID: "a", Rank: rank(1)},
			{ID: "x", Rank: rank(3)},
			{ID: "b", Rank: rank(1)},
			{ID: "y1", Rank: rank(2)},
			{ID: "y2", Rank: rank(3)},
			{ID: "sink", Rank: rank(4)},
		},
		Edges: []layeredgraph.Edge{
			{From: "a", To: "x"},
			{From: "x", To: "sink"},
			{From: "b", To: "y1"},
			{From: "y1", To: "y2"},
			{From: "y2", To: "sink"},
		},
	}
}

func centres(lay *layeredgraph.Layout) map[string]layeredgraph.Point {
	out := make(map[string]layeredgraph.Point, len(lay.Nodes))
	for _, n := range lay.Nodes {
		out[n.ID] = n.Center
	}
	return out
}

func TestLayout_RankPinsLayers(t *testing.T) {
	e := newEngine(t)

	// Unpinned: a is ranked as late as its edge to x allows, so it does not
	// share b's layer — which is what makes the pins observable at all.
	free, err := e.Layout(context.Background(), unevenPaths(false), layeredgraph.LayoutOpts{RankDir: layeredgraph.RankDirLeftRight})
	require.NoError(t, err)
	fc := centres(free)
	require.NotEqual(t, fc["a"].X, fc["b"].X, "the unpinned control must differ, or the test proves nothing")

	lay, err := e.Layout(context.Background(), unevenPaths(true), layeredgraph.LayoutOpts{RankDir: layeredgraph.RankDirLeftRight})
	require.NoError(t, err)
	pc := centres(lay)
	// Left-right ranks: a layer is a column, so a shared Rank is a shared X.
	assert.InDelta(t, pc["a"].X, pc["b"].X, 0.5, "same Rank, same layer")
	assert.InDelta(t, pc["x"].X, pc["y2"].X, 0.5, "same Rank, same layer")
	assert.Less(t, pc["a"].X, pc["y1"].X)
	assert.Less(t, pc["y1"].X, pc["y2"].X)
	assert.Less(t, pc["y2"].X, pc["sink"].X)
	// a→x spans two layers: x is pinned past y1's layer, not next to a.
	assert.Greater(t, pc["x"].X, pc["y1"].X)
}

func TestLayout_RankIsRelative(t *testing.T) {
	e := newEngine(t)
	m := unevenPaths(true)
	shifted := unevenPaths(true)
	for i := range shifted.Nodes {
		shifted.Nodes[i].Rank += 10
	}
	a, err := e.Layout(context.Background(), m, layeredgraph.LayoutOpts{})
	require.NoError(t, err)
	b, err := e.Layout(context.Background(), shifted, layeredgraph.LayoutOpts{})
	require.NoError(t, err)
	assert.Equal(t, centres(a), centres(b), "only Rank differences count")
}

func TestLayout_BackwardEdgeBetweenPinsKeepsThePins(t *testing.T) {
	e := newEngine(t)
	m := unevenPaths(true)
	// sink (Rank 4) → a (Rank 1) runs against the pins; it must neither fail
	// nor pull a out of its layer.
	m.Edges = append(m.Edges, layeredgraph.Edge{From: "sink", To: "a"})
	lay, err := e.Layout(context.Background(), m, layeredgraph.LayoutOpts{})
	require.NoError(t, err)
	require.Len(t, lay.Edges, len(m.Edges))
	pc := centres(lay)
	// Top-bottom ranks: a layer is a row.
	assert.InDelta(t, pc["a"].Y, pc["b"].Y, 0.5)
	assert.Less(t, pc["a"].Y, pc["sink"].Y)
}

func TestLayout_UnpinnedNodesBesidePinnedOnes(t *testing.T) {
	e := newEngine(t)
	m := unevenPaths(true)
	// A free node hanging off a pinned one is placed by the engine as usual.
	m.Nodes = append(m.Nodes, layeredgraph.Node{ID: "free"})
	m.Edges = append(m.Edges, layeredgraph.Edge{From: "y1", To: "free"})
	lay, err := e.Layout(context.Background(), m, layeredgraph.LayoutOpts{})
	require.NoError(t, err)
	pc := centres(lay)
	assert.Greater(t, pc["free"].Y, pc["y1"].Y)
	assert.InDelta(t, pc["a"].Y, pc["b"].Y, 0.5)
}

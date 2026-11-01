package bowtie

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
)

func prototypeModel(t *testing.T) *Model {
	t.Helper()
	m, err := BuildModel(prototypeRows())
	require.NoError(t, err)
	return &m
}

func rankOf(g diagramGraph) map[string]int {
	out := make(map[string]int, len(g.model.Nodes))
	for _, n := range g.model.Nodes {
		out[n.ID] = n.Rank
	}
	return out
}

func TestGraphPinsTheBowTieLayers(t *testing.T) {
	g := buildGraph(prototypeModel(t))
	r := rankOf(g)
	// threats | 2 prevention columns | top | 2 mitigation columns | consequences
	assert.Equal(t, 1, r["t0"])
	assert.Equal(t, 1, r["t1"])
	assert.Equal(t, 4, r["top"])
	assert.Equal(t, 7, r["c0"])
	assert.Equal(t, 7, r["c2"])
	// A two-barrier threat fills both columns, in step order.
	assert.Equal(t, 2, r["t0b0"])
	assert.Equal(t, 3, r["t0b1"])
	// A one-barrier threat keeps its barrier next to the top event.
	assert.Equal(t, 3, r["t1b0"])
	// The right half mirrors: a one-barrier consequence's barrier is next to
	// the top event too.
	assert.Equal(t, 5, r["c0b0"])
	assert.Equal(t, 6, r["c0b1"])
	assert.Equal(t, 5, r["c1b0"])

	assert.Len(t, g.model.Nodes, 1+3+3+9)
	// Every path is one chain: threat→…→top or top→…→consequence.
	assert.Len(t, g.model.Edges, 3+5+3+4)
	el := g.elementOf["c0b1"]
	assert.Equal(t, Element{Kind: ElementBarrier, Side: SideMitigation, Path: 0, Barrier: 1}, el)
	assert.Equal(t, ElementTopEvent, g.elementOf["top"].Kind)
}

func TestGraphKeyFollowsStatusButNotEvidence(t *testing.T) {
	m := prototypeModel(t)
	k0 := buildGraph(m).key
	m.Threats[0].Barriers[0].Evidence = "something else"
	assert.Equal(t, k0, buildGraph(m).key, "evidence is read at hover time, not laid out")
	m.Threats[0].Barriers[0].Status = StatusFailed
	assert.NotEqual(t, k0, buildGraph(m).key, "a status changes the barrier's row, so its box")
}

func TestWrap(t *testing.T) {
	assert.Equal(t, "Ransomware\nencrypts core ERP", wrap("Ransomware encrypts core ERP", 20))
	assert.Equal(t, "Production\nhalt over 48 h", wrap("Production halt over 48 h", 24), "balanced, no orphaned word")
	assert.Equal(t, "Data leaked for extortion", wrap("Data leaked for extortion", 25))
	assert.Equal(t, "short", wrap("  short ", 20))
	assert.Equal(t, "averyveryverylongword\nx", wrap("averyveryverylongword x", 10))
	assert.Equal(t, "", wrap("   ", 10))
}

// TestLayoutHonoursTheColumns runs the real engine: the pinned layers must
// come back as columns, left to right.
func TestLayoutHonoursTheColumns(t *testing.T) {
	eng, err := goccyengine.New(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	g := buildGraph(prototypeModel(t))
	lay, err := eng.Layout(context.Background(), g.model, layeredgraph.LayoutOpts{RankDir: layeredgraph.RankDirLeftRight})
	require.NoError(t, err)
	x := map[string]float64{}
	for _, n := range lay.Nodes {
		x[n.ID] = n.Center.X
	}
	assert.InDelta(t, x["t0"], x["t2"], 0.5)
	assert.InDelta(t, x["t0b1"], x["t1b0"], 0.5, "barriers next to the top event share a column")
	assert.InDelta(t, x["c0b0"], x["c2b0"], 0.5)
	assert.Less(t, x["t0"], x["t0b0"])
	assert.Less(t, x["t0b1"], x["top"])
	assert.Less(t, x["top"], x["c0b0"])
	assert.Less(t, x["c0b1"], x["c0"])
	// Barriers are row nodes: a title and the status row.
	for _, n := range lay.Nodes {
		if g.elementOf[n.ID].Kind == ElementBarrier {
			require.True(t, n.IsRowNode(), n.ID)
			require.Len(t, n.Rows, 1)
		}
	}
}

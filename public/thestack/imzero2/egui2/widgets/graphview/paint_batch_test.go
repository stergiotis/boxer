package graphview

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/metrics"
)

// pinnedLine is n pinned nodes on a horizontal line spaced dx world units
// apart, chained by n-1 directed edges.
func pinnedLine(n int, dx float32) (nodes []NodeSpec, edges []EdgeSpec) {
	for i := range n {
		nodes = append(nodes, NodeSpec{Id: uint64(i + 1), Pinned: true, PinX: float32(i) * dx, PinY: 0})
		if i > 0 {
			edges = append(edges, EdgeSpec{From: uint64(i), To: uint64(i + 1)})
		}
	}
	return
}

// resetHostBudget clears the host's reading, so a test is judged by the
// widget's own budget and starts with no back-off.
func resetHostBudget(t *testing.T) {
	metrics.Current.RecordPaintBudget(0, 0, 0)
	t.Cleanup(func() { metrics.Current.RecordPaintBudget(0, 0, 0) })
}

// What lies wholly outside the canvas is not sent: zoomed in on one end of a
// long line, most nodes and edges are culled, and the drawn and culled
// counts add up to the declaration (ADR-0329, proposed).
func TestPaintCullsWhatLiesOffTheCanvas(t *testing.T) {
	resetHostBudget(t)
	nodes, edges := pinnedLine(100, 50)
	s := newScene(t, "cull", Options{Layout: LayoutRandom}, 400, 300)
	s.frame(nodes, edges)
	s.v.SetCamera(1, 0, 0) // world origin at the canvas's top-left, zoom 1
	s.frame(nodes, edges)
	st := s.v.PaintStats()
	require.Equal(t, 100, st.Nodes+st.NodesCulled)
	require.Equal(t, 99, st.Edges+st.EdgesCulled)
	require.Less(t, st.Nodes, 20, "a 400-pixel canvas shows about eight of them")
	require.Positive(t, st.NodesCulled)
	require.Positive(t, st.EdgesCulled)
}

// Past the budget the picture drops its decorations before its data: the
// pinned rings first, then the arrow heads, then a stable sample of edges.
func TestBudgetDropsRingsThenHeadsThenThinsEdges(t *testing.T) {
	resetHostBudget(t)
	// Spaced so the fitted discs leave room for every edge and its head.
	nodes, edges := pinnedLine(40, 40)
	render := func(budget int) PaintStats {
		s := newScene(t, "budget", Options{Layout: LayoutRandom, VertexBudget: budget}, 1600, 300)
		s.frame(nodes, edges)
		s.frame(nodes, edges)
		return s.v.PaintStats()
	}
	all := render(1 << 30)
	require.False(t, all.Coarse(), "an ample budget draws everything")
	require.Equal(t, 39, all.Edges)
	require.False(t, all.NoTips)
	noRings := render(all.Vertices - 1)
	require.True(t, noRings.NoRings)
	require.False(t, noRings.NoTips)
	require.Equal(t, 1, noRings.EdgeStride)

	// Room for the discs and a fraction of the segments: no rings, no heads,
	// and one edge in k.
	segs := 4 * all.Edges
	thin := render(noRings.Vertices - 3*all.Edges - segs/2)
	require.True(t, thin.NoRings)
	require.True(t, thin.NoTips)
	require.Greater(t, thin.EdgeStride, 1)
	require.Less(t, thin.Edges, all.Edges)
	require.LessOrEqual(t, thin.Vertices, thin.Budget)
}

// A host frame past its budget backs the graph's own budget off, and frames
// under it let it recover — so a map and the graph it hosts settle under the
// host's line between them (ADR-0329, proposed).
func TestHostOverBudgetBacksTheGraphOff(t *testing.T) {
	resetHostBudget(t)
	v := New(nil, "backoff", Options{VertexBudget: 1000})
	require.Equal(t, 1000, v.vertexBudget())
	metrics.Current.RecordPaintBudget(10, 10, 1)
	backed := v.vertexBudget()
	require.Less(t, backed, 1000)
	metrics.Current.RecordPaintBudget(1, 10, 0)
	require.Greater(t, v.vertexBudget(), backed, "recovers once the host is under")
}

// The edge sample is a function of the edge index alone, so a stride keeps
// the same edges frame to frame.
func TestEdgeThinningIsStable(t *testing.T) {
	v := New(nil, "stable", Options{})
	v.stats.EdgeStride = 3
	var kept []int32
	for e := range int32(10) {
		if v.keepEdge(e) {
			kept = append(kept, e)
		}
	}
	require.Equal(t, []int32{0, 3, 6, 9}, kept)
}

// The estimate mirrors the host's mesh disc (rust/imzero2 paint_budget.rs).
func TestDiscVerticesMirrorTheHost(t *testing.T) {
	require.Equal(t, 6, discSegments(1))
	require.Equal(t, 13, discSegments(5))
	require.Equal(t, 1+2*13, discVertices(5, 0, true))
	require.Equal(t, 3*discSegments(5.5), discVertices(5, 1, false))
}

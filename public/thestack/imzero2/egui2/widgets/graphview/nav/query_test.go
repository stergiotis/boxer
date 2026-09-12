package nav

import (
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
)

func TestNeighboursAndDegreeReadTheUniverse(t *testing.T) {
	// chain: 1→2→3→4→5→6, 3→7, 8→1.
	n := chain(Options{Mode: ModeManual})
	require.Equal(t, []uint64{2, 8}, n.Neighbours(1, DirectionBoth))
	require.Equal(t, []uint64{2}, n.Neighbours(1, DirectionOut))
	require.Equal(t, []uint64{8}, n.Neighbours(1, DirectionIn))
	require.Equal(t, []uint64{2, 4, 7}, n.Neighbours(3, DirectionBoth), "ascending, not adjacency order")
	require.Nil(t, n.Neighbours(99, DirectionBoth), "an unknown id has none")

	require.Equal(t, 2, n.Degree(1, DirectionBoth))
	require.Equal(t, 1, n.Degree(1, DirectionOut))
	require.Equal(t, 3, n.Degree(3, DirectionBoth))
	require.Equal(t, 0, n.Degree(99, DirectionBoth))

	// The picture does not change the answers: these are questions about the
	// graph, and nothing here has been shown at all.
	require.Empty(t, visible(n))
	n.Hide(2)
	require.Equal(t, []uint64{2, 8}, n.Neighbours(1, DirectionBoth), "a hidden node is still a neighbour")
	require.Equal(t, 2, n.Degree(1, DirectionBoth))
}

func TestDegreeCountsParallelEdgesAndIgnoresSelfLoops(t *testing.T) {
	n := New(Options{})
	n.AddNodes(nodes(1, 2))
	n.AddEdges([]graphview.EdgeSpec{
		{From: 1, To: 2, Id: 1},
		{From: 1, To: 2, Id: 2},
		{From: 1, To: 1, Id: 3},
	})
	require.Equal(t, []uint64{2}, n.Neighbours(1, DirectionBoth), "one neighbour, twice over")
	require.Equal(t, 2, n.Degree(1, DirectionBoth), "both parallel edges count")
	require.Equal(t, 2, n.Degree(1, DirectionOut))
	require.Equal(t, 0, n.Degree(1, DirectionIn), "the self-loop is not in the adjacency")
}

func TestComponentsPartitionTheUniverse(t *testing.T) {
	n := chain(Options{})
	n.AddNodes(nodes(20, 21, 30))
	n.AddEdges(edges([2]uint64{21, 20}))
	comps := n.Components()
	require.Equal(t, [][]uint64{
		{1, 2, 3, 4, 5, 6, 7, 8},
		{20, 21},
		{30},
	}, comps, "ascending members, ordered by their smallest, isolated nodes included")

	// Direction is ignored: 8→1 and 21→20 both join their components.
	require.Equal(t, 3, len(comps))

	require.Nil(t, New(Options{}).Components(), "an empty universe has none")
}

func TestShortestPathIsFewestHopsAndDeterministic(t *testing.T) {
	n := chain(Options{})
	require.Equal(t, []uint64{1, 2, 3, 4, 5, 6}, n.ShortestPath(1, 6, DirectionOut))
	require.Equal(t, []uint64{1}, n.ShortestPath(1, 1, DirectionBoth))
	require.Nil(t, n.ShortestPath(6, 1, DirectionOut), "against the arrows there is no way back")
	require.Equal(t, []uint64{6, 5, 4, 3, 2, 1}, n.ShortestPath(6, 1, DirectionBoth))
	require.Nil(t, n.ShortestPath(1, 99, DirectionBoth), "an unknown end has no path")
	require.Nil(t, n.ShortestPath(99, 1, DirectionBoth))

	// A shortcut is taken when it is shorter, and the tie-break is by id.
	n.AddEdges(edges([2]uint64{1, 5}))
	require.Equal(t, []uint64{1, 5, 6}, n.ShortestPath(1, 6, DirectionOut))

	// Two equal-length routes: the id order of the adjacency decides, so the
	// answer is the same every time.
	m := New(Options{})
	m.AddNodes(nodes(1, 2, 3, 4))
	m.AddEdges(edges([2]uint64{1, 3}, [2]uint64{1, 2}, [2]uint64{2, 4}, [2]uint64{3, 4}))
	first := m.ShortestPath(1, 4, DirectionOut)
	require.Equal(t, []uint64{1, 2, 4}, first, "the smaller-id neighbour wins the tie")
	for range 5 {
		require.Equal(t, first, m.ShortestPath(1, 4, DirectionOut))
	}
}

func TestQueriesAreSafeInsideTheStyleHook(t *testing.T) {
	// The hook runs inside Declare, where the walk's scratch is live. A
	// query must not disturb it, so it keeps its own (ADR-0225 §SD3).
	n := chain(Options{Mode: ModeManual, ExpandDepth: 2})
	n.Show(1)
	n.Expand(1, 2, DirectionBoth)
	before := visible(n)

	var seen []int
	n.Opts.Style = func(id uint64, info NodeInfo, spec *graphview.NodeSpec) {
		seen = append(seen, n.Degree(id, DirectionBoth))
		n.Neighbours(id, DirectionBoth)
		n.ShortestPath(id, 6, DirectionBoth)
		n.Components()
	}
	require.Equal(t, before, visible(n), "the derivation is unchanged by the queries")
	require.Len(t, seen, len(before))
}

func TestShortestPathAgreesWithABreadthFirstOracle(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cnt := rapid.IntRange(2, 12).Draw(rt, "n")
		n := New(Options{})
		idList := make([]uint64, cnt)
		for i := range idList {
			idList[i] = uint64(i + 1)
		}
		n.AddNodes(nodes(idList...))
		m := rapid.IntRange(0, 3*cnt).Draw(rt, "m")
		var es []graphview.EdgeSpec
		for i := range m {
			es = append(es, graphview.EdgeSpec{
				From: uint64(rapid.IntRange(1, cnt).Draw(rt, "from")),
				To:   uint64(rapid.IntRange(1, cnt).Draw(rt, "to")),
				Id:   uint64(i),
			})
		}
		n.AddEdges(es)
		src := uint64(rapid.IntRange(1, cnt).Draw(rt, "src"))
		dst := uint64(rapid.IntRange(1, cnt).Draw(rt, "dst"))
		got := n.ShortestPath(src, dst, DirectionBoth)

		// Oracle: hop counts by repeated relaxation over the declared edges.
		const inf = 1 << 30
		dist := map[uint64]int{src: 0}
		for range cnt {
			for _, e := range es {
				for _, pair := range [][2]uint64{{e.From, e.To}, {e.To, e.From}} {
					if e.From == e.To {
						continue
					}
					if d, ok := dist[pair[0]]; ok {
						if cur, seen := dist[pair[1]]; !seen || d+1 < cur {
							dist[pair[1]] = d + 1
						}
					}
				}
			}
		}
		want, reachable := dist[dst]
		if !reachable {
			require.Nil(rt, got)
			return
		}
		require.Len(rt, got, want+1, "as many nodes as hops plus one")
		require.Equal(rt, src, got[0])
		require.Equal(rt, dst, got[len(got)-1])
		// Every step is a real edge.
		for i := 0; i+1 < len(got); i++ {
			a, b := got[i], got[i+1]
			found := false
			for _, e := range es {
				if (e.From == a && e.To == b) || (e.From == b && e.To == a) {
					found = true
					break
				}
			}
			require.True(rt, found, "step %d→%d is an edge", a, b)
		}
	})
}

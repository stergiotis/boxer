package nav

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
)

func ids(specs []graphview.NodeSpec) []uint64 {
	out := make([]uint64, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.Id)
	}
	slices.Sort(out)
	return out
}

func nodes(idList ...uint64) []Node {
	out := make([]Node, 0, len(idList))
	for _, id := range idList {
		out = append(out, Node{Spec: graphview.NodeSpec{Id: id}})
	}
	return out
}

func edges(pairs ...[2]uint64) []graphview.EdgeSpec {
	out := make([]graphview.EdgeSpec, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, graphview.EdgeSpec{From: p[0], To: p[1]})
	}
	return out
}

// chain is 1→2→3→4→5→6 with a side branch 3→7 and an edge into 1 from 8.
func chain(o Options) *Navigator {
	n := New(o)
	n.AddNodes(nodes(1, 2, 3, 4, 5, 6, 7, 8))
	n.AddEdges(edges([2]uint64{1, 2}, [2]uint64{2, 3}, [2]uint64{3, 4}, [2]uint64{4, 5}, [2]uint64{5, 6}, [2]uint64{3, 7}, [2]uint64{8, 1}))
	return n
}

func visible(n *Navigator) []uint64 {
	ns, _ := n.Declare()
	return ids(ns)
}

func TestManualExpandAndCollapseAreInverses(t *testing.T) {
	n := chain(Options{Mode: ModeManual})
	require.Empty(t, visible(n), "nothing is shown until a root is")
	require.True(t, n.Show(1))
	require.False(t, n.Show(99))
	require.Equal(t, []uint64{1}, visible(n))
	require.True(t, n.Expand(1, 1, DirBoth))
	require.Equal(t, []uint64{1, 2, 8}, visible(n))
	n.Expand(2, 2, DirBoth)
	require.Equal(t, []uint64{1, 2, 3, 4, 7, 8}, visible(n))
	require.Equal(t, 2, n.Depth(4), "hops from the nearest root")
	require.Equal(t, float32(1), n.Relevance(4))
	require.True(t, n.Collapse(2))
	require.Equal(t, []uint64{1, 2, 8}, visible(n), "what only 2 revealed is gone")
	require.False(t, n.Collapse(2))
	n.Collapse(1)
	require.Equal(t, []uint64{1}, visible(n))
	require.False(t, n.Expand(99, 1, DirBoth))
}

func TestManualDirectionAndHiddenWall(t *testing.T) {
	n := chain(Options{Mode: ModeManual})
	n.Show(1)
	n.Expand(1, 1, DirOut)
	require.Equal(t, []uint64{1, 2}, visible(n), "out follows 1→2 only")
	n.Expand(1, 1, DirIn)
	require.Equal(t, []uint64{1, 8}, visible(n), "in follows 8→1 only, replacing the earlier expansion")
	n.Expand(1, 4, DirBoth)
	require.Equal(t, []uint64{1, 2, 3, 4, 5, 7, 8}, visible(n))
	n.Hide(3)
	require.Equal(t, []uint64{1, 2, 8}, visible(n), "a hidden node is a wall")
	require.True(t, n.IsHidden(3))
	require.Equal(t, 1, n.HiddenNeighbours(2))
	require.Equal(t, 0, n.HiddenNeighbours(8))
	n.Show(3)
	require.Equal(t, []uint64{1, 2, 3, 4, 5, 7, 8}, visible(n))
	n.Close(1)
	require.Equal(t, []uint64{3}, visible(n), "close is collapse plus hide; 3 stays a root, unexpanded")
}

func TestManualExpansionsChainRegardlessOfOrder(t *testing.T) {
	// 3's expansion applies once 1's makes 3 visible, whatever the map order.
	n := chain(Options{Mode: ModeManual})
	n.Show(1)
	n.Expand(3, 1, DirBoth)
	require.Equal(t, []uint64{1}, visible(n), "an expansion of a node not shown waits")
	n.Expand(1, 2, DirBoth)
	require.Equal(t, []uint64{1, 2, 3, 4, 7, 8}, visible(n))
}

func TestFocusModeRadiusRelevanceAndList(t *testing.T) {
	n := chain(Options{Mode: ModeFocus, FocusRadius: 2, FocusTailRadius: 1, MaxFocusNodes: 2})
	require.True(t, n.Focus(1, 0))
	require.Equal(t, []uint64{1, 2, 3, 8}, visible(n))
	require.Equal(t, float32(1), n.Relevance(1))
	require.Equal(t, float32(0.5), n.Relevance(2))
	require.Equal(t, float32(0.25), n.Relevance(3))
	require.Equal(t, 2, n.Depth(3))
	require.Equal(t, float32(0), n.Relevance(4), "not visible")
	require.Equal(t, -1, n.Depth(4))

	n.Focus(4, 0)
	require.Equal(t, []uint64{1, 2, 3, 4, 5, 6, 7, 8}, visible(n), "1 at the tail radius keeps 2 and 8; 4 reaches 2..7")
	require.Equal(t, []uint64{1, 4}, slices.Collect(n.FocusNodes()))
	require.Equal(t, float32(0.5), n.Relevance(3), "the larger of the two focus contributions")
	require.Equal(t, float32(0.5), n.Relevance(2))

	n.Focus(6, 0)
	require.Equal(t, []uint64{4, 6}, slices.Collect(n.FocusNodes()), "the oldest was unfocused")
	require.Equal(t, []uint64{3, 4, 5, 6}, visible(n))
	n.Focus(4, 0)
	require.Equal(t, []uint64{6, 4}, slices.Collect(n.FocusNodes()), "refocusing moves to the end")
	require.True(t, n.IsFocused(4))
	n.Unfocus(6)
	require.Equal(t, []uint64{4}, slices.Collect(n.FocusNodes()))
	n.ClearFocus()
	require.Empty(t, visible(n))

	strict := chain(Options{Mode: ModeFocus, MaxFocusNodes: 1, NoAutoUnfocus: true})
	require.True(t, strict.Focus(1, 0))
	require.False(t, strict.Focus(2, 0), "refused rather than unfocusing")
	require.Equal(t, []uint64{1}, slices.Collect(strict.FocusNodes()))
}

func TestFocusModeRootsAndHiddenCompose(t *testing.T) {
	n := chain(Options{Mode: ModeFocus, FocusRadius: 1})
	n.Focus(1, 0)
	n.Show(6)
	require.Equal(t, []uint64{1, 2, 6, 8}, visible(n), "a root is shown in every mode")
	require.Equal(t, 0, n.Depth(6))
	n.Hide(2)
	require.Equal(t, []uint64{1, 6, 8}, visible(n))
	n.Hide(1)
	require.Equal(t, []uint64{6}, visible(n), "a hidden focus node contributes nothing")
	require.True(t, n.IsFocused(1), "but stays listed for when it is shown")
	n.Show(1)
	require.Equal(t, []uint64{1, 6, 8}, visible(n))
}

func TestStubsReachThePendingListAndLeaveItWhenLoaded(t *testing.T) {
	n := New(Options{Mode: ModeFocus, FocusRadius: 2})
	n.AddNodes([]Node{{Spec: graphview.NodeSpec{Id: 1}}, {Spec: graphview.NodeSpec{Id: 2}}, {Spec: graphview.NodeSpec{Id: 3}, Stub: true}})
	n.AddEdges(edges([2]uint64{1, 2}, [2]uint64{2, 3}))
	n.Focus(1, 0)
	require.Equal(t, []uint64{1, 2, 3}, visible(n))
	require.Empty(t, n.Pending(), "a stub at the frontier is shown, not wanted")
	n.Opts.FocusRadius = 3
	require.Equal(t, []uint64{3}, n.Pending(), "a stub with hops left is wanted")
	require.True(t, n.IsStub(3))
	n.AddNodes([]Node{{Spec: graphview.NodeSpec{Id: 3}}})
	n.AddEdges(edges([2]uint64{3, 4}))
	require.Equal(t, []uint64{1, 2, 3}, visible(n), "an edge to an unknown id waits")
	n.AddNodes(nodes(4))
	require.Equal(t, []uint64{1, 2, 3, 4}, visible(n))
	require.Empty(t, n.Pending())

	// A focused stub is wanted at once; a manual expansion of one too.
	m := New(Options{Mode: ModeManual})
	m.AddNodes([]Node{{Spec: graphview.NodeSpec{Id: 9}, Stub: true}})
	m.Show(9)
	require.Empty(t, m.Pending())
	m.Expand(9, 1, DirBoth)
	require.Equal(t, []uint64{9}, m.Pending())
}

func TestDeclareIsCachedOrderedAndStyled(t *testing.T) {
	styled := map[uint64][2]float32{}
	n := chain(Options{Mode: ModeFocus, FocusRadius: 1, Style: func(id uint64, rel float32, depth int, spec *graphview.NodeSpec) {
		styled[id] = [2]float32{rel, float32(depth)}
		spec.Radius = 10 * rel
	}})
	n.Focus(1, 0)
	ns, es := n.Declare()
	require.Equal(t, []uint64{1, 2, 8}, ids(ns))
	require.Equal(t, uint64(1), ns[0].Id, "universe order")
	require.Equal(t, float32(5), ns[1].Radius, "the hook edited the declared spec")
	require.Equal(t, [2]float32{0.5, 1}, styled[2])
	require.Len(t, es, 2, "edges with both ends visible: 1→2 and 8→1")
	ns2, _ := n.Declare()
	require.Same(t, &ns[0], &ns2[0], "nothing changed: the same backing array")
	n.Opts.FocusRadius = 2
	ns3, _ := n.Declare()
	require.Len(t, ns3, 4, "an option change derives afresh: 3 joins at two hops")
}

func TestApplyGestures(t *testing.T) {
	n := chain(Options{Mode: ModeManual})
	n.Show(1)
	dbl := graphview.Event{Kind: graphview.EventKindNodeDoubleClick, Node: 1}
	n.Apply(dbl)
	require.True(t, n.Expanded(1))
	require.Equal(t, []uint64{1, 2, 8}, visible(n))
	n.Apply(dbl)
	require.False(t, n.Expanded(1))
	n.Apply(graphview.Event{Kind: graphview.EventKindNodeClick, Node: 1})
	require.False(t, n.Expanded(1), "a plain click is not wired")

	f := chain(Options{Mode: ModeFocus})
	f.Apply(dbl)
	require.Equal(t, []uint64{1}, slices.Collect(f.FocusNodes()))
	a := chain(Options{Mode: ModeShowAll})
	a.Apply(dbl)
	require.Len(t, visible(a), 8, "show-all shows everything and ignores the gesture")
}

func TestUniverseEditsAndReset(t *testing.T) {
	n := chain(Options{Mode: ModeManual})
	n.SetInitial([]uint64{1, 3})
	n.Reset()
	require.Equal(t, []uint64{1, 3}, visible(n))
	n.Expand(3, 1, DirBoth)
	require.Equal(t, []uint64{1, 2, 3, 4, 7}, visible(n))
	n.RemoveNodes([]uint64{4})
	require.False(t, n.Known(4))
	require.Equal(t, []uint64{1, 2, 3, 7}, visible(n))
	require.Equal(t, 5, n.EdgeCount(), "the two edges at 4 went with it")
	n.RemoveEdges([]graphview.EdgeRef{{From: 3, To: 7}})
	require.Equal(t, []uint64{1, 2, 3}, visible(n))
	n.AddNodes(nodes(4))
	n.AddEdges(edges([2]uint64{3, 4}))
	require.Equal(t, []uint64{1, 2, 3, 4}, visible(n), "a returning id takes effect again")
	n.Reset()
	require.Equal(t, []uint64{1, 3}, visible(n))

	f := chain(Options{Mode: ModeFocus, FocusRadius: 1})
	f.SetInitial([]uint64{6})
	f.Reset()
	require.Equal(t, []uint64{6}, slices.Collect(f.FocusNodes()), "focus mode focuses the initial nodes")
	require.Equal(t, []uint64{5, 6}, visible(f))
	f.Clear()
	require.Equal(t, 0, f.NodeCount())
	require.Empty(t, visible(f))
}

func TestParallelEdgesAndSelfLoops(t *testing.T) {
	n := New(Options{Mode: ModeManual})
	n.AddNodes(nodes(1, 2))
	n.AddEdges([]graphview.EdgeSpec{{From: 1, To: 2, Id: 1}, {From: 1, To: 2, Id: 2}, {From: 1, To: 1}})
	n.Show(1)
	require.Equal(t, 1, n.HiddenNeighbours(1), "two parallel edges, one neighbour; the loop is none")
	n.Expand(1, 1, DirBoth)
	_, es := n.Declare()
	require.Len(t, es, 3)
	n.AddEdges([]graphview.EdgeSpec{{From: 1, To: 2, Id: 2, Label: "replaced"}})
	require.Equal(t, 3, n.EdgeCount())
	_, es = n.Declare()
	require.Equal(t, "replaced", es[1].Label)
}

// A random universe under a random gesture sequence: an expand followed by
// its collapse leaves the picture as it was, hidden nodes are never shown,
// roots always are unless hidden, and declared edges join visible ends.
func TestGesturesKeepTheInvariants(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cnt := rapid.IntRange(1, 12).Draw(rt, "n")
		mode := Mode(rapid.IntRange(1, 2).Draw(rt, "mode"))
		n := New(Options{Mode: mode, FocusRadius: rapid.IntRange(1, 3).Draw(rt, "radius"), MaxFocusNodes: rapid.IntRange(1, 3).Draw(rt, "max")})
		nl := make([]Node, 0, cnt)
		for i := 1; i <= cnt; i++ {
			nl = append(nl, Node{Spec: graphview.NodeSpec{Id: uint64(i)}, Stub: rapid.Bool().Draw(rt, "stub")})
		}
		n.AddNodes(nl)
		m := rapid.IntRange(0, 3*cnt).Draw(rt, "m")
		el := make([]graphview.EdgeSpec, 0, m)
		for range m {
			el = append(el, graphview.EdgeSpec{From: uint64(rapid.IntRange(1, cnt).Draw(rt, "f")), To: uint64(rapid.IntRange(1, cnt).Draw(rt, "t"))})
		}
		n.AddEdges(el)
		steps := rapid.IntRange(0, 12).Draw(rt, "steps")
		for range steps {
			id := uint64(rapid.IntRange(1, cnt).Draw(rt, "id"))
			switch rapid.IntRange(0, 5).Draw(rt, "op") {
			case 0:
				n.Show(id)
			case 1:
				n.Hide(id)
			case 2:
				n.Expand(id, rapid.IntRange(1, 3).Draw(rt, "d"), Direction(rapid.IntRange(0, 2).Draw(rt, "dir")))
			case 3:
				n.Collapse(id)
			case 4:
				n.Focus(id, 0)
			case 5:
				n.Unfocus(id)
			}
		}
		before := visible(n)
		x := uint64(rapid.IntRange(1, cnt).Draw(rt, "x"))
		if !n.Expanded(x) {
			n.Expand(x, rapid.IntRange(1, 3).Draw(rt, "xd"), DirBoth)
			n.Collapse(x)
			require.Equal(rt, before, visible(n), "expand then collapse is the identity")
		}
		ns, es := n.Declare()
		vis := map[uint64]bool{}
		for _, s := range ns {
			vis[s.Id] = true
			require.False(rt, n.IsHidden(s.Id), "a hidden node is never declared")
		}
		for id := uint64(1); id <= uint64(cnt); id++ {
			if _, root := n.roots[id]; root && !n.IsHidden(id) {
				require.True(rt, vis[id], "a root not hidden is visible")
			}
		}
		for _, e := range es {
			require.True(rt, vis[e.From] && vis[e.To], "declared edges join visible ends")
		}
		for _, id := range n.Pending() {
			require.True(rt, n.IsStub(id), "only stubs are wanted")
		}
	})
}

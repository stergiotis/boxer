// Differential tests: every algo entry point is checked against a
// brute-force reference built on the transitive closure of the live
// subgraph. The references share nothing with the production code (no
// Tarjan, no Kahn, no DFS bookkeeping), so a bug has to be made twice —
// in two independent formulations — to slip through. Inputs are
// rapid-generated graggles including cycles, deletions, and the pseudo-
// edges those produce.
package algo_test

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/algo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/store"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// liveView captures the exact graph the algorithms operate on: live
// nodes and the live+pseudo successor relation.
type liveView struct {
	nodes []t.NodeID
	succ  map[t.NodeID][]t.NodeID
	reach map[t.NodeID]map[t.NodeID]bool // transitive, NOT reflexive
}

func newLiveView(g t.GraphReaderI) *liveView {
	v := &liveView{succ: make(map[t.NodeID][]t.NodeID), reach: make(map[t.NodeID]map[t.NodeID]bool)}
	for n := range g.AllLiveNodes() {
		v.nodes = append(v.nodes, n)
	}
	for _, n := range v.nodes {
		seen := make(map[t.NodeID]struct{})
		for c := range g.LiveChildren(n) {
			if !g.IsLive(c) {
				continue
			}
			if _, dup := seen[c]; dup {
				continue // parallel edges collapse for reachability purposes
			}
			seen[c] = struct{}{}
			v.succ[n] = append(v.succ[n], c)
		}
	}
	// Floyd-Warshall-free closure: plain BFS per node, O(V*(V+E)).
	for _, n := range v.nodes {
		r := make(map[t.NodeID]bool)
		stack := slices.Clone(v.succ[n])
		for len(stack) > 0 {
			x := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if r[x] {
				continue
			}
			r[x] = true
			stack = append(stack, v.succ[x]...)
		}
		v.reach[n] = r
	}
	return v
}

func (v *liveView) comparable_(a, b t.NodeID) bool {
	return v.reach[a][b] || v.reach[b][a]
}

// refSCCPartition: a ~ b iff they reach each other (or a == b).
func (v *liveView) refSCCPartition() map[t.NodeID][]t.NodeID {
	classOf := make(map[t.NodeID]t.NodeID) // node -> class representative (min by CompareNodeID)
	for _, a := range v.nodes {
		rep := a
		for _, b := range v.nodes {
			if a != b && v.reach[a][b] && v.reach[b][a] && t.CompareNodeID(b, rep) < 0 {
				rep = b
			}
		}
		classOf[a] = rep
	}
	out := make(map[t.NodeID][]t.NodeID)
	for n, rep := range classOf {
		out[rep] = append(out[rep], n)
	}
	for _, members := range out {
		slices.SortFunc(members, t.CompareNodeID)
	}
	return out
}

// refIsLinear: acyclic and totally comparable.
func (v *liveView) refIsLinear() bool {
	for _, a := range v.nodes {
		if v.reach[a][a] {
			return false // on a cycle
		}
	}
	for i, a := range v.nodes {
		for _, b := range v.nodes[i+1:] {
			if !v.comparable_(a, b) {
				return false
			}
		}
	}
	return true
}

// refLinearOrder: when linear, descending count of reachable nodes is
// the unique topological order.
func (v *liveView) refLinearOrder() []t.NodeID {
	order := slices.Clone(v.nodes)
	sort.SliceStable(order, func(i, j int) bool {
		return len(v.reach[order[i]]) > len(v.reach[order[j]])
	})
	return order
}

// canonicalize conflict sets for comparison.
func canonOrderConflicts(infos []algo.ConflictInfo) map[string]struct{} {
	out := make(map[string]struct{})
	for _, ci := range infos {
		if ci.Kind != "order" || len(ci.Nodes) != 3 {
			continue
		}
		a, b := ci.Nodes[1], ci.Nodes[2]
		if t.CompareNodeID(b, a) < 0 {
			a, b = b, a
		}
		out[ci.Nodes[0].String()+"|"+a.String()+"|"+b.String()] = struct{}{}
	}
	return out
}

func canonSingletons(infos []algo.ConflictInfo, kind string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, ci := range infos {
		if ci.Kind == kind {
			out[ci.Nodes[0].String()] = struct{}{}
		}
	}
	return out
}

// buildRandomGraggle drives the real store mutators: chained inserts,
// random extra edges (possibly cycle-forming), random deletions, then
// pseudo-edge resolution — so references see production-shaped graphs.
func buildRandomGraggle(rt *rapid.T) *store.Graggle {
	g := store.New()
	n := rapid.IntRange(1, 10).Draw(rt, "nodes")
	ids := []t.NodeID{t.RootNodeID}
	for i := 0; i < n; i++ {
		id := t.NodeID{Patch: ph("diff"), Index: uint64(i)}
		up := ids[rapid.IntRange(0, len(ids)-1).Draw(rt, "up")]
		if err := g.AddNode(id, []byte{byte('a' + i%26), '\n'}, ph("diff"), []t.NodeID{up}, nil); err != nil {
			rt.Fatalf("AddNode: %v", err)
		}
		ids = append(ids, id)
	}
	extra := rapid.IntRange(0, 4).Draw(rt, "extraEdges")
	for i := 0; i < extra; i++ {
		src := ids[rapid.IntRange(0, len(ids)-1).Draw(rt, "src")]
		dest := ids[rapid.IntRange(1, len(ids)-1).Draw(rt, "dest")] // never into root
		if src == dest {
			continue
		}
		if err := g.AddEdge(src, dest, ph("extra")); err != nil {
			rt.Fatalf("AddEdge: %v", err)
		}
	}
	dels := rapid.IntRange(0, n/2).Draw(rt, "dels")
	for i := 0; i < dels; i++ {
		victim := ids[rapid.IntRange(1, len(ids)-1).Draw(rt, "victim")]
		if g.IsLive(victim) {
			if err := g.DeleteNode(victim, ph("del")); err != nil {
				rt.Fatalf("DeleteNode: %v", err)
			}
		}
	}
	g.ResolvePseudoEdges()
	return g
}

func TestDifferential_AlgorithmsAgainstClosureReference(tt *testing.T) {
	rapid.Check(tt, func(rt *rapid.T) {
		g := buildRandomGraggle(rt)
		v := newLiveView(g)

		// --- Tarjan: identical SCC partitions.
		want := v.refSCCPartition()
		got := make(map[t.NodeID][]t.NodeID)
		for _, scc := range algo.Tarjan(g) {
			members := slices.Clone(scc)
			slices.SortFunc(members, t.CompareNodeID)
			got[members[0]] = members
		}
		if len(got) != len(want) {
			rt.Fatalf("SCC count: got %d want %d", len(got), len(want))
		}
		for rep, members := range want {
			if !slices.Equal(got[rep], members) {
				rt.Fatalf("SCC class %v: got %v want %v", rep, got[rep], members)
			}
		}

		// --- TopoSort: nil iff cyclic; otherwise a valid topological
		// permutation of the live nodes.
		cyclic := false
		for _, n := range v.nodes {
			if v.reach[n][n] {
				cyclic = true
			}
		}
		order := algo.TopoSort(g)
		if cyclic != (order == nil) {
			rt.Fatalf("TopoSort nil-ness disagrees with reference: cyclic=%v order=%v", cyclic, order)
		}
		if order != nil {
			if len(order) != len(v.nodes) {
				rt.Fatalf("TopoSort length %d, live nodes %d", len(order), len(v.nodes))
			}
			pos := make(map[t.NodeID]int, len(order))
			for i, n := range order {
				pos[n] = i
			}
			for src, succs := range v.succ {
				for _, dst := range succs {
					if pos[src] >= pos[dst] {
						rt.Fatalf("TopoSort violates edge %v->%v", src, dst)
					}
				}
			}
		}

		// --- LinearOrder: nil-ness and, when linear, the exact order.
		linear := algo.LinearOrder(g)
		if v.refIsLinear() != (linear != nil) {
			rt.Fatalf("LinearOrder nil-ness disagrees with reference (refLinear=%v)", v.refIsLinear())
		}
		if linear != nil {
			if want := v.refLinearOrder(); !slices.Equal(linear, want) {
				rt.Fatalf("LinearOrder: got %v want %v", linear, want)
			}
		}

		// --- DetectConflicts: per-kind canonical equality.
		infos := algo.DetectConflicts(g)

		wantOrder := make(map[string]struct{})
		for _, p := range v.nodes {
			children := v.succ[p]
			for i := 0; i < len(children); i++ {
				for j := i + 1; j < len(children); j++ {
					a, b := children[i], children[j]
					if a == b || v.comparable_(a, b) {
						continue
					}
					if t.CompareNodeID(b, a) < 0 {
						a, b = b, a
					}
					wantOrder[p.String()+"|"+a.String()+"|"+b.String()] = struct{}{}
				}
			}
		}
		if got, want := canonOrderConflicts(infos), wantOrder; !mapsEqual(got, want) {
			rt.Fatalf("order conflicts: got %v want %v", keys(got), keys(want))
		}

		wantOrphan := make(map[string]struct{})
		for _, n := range v.nodes {
			if n != t.RootNodeID && !v.reach[t.RootNodeID][n] {
				wantOrphan[n.String()] = struct{}{}
			}
		}
		if got := canonSingletons(infos, "orphan"); !mapsEqual(got, wantOrphan) {
			rt.Fatalf("orphan conflicts: got %v want %v", keys(got), keys(wantOrphan))
		}

		wantCycleMembers := make(map[string]struct{})
		for _, n := range v.nodes {
			if v.reach[n][n] {
				wantCycleMembers[n.String()] = struct{}{}
			}
		}
		gotCycleMembers := make(map[string]struct{})
		for _, ci := range infos {
			if ci.Kind == "cycle" {
				for _, n := range ci.Nodes {
					gotCycleMembers[n.String()] = struct{}{}
				}
			}
		}
		if !mapsEqual(gotCycleMembers, wantCycleMembers) {
			rt.Fatalf("cycle members: got %v want %v", keys(gotCycleMembers), keys(wantCycleMembers))
		}

		// --- Zombie: live node with any incident deleted-kind edge.
		wantZombie := make(map[string]struct{})
		for _, n := range v.nodes {
			if n == t.RootNodeID {
				continue
			}
			has := false
			for e := range g.ForwardEdges(n) {
				if e.Kind == t.EdgeKindDeleted {
					has = true
				}
			}
			for e := range g.BackwardEdges(n) {
				if e.Kind == t.EdgeKindDeleted {
					has = true
				}
			}
			if has {
				wantZombie[n.String()] = struct{}{}
			}
		}
		if got := canonSingletons(infos, "zombie"); !mapsEqual(got, wantZombie) {
			rt.Fatalf("zombie conflicts: got %v want %v", keys(got), keys(wantZombie))
		}
	})
}

func mapsEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func keys(m map[string]struct{}) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

package algo_test

import (
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/algo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/store"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

func TestTopoSort_SingleNode(tt *testing.T) {
	g := store.New()
	order := algo.TopoSort(g)
	if order == nil {
		tt.Fatal("single root node should have a topo sort")
	}
	if len(order) != 1 || order[0] != t.RootNodeID {
		tt.Fatalf("expected [root], got %v", order)
	}
}

func TestTopoSort_Diamond(tt *testing.T) {
	// root -> a, root -> b, a -> c, b -> c
	g := store.New()
	a := nid("topo1", 0)
	b := nid("topo1", 1)
	c := nid("topo1", 2)
	g.AddNode(a, []byte("a\n"), ph("topo1"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("topo1"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(c, []byte("c\n"), ph("topo1"), []t.NodeID{a, b}, nil)

	order := algo.TopoSort(g)
	if order == nil {
		tt.Fatal("diamond DAG should have a topo sort")
	}
	if len(order) != 4 {
		tt.Fatalf("expected 4 nodes, got %d", len(order))
	}

	// root must come before a,b; a,b must come before c.
	pos := make(map[t.NodeID]int)
	for i, n := range order {
		pos[n] = i
	}
	if pos[t.RootNodeID] >= pos[a] || pos[t.RootNodeID] >= pos[b] {
		tt.Fatal("root must come before a and b")
	}
	if pos[a] >= pos[c] || pos[b] >= pos[c] {
		tt.Fatal("a and b must come before c")
	}
}

func TestTopoSort_CycleReturnsNil(tt *testing.T) {
	// Create a cycle by building nodes then adding a back-edge.
	g := store.New()
	a := nid("topo_cyc", 0)
	b := nid("topo_cyc", 1)
	g.AddNode(a, []byte("a\n"), ph("topo_cyc"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("topo_cyc"), []t.NodeID{a}, nil)

	// Add back-edge b -> a to create cycle.
	g.AddEdge(b, a, ph("topo_cyc_back"))

	order := algo.TopoSort(g)
	if order != nil {
		tt.Fatal("cyclic graph should return nil from TopoSort")
	}
}

func TestTarjan_Cycle(tt *testing.T) {
	g := store.New()
	a := nid("tarj_cyc", 0)
	b := nid("tarj_cyc", 1)
	g.AddNode(a, []byte("a\n"), ph("tarj_cyc"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("tarj_cyc"), []t.NodeID{a}, nil)

	// Create cycle: b -> a.
	g.AddEdge(b, a, ph("tarj_cyc_back"))

	sccs := algo.Tarjan(g)

	// Should have a multi-vertex SCC containing a and b.
	foundCycle := false
	for _, scc := range sccs {
		if len(scc) > 1 {
			foundCycle = true
			set := make(map[t.NodeID]struct{})
			for _, n := range scc {
				set[n] = struct{}{}
			}
			if _, ok := set[a]; !ok {
				tt.Fatal("cycle SCC should contain a")
			}
			if _, ok := set[b]; !ok {
				tt.Fatal("cycle SCC should contain b")
			}
		}
	}
	if !foundCycle {
		tt.Fatal("expected a multi-vertex SCC for the cycle")
	}
}

func TestTarjan_DiamondNoCycle(tt *testing.T) {
	g := store.New()
	a := nid("tarj_dia", 0)
	b := nid("tarj_dia", 1)
	c := nid("tarj_dia", 2)
	g.AddNode(a, []byte("a\n"), ph("tarj_dia"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("tarj_dia"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(c, []byte("c\n"), ph("tarj_dia"), []t.NodeID{a, b}, nil)

	sccs := algo.Tarjan(g)
	for _, scc := range sccs {
		if len(scc) > 1 {
			tt.Fatalf("diamond DAG should have no multi-vertex SCC, got %v", scc)
		}
	}
}

func TestTarjan_MultipleSCCs(tt *testing.T) {
	// Create two separate cycles.
	g := store.New()
	a := nid("tarj_multi", 0)
	b := nid("tarj_multi", 1)
	c := nid("tarj_multi", 2)
	d := nid("tarj_multi", 3)

	g.AddNode(a, []byte("a\n"), ph("tarj_multi"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("tarj_multi"), []t.NodeID{a}, nil)
	g.AddNode(c, []byte("c\n"), ph("tarj_multi"), []t.NodeID{b}, nil)
	g.AddNode(d, []byte("d\n"), ph("tarj_multi"), []t.NodeID{c}, nil)

	// Cycle 1: a <-> b
	g.AddEdge(b, a, ph("back1"))
	// Cycle 2: c <-> d
	g.AddEdge(d, c, ph("back2"))

	sccs := algo.Tarjan(g)
	multiVertex := 0
	for _, scc := range sccs {
		if len(scc) > 1 {
			multiVertex++
		}
	}
	// Note: a<->b and c<->d plus the chain a->b->c->d means
	// b->a creates {a,b} cycle and d->c creates {c,d} cycle.
	// But since b->c exists, a->b->c->d->c creates a larger connected component.
	// Actually: a->b->c->d->c creates a cycle through c,d. And b->a creates a cycle through a,b.
	// These are separate SCCs because there's no back-edge from {c,d} to {a,b}.
	if multiVertex < 2 {
		tt.Fatalf("expected at least 2 multi-vertex SCCs, got %d", multiVertex)
	}
}

func TestLinearOrder_Conflict(tt *testing.T) {
	// Fork: root -> {a, b}, no edge between a and b.
	g := store.New()
	a := nid("lo_conf", 0)
	b := nid("lo_conf", 1)
	g.AddNode(a, []byte("a\n"), ph("lo_conf"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("lo_conf"), []t.NodeID{t.RootNodeID}, nil)

	order := algo.LinearOrder(g)
	if order != nil {
		tt.Fatal("fork should not have linear order")
	}
}

func TestLinearOrder_SingleNode(tt *testing.T) {
	g := store.New()
	order := algo.LinearOrder(g)
	if order == nil {
		tt.Fatal("single root should have linear order")
	}
	if len(order) != 1 {
		tt.Fatalf("expected 1 node, got %d", len(order))
	}
}

func TestDetectConflicts_OrderConflict(tt *testing.T) {
	g := store.New()
	a := nid("dc_order", 0)
	b := nid("dc_order", 1)
	g.AddNode(a, []byte("a\n"), ph("dc_order"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("dc_order"), []t.NodeID{t.RootNodeID}, nil)

	conflicts := algo.DetectConflicts(g)
	found := false
	for _, c := range conflicts {
		if c.Kind == "order" {
			found = true
		}
	}
	if !found {
		tt.Fatal("expected order conflict for fork")
	}
}

func TestDetectConflicts_CycleConflict(tt *testing.T) {
	g := store.New()
	a := nid("dc_cycle", 0)
	b := nid("dc_cycle", 1)
	g.AddNode(a, []byte("a\n"), ph("dc_cycle"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("dc_cycle"), []t.NodeID{a}, nil)
	g.AddEdge(b, a, ph("dc_cycle_back"))

	conflicts := algo.DetectConflicts(g)
	found := false
	for _, c := range conflicts {
		if c.Kind == "cycle" {
			found = true
		}
	}
	if !found {
		tt.Fatal("expected cycle conflict")
	}
}

func TestDetectConflicts_NoConflict(tt *testing.T) {
	g := store.New()
	a := nid("dc_none", 0)
	b := nid("dc_none", 1)
	g.AddNode(a, []byte("a\n"), ph("dc_none"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("dc_none"), []t.NodeID{a}, nil)

	conflicts := algo.DetectConflicts(g)
	if len(conflicts) != 0 {
		tt.Fatalf("expected no conflicts, got %v", conflicts)
	}
}

func TestDetectConflicts_ZombieConflict(tt *testing.T) {
	// a -> b -> c, then add X anchored at b, then delete b.
	// X stays live but its up-context (b) is deleted -> zombie.
	g := store.New()
	a := nid("dc_zomb", 0)
	b := nid("dc_zomb", 1)
	c := nid("dc_zomb", 2)
	g.AddNode(a, []byte("a\n"), ph("dc_zomb"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("dc_zomb"), []t.NodeID{a}, nil)
	g.AddNode(c, []byte("c\n"), ph("dc_zomb"), []t.NodeID{b}, nil)

	x := nid("dc_zomb_x", 0)
	g.AddNode(x, []byte("X\n"), ph("dc_zomb_x"), []t.NodeID{b}, []t.NodeID{c})
	if err := g.DeleteNode(b, ph("dc_zomb_del")); err != nil {
		tt.Fatalf("DeleteNode(b): %v", err)
	}
	g.ResolvePseudoEdges()

	conflicts := algo.DetectConflicts(g)
	found := false
	for _, conflict := range conflicts {
		if conflict.Kind != "zombie" {
			continue
		}
		if len(conflict.Nodes) >= 1 && conflict.Nodes[0] == x {
			found = true
			// Sanity: the deleted context should be among Nodes[1:].
			sawB := false
			for _, n := range conflict.Nodes[1:] {
				if n == b {
					sawB = true
				}
			}
			if !sawB {
				tt.Fatalf("zombie conflict for X should list b as deleted context, got %v", conflict.Nodes)
			}
		}
	}
	if !found {
		tt.Fatalf("expected zombie conflict for X with deleted context b, got %v", conflicts)
	}
}

func TestDetectConflicts_NoZombieAfterUndelete(tt *testing.T) {
	// Same setup as the zombie test, but undelete b. X should no longer
	// be a zombie because its context edge gets retagged back to live.
	g := store.New()
	a := nid("dc_zomb_undel", 0)
	b := nid("dc_zomb_undel", 1)
	c := nid("dc_zomb_undel", 2)
	g.AddNode(a, []byte("a\n"), ph("dc_zomb_undel"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("dc_zomb_undel"), []t.NodeID{a}, nil)
	g.AddNode(c, []byte("c\n"), ph("dc_zomb_undel"), []t.NodeID{b}, nil)
	x := nid("dc_zomb_undel_x", 0)
	g.AddNode(x, []byte("X\n"), ph("dc_zomb_undel_x"), []t.NodeID{b}, []t.NodeID{c})
	if err := g.DeleteNode(b, ph("dc_zomb_undel_del")); err != nil {
		tt.Fatalf("DeleteNode(b): %v", err)
	}
	if err := g.UndeleteNode(b, ph("dc_zomb_undel_del")); err != nil {
		tt.Fatalf("UndeleteNode(b): %v", err)
	}
	g.ResolvePseudoEdges()

	conflicts := algo.DetectConflicts(g)
	for _, conflict := range conflicts {
		if conflict.Kind == "zombie" {
			tt.Fatalf("did not expect zombie conflict after undelete, got %v", conflict)
		}
	}
}

func TestHasConflicts_Linear(tt *testing.T) {
	g := store.New()
	a := nid("hc_lin", 0)
	g.AddNode(a, []byte("a\n"), ph("hc_lin"), []t.NodeID{t.RootNodeID}, nil)
	if algo.HasConflicts(g) {
		tt.Fatal("linear graph should not have conflicts")
	}
}

func TestHasConflicts_Fork(tt *testing.T) {
	g := store.New()
	a := nid("hc_fork", 0)
	b := nid("hc_fork", 1)
	g.AddNode(a, []byte("a\n"), ph("hc_fork"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("hc_fork"), []t.NodeID{t.RootNodeID}, nil)
	if !algo.HasConflicts(g) {
		tt.Fatal("fork should have conflicts")
	}
}

// A deep linear chain must not blow the stack — the iterative Tarjan should
// handle arbitrary depth bounded only by available memory.
func TestTarjan_DeepChainNoStackOverflow(tt *testing.T) {
	const n = 100_000
	g := store.New()
	prev := t.RootNodeID
	for i := 0; i < n; i++ {
		id := nid("deep", uint64(i))
		if err := g.AddNode(id, []byte("x\n"), ph("deep"), []t.NodeID{prev}, nil); err != nil {
			tt.Fatalf("AddNode %d: %v", i, err)
		}
		prev = id
	}
	sccs := algo.Tarjan(g)
	// Linear chain: every SCC is a singleton; total n+1 (root included).
	if len(sccs) != n+1 {
		tt.Fatalf("expected %d SCCs, got %d", n+1, len(sccs))
	}
}

// A live node unreachable from root must be reported as an "orphan"
// conflict: it breaks the linear order without forking or cycling, so
// without this kind HasConflicts()==true came with an empty conflict
// list and the UI had nothing to show.
func TestDetectConflicts_Orphan(tt *testing.T) {
	g := store.New()
	a := nid("orph", 0)
	if err := g.AddNode(a, []byte("anchored\n"), ph("orph"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	stranded := nid("orph_stranded", 0)
	if err := g.AddNode(stranded, []byte("stranded\n"), ph("orph_stranded"), nil, nil); err != nil {
		tt.Fatal(err)
	}
	g.ResolvePseudoEdges()

	if algo.LinearOrder(g) != nil {
		tt.Fatal("setup: a stranded node must break the linear order")
	}
	found := false
	for _, c := range algo.DetectConflicts(g) {
		if c.Kind == "orphan" && len(c.Nodes) == 1 && c.Nodes[0] == stranded {
			found = true
		}
	}
	if !found {
		tt.Fatalf("expected an orphan conflict for %v, got %v", stranded, algo.DetectConflicts(g))
	}
}

// TopoSort output must not depend on map iteration order: repeated runs
// over the same non-uniquely-ordered graph yield the same sequence.
func TestTopoSort_Deterministic(tt *testing.T) {
	g := store.New()
	// A fork: root -> {x, y} with no order between x and y.
	x := nid("det", 0)
	y := nid("det", 1)
	if err := g.AddNode(x, []byte("x\n"), ph("det"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.AddNode(y, []byte("y\n"), ph("det"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	g.ResolvePseudoEdges()

	first := algo.TopoSort(g)
	for range 32 {
		if got := algo.TopoSort(g); !slices.Equal(got, first) {
			tt.Fatalf("TopoSort not deterministic: %v vs %v", got, first)
		}
	}
}

//go:build llm_generated_opus47

package algo_test

import (
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
package store

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/qc"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// --- Invariants hold on basic states ---

func TestInvariants_EmptyGraggle(tt *testing.T) {
	g := New()
	assertNoInvariantViolations(tt, g)
}

func TestInvariants_SingleNode(tt *testing.T) {
	g := New()
	g.AddNode(nid("inv1", 0), []byte("a\n"), ph("inv1"), []t.NodeID{t.RootNodeID}, nil)
	assertNoInvariantViolations(tt, g)
}

func TestInvariants_LinearChain(tt *testing.T) {
	g := New()
	p := patch.NewPatch("test", "chain", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 2}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}}},
	})
	p.Apply(g)
	assertNoInvariantViolations(tt, g)
}

func TestInvariants_AfterDeletion(tt *testing.T) {
	g := New()
	p := patch.NewPatch("test", "chain", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 2}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}}},
	})
	p.Apply(g)

	// Delete middle node.
	g.DeleteNode(t.NodeID{Patch: p.Hash, Index: 1}, testDeleter)
	g.ResolvePseudoEdges()
	assertNoInvariantViolations(tt, g)
}

func TestInvariants_AfterUndeletion(tt *testing.T) {
	g := New()
	p := patch.NewPatch("test", "chain", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 2}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}}},
	})
	p.Apply(g)

	mid := t.NodeID{Patch: p.Hash, Index: 1}
	g.DeleteNode(mid, testDeleter)
	g.ResolvePseudoEdges()
	g.UndeleteNode(mid, testDeleter)
	g.ResolvePseudoEdges()
	assertNoInvariantViolations(tt, g)
}

func TestInvariants_OrderConflict(tt *testing.T) {
	g := New()
	base := patch.NewPatch("test", "base", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})
	base.Apply(g)

	lineA := t.NodeID{Patch: base.Hash, Index: 0}
	lineC := t.NodeID{Patch: base.Hash, Index: 1}

	p1 := patch.NewPatch("u1", "X", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("X\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})
	p2 := patch.NewPatch("u2", "Y", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("Y\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})
	p1.Apply(g)
	p2.Apply(g)
	assertNoInvariantViolations(tt, g)
}

func TestInvariants_CycleConflict(tt *testing.T) {
	g := New()
	a := nid("inv_cyc", 0)
	b := nid("inv_cyc", 1)
	g.AddNode(a, []byte("a\n"), ph("inv_cyc"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("inv_cyc"), []t.NodeID{a}, nil)
	g.AddEdge(b, a, ph("inv_cyc_back"))
	g.ResolvePseudoEdges()
	assertNoInvariantViolations(tt, g)
}

func TestInvariants_AfterApplyUnapply(tt *testing.T) {
	g := New()
	base := patch.NewPatch("test", "base", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})
	base.Apply(g)
	assertNoInvariantViolations(tt, g)

	p := patch.NewPatch("test", "insert", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("X\n"),
			UpContext: []t.NodeID{{Patch: base.Hash, Index: 0}}, DownContext: []t.NodeID{{Patch: base.Hash, Index: 1}}},
	})
	p.Apply(g)
	assertNoInvariantViolations(tt, g)

	p.Unapply(g)
	assertNoInvariantViolations(tt, g)
}

func TestInvariants_ZombieNode(tt *testing.T) {
	g := New()
	base := patch.NewPatch("test", "base", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 2}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}}},
	})
	base.Apply(g)

	lineB := t.NodeID{Patch: base.Hash, Index: 1}
	lineC := t.NodeID{Patch: base.Hash, Index: 2}

	// Insert X with context b, then delete b (zombie scenario).
	pX := patch.NewPatch("u1", "X after b", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("X\n"),
			UpContext: []t.NodeID{lineB}, DownContext: []t.NodeID{lineC}},
	})
	pDel := patch.NewPatch("u2", "delete b", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindDeleteNode, NodeID: lineB},
	})
	pX.Apply(g)
	pDel.Apply(g)
	assertNoInvariantViolations(tt, g)
}

func TestInvariants_ConflictResolution(tt *testing.T) {
	g := New()
	base := patch.NewPatch("test", "base", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})
	base.Apply(g)

	lineA := t.NodeID{Patch: base.Hash, Index: 0}
	lineC := t.NodeID{Patch: base.Hash, Index: 1}

	p1 := patch.NewPatch("u1", "X", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("X\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})
	p2 := patch.NewPatch("u2", "Y", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("Y\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})
	p1.Apply(g)
	p2.Apply(g)
	assertNoInvariantViolations(tt, g)

	// Resolve conflict.
	lineX := t.NodeID{Patch: p1.Hash, Index: 0}
	lineY := t.NodeID{Patch: p2.Hash, Index: 0}
	res := patch.NewPatch("resolver", "X before Y", []t.PatchHash{p1.Hash, p2.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewEdge, Src: lineX, Dest: lineY},
	})
	res.Apply(g)
	assertNoInvariantViolations(tt, g)
}

// --- Property: Invariants hold under random patch sequences ---

func TestInvariants_RandomPatchSequence(tt *testing.T) {
	for seed := int64(0); seed < 50; seed++ {
		rng := rand.New(rand.NewSource(seed))
		g, base := makeBaseGraggle(3+rng.Intn(5), fmt.Sprintf("rand_inv_%d", seed))
		assertNoInvariantViolations(tt, g)

		lineCount := len(slices.Collect(g.AllLiveNodes())) - 1 // exclude root

		// Apply 3-5 random insert patches.
		for i := 0; i < 3+rng.Intn(3); i++ {
			p := randomInsertPatch(base, rng, fmt.Sprintf("rp_%d_%d", seed, i), lineCount)
			p.Apply(g)
			errs := qc.CheckInvariants(g)
			if len(errs) > 0 {
				tt.Errorf("seed %d, patch %d: %d invariant violations", seed, i, len(errs))
				for _, err := range errs {
					tt.Errorf("  %v", err)
				}
				tt.FailNow()
			}
		}
	}
}

func TestInvariants_RandomDeleteSequence(tt *testing.T) {
	for seed := int64(0); seed < 30; seed++ {
		rng := rand.New(rand.NewSource(seed))
		lineCount := 4 + rng.Intn(4)
		g, base := makeBaseGraggle(lineCount, fmt.Sprintf("rand_del_%d", seed))
		assertNoInvariantViolations(tt, g)

		// Delete 1-3 random non-root nodes.
		nDel := 1 + rng.Intn(min(3, lineCount-1))
		deleted := make(map[int]struct{})
		for d := 0; d < nDel; d++ {
			for {
				idx := rng.Intn(lineCount)
				if _, ok := deleted[idx]; !ok {
					deleted[idx] = struct{}{}
					g.DeleteNode(t.NodeID{Patch: base.Hash, Index: uint64(idx)}, testDeleter)
					break
				}
			}
		}
		g.ResolvePseudoEdges()
		errs := qc.CheckInvariants(g)
		if len(errs) > 0 {
			tt.Errorf("seed %d: %d invariant violations after deleting %d nodes", seed, len(errs), nDel)
			for _, err := range errs {
				tt.Errorf("  %v", err)
			}
			tt.FailNow()
		}
	}
}

func TestInvariants_MergeCommutativity(tt *testing.T) {
	// Check invariants hold in both patch application orders.
	for seed := int64(0); seed < 30; seed++ {
		rng := rand.New(rand.NewSource(seed))
		lineCount := 3 + rng.Intn(4)
		baseSeed := fmt.Sprintf("comm_inv_%d", seed)

		g1, base := makeBaseGraggle(lineCount, baseSeed)
		g2, _ := makeBaseGraggle(lineCount, baseSeed)

		p1 := randomInsertPatch(base, rng, fmt.Sprintf("ci1_%d", seed), lineCount)
		p2 := randomInsertPatch(base, rng, fmt.Sprintf("ci2_%d", seed), lineCount)

		p1.Apply(g1)
		p2.Apply(g1)
		errs1 := qc.CheckInvariants(g1)

		p2.Apply(g2)
		p1.Apply(g2)
		errs2 := qc.CheckInvariants(g2)

		if len(errs1) > 0 {
			tt.Errorf("seed %d order p1,p2: %d violations", seed, len(errs1))
			for _, err := range errs1 {
				tt.Errorf("  %v", err)
			}
		}
		if len(errs2) > 0 {
			tt.Errorf("seed %d order p2,p1: %d violations", seed, len(errs2))
			for _, err := range errs2 {
				tt.Errorf("  %v", err)
			}
		}
		if len(errs1) > 0 || len(errs2) > 0 {
			tt.FailNow()
		}
	}
}

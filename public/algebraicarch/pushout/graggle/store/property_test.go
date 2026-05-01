//go:build llm_generated_opus47

package store

import (
	"bytes"
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/patch"
	"github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/qc"
	t "github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/types"
)

func liveNodeSet(g *Graggle) map[t.NodeID]struct{} {
	set := make(map[t.NodeID]struct{})
	for n := range g.AllLiveNodes() {
		set[n] = struct{}{}
	}
	return set
}

func sameNodeSets(a, b map[t.NodeID]struct{}) bool {
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

func TestProperty_Commutativity(tt *testing.T) {
	for seed := int64(0); seed < 50; seed++ {
		rng := rand.New(rand.NewSource(seed))
		lineCount := 3 + rng.Intn(5)

		g1, base := makeBaseGraggle(lineCount, fmt.Sprintf("comm%d", seed))
		g2, _ := makeBaseGraggle(lineCount, fmt.Sprintf("comm%d", seed))

		p1 := randomInsertPatch(base, rng, fmt.Sprintf("u1_s%d", seed), lineCount)
		p2 := randomInsertPatch(base, rng, fmt.Sprintf("u2_s%d", seed), lineCount)

		// Order 1: p1, p2.
		p1.Apply(g1)
		p2.Apply(g1)

		// Order 2: p2, p1.
		p2.Apply(g2)
		p1.Apply(g2)

		set1 := liveNodeSet(g1)
		set2 := liveNodeSet(g2)
		if !sameNodeSets(set1, set2) {
			tt.Fatalf("seed %d: commutativity violated — different live node sets", seed)
		}
	}
}

func TestProperty_Associativity(tt *testing.T) {
	for seed := int64(0); seed < 30; seed++ {
		rng := rand.New(rand.NewSource(seed))
		lineCount := 3 + rng.Intn(5)

		baseSeed := fmt.Sprintf("assoc%d", seed)
		_, base := makeBaseGraggle(lineCount, baseSeed)

		p1 := randomInsertPatch(base, rng, fmt.Sprintf("a1_s%d", seed), lineCount)
		p2 := randomInsertPatch(base, rng, fmt.Sprintf("a2_s%d", seed), lineCount)
		p3 := randomInsertPatch(base, rng, fmt.Sprintf("a3_s%d", seed), lineCount)

		patches := []*patch.Patch{p1, p2, p3}
		// All 6 permutations.
		perms := [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}

		var sets []map[t.NodeID]struct{}
		for _, perm := range perms {
			g, _ := makeBaseGraggle(lineCount, baseSeed)
			for _, idx := range perm {
				patches[idx].Apply(g)
			}
			sets = append(sets, liveNodeSet(g))
		}

		for i := 1; i < len(sets); i++ {
			if !sameNodeSets(sets[0], sets[i]) {
				tt.Fatalf("seed %d: associativity violated at permutation %d", seed, i)
			}
		}
	}
}

func TestProperty_ApplyUnapplyIdentity(tt *testing.T) {
	for seed := int64(0); seed < 50; seed++ {
		rng := rand.New(rand.NewSource(seed))
		lineCount := 2 + rng.Intn(6)

		g, base := makeBaseGraggle(lineCount, fmt.Sprintf("roundtrip%d", seed))
		originalRender := g.Render()

		p := randomInsertPatch(base, rng, fmt.Sprintf("rt_s%d", seed), lineCount)
		if err := p.Apply(g); err != nil {
			tt.Fatalf("seed %d: apply failed: %v", seed, err)
		}
		if err := p.Unapply(g); err != nil {
			tt.Fatalf("seed %d: unapply failed: %v", seed, err)
		}

		restored := g.Render()
		if !bytes.Equal(originalRender, restored) {
			tt.Fatalf("seed %d: apply/unapply not identity:\n  original: %q\n  restored: %q", seed, originalRender, restored)
		}
	}
}

func TestProperty_PseudoEdgeIdempotent(tt *testing.T) {
	for seed := int64(0); seed < 30; seed++ {
		rng := rand.New(rand.NewSource(seed))
		lineCount := 4 + rng.Intn(4)

		g, base := makeBaseGraggle(lineCount, fmt.Sprintf("pseudo_idem%d", seed))

		// Delete a random middle node.
		delIdx := 1 + rng.Intn(lineCount-1) // avoid first (to keep root connection)
		if delIdx >= lineCount {
			delIdx = lineCount - 1
		}
		nodeID := t.NodeID{Patch: base.Hash, Index: uint64(delIdx)}
		g.DeleteNode(nodeID)
		g.ResolvePseudoEdges()

		r1 := g.Render()
		g.ResolvePseudoEdges() // second call
		r2 := g.Render()

		if !bytes.Equal(r1, r2) {
			tt.Fatalf("seed %d: pseudo-edge resolve not idempotent", seed)
		}
	}
}

func TestProperty_CloneEquivalence(tt *testing.T) {
	for seed := int64(0); seed < 30; seed++ {
		rng := rand.New(rand.NewSource(seed))
		lineCount := 2 + rng.Intn(6)

		g, _ := makeBaseGraggle(lineCount, fmt.Sprintf("clone_eq%d", seed))
		clone := g.Clone()

		origRender := g.Render()
		cloneRender := clone.Render()
		if !bytes.Equal(origRender, cloneRender) {
			tt.Fatalf("seed %d: clone renders differently:\n  orig:  %q\n  clone: %q", seed, origRender, cloneRender)
		}

		origNodes := liveNodeSet(g)
		cloneNodes := liveNodeSet(clone)
		if !sameNodeSets(origNodes, cloneNodes) {
			tt.Fatalf("seed %d: clone has different live node set", seed)
		}
	}
}

// --- Incremental vs. Batch Equivalence ---

func TestProperty_IncrementalVsBatch(tt *testing.T) {
	// Applying N changes as N separate single-change patches must produce
	// the same graggle as applying them all at once in a single patch.
	for seed := int64(0); seed < 40; seed++ {
		rng := rand.New(rand.NewSource(seed))
		lineCount := 3 + rng.Intn(5)
		baseSeed := fmt.Sprintf("incr_batch_%d", seed)

		// Build base.
		gBatch, base := makeBaseGraggle(lineCount, baseSeed)
		gIncr, _ := makeBaseGraggle(lineCount, baseSeed)

		// Generate a batch of insert changes.
		nInserts := 2 + rng.Intn(4)
		var allChanges []patch.Change
		for i := 0; i < nInserts; i++ {
			pos := rng.Intn(lineCount)
			upCtx := t.NodeID{Patch: base.Hash, Index: uint64(pos)}
			var downCtx []t.NodeID
			if pos+1 < lineCount {
				downCtx = []t.NodeID{{Patch: base.Hash, Index: uint64(pos + 1)}}
			}
			c := patch.Change{
				Kind:       patch.ChangeKindNewNode,
				NodeID:     t.NodeID{Patch: t.PlaceholderHash, Index: uint64(i)},
				Content:    []byte(fmt.Sprintf("ins_%d_%d\n", seed, i)),
				UpContext:  []t.NodeID{upCtx},
				DownContext: downCtx,
			}
			allChanges = append(allChanges, c)
		}

		// Batch: apply all at once.
		pBatch := patch.NewPatch("batch", baseSeed, []t.PatchHash{base.Hash}, allChanges)
		if err := pBatch.Apply(gBatch); err != nil {
			tt.Fatalf("seed %d: batch apply failed: %v", seed, err)
		}

		// Incremental: apply each change as its own patch.
		for i, c := range allChanges {
			// Fix up placeholder to use unique per-change hash.
			singleChange := patch.Change{
				Kind:       c.Kind,
				NodeID:     t.NodeID{Patch: t.PlaceholderHash, Index: 0},
				Content:    c.Content,
				UpContext:  c.UpContext,
				DownContext: c.DownContext,
			}
			pSingle := patch.NewPatch(fmt.Sprintf("incr_%d", i), baseSeed, []t.PatchHash{base.Hash}, []patch.Change{singleChange})
			if err := pSingle.Apply(gIncr); err != nil {
				tt.Fatalf("seed %d, change %d: incremental apply failed: %v", seed, i, err)
			}
		}

		// Both should have the same set of live nodes (same count).
		batchNodes := liveNodeSet(gBatch)
		incrNodes := liveNodeSet(gIncr)
		if len(batchNodes) != len(incrNodes) {
			tt.Fatalf("seed %d: different node counts: batch=%d, incremental=%d",
				seed, len(batchNodes), len(incrNodes))
		}

		// Both should pass invariants.
		if errs := qc.CheckInvariants(gBatch); len(errs) > 0 {
			tt.Fatalf("seed %d: batch invariant violations: %v", seed, errs)
		}
		if errs := qc.CheckInvariants(gIncr); len(errs) > 0 {
			tt.Fatalf("seed %d: incremental invariant violations: %v", seed, errs)
		}
	}
}

// --- Random Mixed Change Sequences ---

// randomChange generates a random applicable change against a graggle.
// Returns the change and true, or zero-value and false if no valid change could be generated.
func randomChange(g *Graggle, rng *rand.Rand, label string, nodeIdx *uint64) (patch.Change, bool) {
	liveNodes := slices.Collect(g.AllLiveNodes())
	if len(liveNodes) == 0 {
		return patch.Change{}, false
	}

	// Weighted choice: 50% insert, 30% delete, 20% new edge.
	roll := rng.Intn(100)
	switch {
	case roll < 50:
		// Insert: pick a random live node as up-context.
		upIdx := rng.Intn(len(liveNodes))
		upCtx := liveNodes[upIdx]
		idx := *nodeIdx
		*nodeIdx++
		return patch.Change{
			Kind:      patch.ChangeKindNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: idx},
			Content:   []byte(fmt.Sprintf("%s_%d\n", label, idx)),
			UpContext: []t.NodeID{upCtx},
		}, true

	case roll < 80:
		// Delete: pick a random non-root live node.
		var candidates []t.NodeID
		for _, n := range liveNodes {
			if n != t.RootNodeID {
				candidates = append(candidates, n)
			}
		}
		if len(candidates) == 0 {
			return patch.Change{}, false
		}
		target := candidates[rng.Intn(len(candidates))]
		return patch.Change{
			Kind:   patch.ChangeKindDeleteNode,
			NodeID: target,
		}, true

	default:
		// New edge: pick two random live nodes with no existing live edge between them.
		if len(liveNodes) < 2 {
			return patch.Change{}, false
		}
		for attempt := 0; attempt < 10; attempt++ {
			i := rng.Intn(len(liveNodes))
			j := rng.Intn(len(liveNodes))
			if i == j {
				continue
			}
			src, dest := liveNodes[i], liveNodes[j]
			if !g.edges.HasLiveEdgeTo(src, dest) {
				return patch.Change{
					Kind: patch.ChangeKindNewEdge,
					Src:  src,
					Dest: dest,
				}, true
			}
		}
		return patch.Change{}, false
	}
}

func TestProperty_RandomMixedSequence(tt *testing.T) {
	// Generate random sequences of inserts, deletes, and edge additions.
	// After each operation, check invariants.
	for seed := int64(0); seed < 50; seed++ {
		rng := rand.New(rand.NewSource(seed))
		lineCount := 3 + rng.Intn(4)
		g, _ := makeBaseGraggle(lineCount, fmt.Sprintf("mixed_%d", seed))

		var nodeIdx uint64 = uint64(lineCount)
		nOps := 5 + rng.Intn(10)

		for op := 0; op < nOps; op++ {
			c, ok := randomChange(g, rng, fmt.Sprintf("m%d", seed), &nodeIdx)
			if !ok {
				continue
			}

			switch c.Kind {
			case patch.ChangeKindNewNode:
				p := patch.NewPatch(fmt.Sprintf("op_%d", op), "insert", nil, []patch.Change{c})
				p.Apply(g)
			case patch.ChangeKindDeleteNode:
				g.DeleteNode(c.NodeID)
				g.ResolvePseudoEdges()
			case patch.ChangeKindNewEdge:
				g.AddEdge(c.Src, c.Dest, ph(fmt.Sprintf("edge_%d_%d", seed, op)))
				g.ResolvePseudoEdges()
			}

			errs := qc.CheckInvariants(g)
			if len(errs) > 0 {
				tt.Errorf("seed %d, op %d (kind=%d): %d invariant violations",
					seed, op, c.Kind, len(errs))
				for _, err := range errs {
					tt.Errorf("  %v", err)
				}
				tt.FailNow()
			}
		}
	}
}

func TestProperty_RandomMixedSequenceCommutativity(tt *testing.T) {
	// Generate two independent patches from a random base, apply in both orders,
	// verify same live node set. Uses mixed operations (not just inserts).
	for seed := int64(0); seed < 30; seed++ {
		rng := rand.New(rand.NewSource(seed))
		lineCount := 3 + rng.Intn(4)
		baseSeed := fmt.Sprintf("mixed_comm_%d", seed)

		g1, base := makeBaseGraggle(lineCount, baseSeed)
		g2, _ := makeBaseGraggle(lineCount, baseSeed)

		// Generate two independent insert patches (inserts always commute
		// when they reference only base nodes as context).
		p1 := randomInsertPatch(base, rng, fmt.Sprintf("mc1_%d", seed), lineCount)
		p2 := randomInsertPatch(base, rng, fmt.Sprintf("mc2_%d", seed), lineCount)

		p1.Apply(g1)
		p2.Apply(g1)
		if errs := qc.CheckInvariants(g1); len(errs) > 0 {
			tt.Fatalf("seed %d order p1,p2: invariant violations: %v", seed, errs)
		}

		p2.Apply(g2)
		p1.Apply(g2)
		if errs := qc.CheckInvariants(g2); len(errs) > 0 {
			tt.Fatalf("seed %d order p2,p1: invariant violations: %v", seed, errs)
		}

		if !sameNodeSets(liveNodeSet(g1), liveNodeSet(g2)) {
			tt.Fatalf("seed %d: commutativity violated with invariant checks", seed)
		}
	}
}

func TestProperty_ApplyUnapplyReapply(tt *testing.T) {
	// Apply, unapply, reapply — the ojo triple roundtrip pattern.
	// State after apply(1) should equal state after apply(1), unapply(1), apply(1).
	for seed := int64(0); seed < 40; seed++ {
		rng := rand.New(rand.NewSource(seed))
		lineCount := 3 + rng.Intn(5)

		g1, base := makeBaseGraggle(lineCount, fmt.Sprintf("reapply_%d", seed))
		g2, _ := makeBaseGraggle(lineCount, fmt.Sprintf("reapply_%d", seed))

		p := randomInsertPatch(base, rng, fmt.Sprintf("ra_%d", seed), lineCount)

		// g1: just apply once.
		p.Apply(g1)
		render1 := g1.Render()

		// g2: apply, unapply, reapply.
		p.Apply(g2)
		p.Unapply(g2)
		p.Apply(g2)
		render2 := g2.Render()

		if !bytes.Equal(render1, render2) {
			tt.Fatalf("seed %d: apply != apply+unapply+apply:\n  once:  %q\n  triple: %q",
				seed, render1, render2)
		}

		if errs := qc.CheckInvariants(g2); len(errs) > 0 {
			tt.Fatalf("seed %d: invariant violations after reapply: %v", seed, errs)
		}
	}
}

func TestProperty_GhostMonotonicity(tt *testing.T) {
	// Once deleted, a node stays deleted unless explicitly undeleted.
	for seed := int64(0); seed < 30; seed++ {
		rng := rand.New(rand.NewSource(seed))
		lineCount := 4 + rng.Intn(4)

		g, base := makeBaseGraggle(lineCount, fmt.Sprintf("ghost%d", seed))

		// Delete a random node.
		delIdx := 1 + rng.Intn(lineCount-1)
		if delIdx >= lineCount {
			delIdx = lineCount - 1
		}
		deletedID := t.NodeID{Patch: base.Hash, Index: uint64(delIdx)}
		g.DeleteNode(deletedID)
		g.ResolvePseudoEdges()

		if !g.IsDeleted(deletedID) {
			tt.Fatalf("seed %d: node should be deleted", seed)
		}

		// Apply another insert patch — deleted node should remain deleted.
		p2 := randomInsertPatch(base, rng, fmt.Sprintf("ghost_add_s%d", seed), lineCount)
		p2.Apply(g)

		if !g.IsDeleted(deletedID) {
			tt.Fatalf("seed %d: ghost monotonicity violated — deleted node became live after additional patch", seed)
		}
	}
}
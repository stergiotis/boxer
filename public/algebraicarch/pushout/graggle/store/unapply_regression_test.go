// Regression tests for the Unapply-direction defects found in the
// 2026-06-11 review: tombstone resurrection under convergent deletes,
// the delete-only-dependent pre-flight hole, and pseudo-edge state not
// being restored when a shadowing live edge is removed. Each test is the
// in-repo port of an executable repro that failed before the fix.
package store

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/algo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

func plc(idx uint64) t.NodeID { return t.NodeID{Patch: t.PlaceholderHash, Index: idx} }

// Two patches delete the same node (the convergent-edit case DeleteNode
// idempotency exists for). Unapplying ONE of them must NOT resurrect the
// node — the other deleting patch is still applied. Unapplying both must.
func TestUnapply_DoubleDeleteKeepsTombstone(tt *testing.T) {
	g := New()
	base := patch.NewPatch("base", "add A", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: plc(0), Content: []byte("k \"v1\"\n"), UpContext: []t.NodeID{t.RootNodeID}},
	})
	if err := base.Apply(g); err != nil {
		tt.Fatal(err)
	}
	a := t.NodeID{Patch: base.Hash, Index: 0}

	p1 := patch.NewPatch("alice", "edit", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindDeleteNode, NodeID: a},
		{Kind: patch.ChangeKindNewNode, NodeID: plc(0), Content: []byte("k \"alice\"\n"), UpContext: []t.NodeID{t.RootNodeID}},
	})
	p2 := patch.NewPatch("bob", "edit", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindDeleteNode, NodeID: a},
		{Kind: patch.ChangeKindNewNode, NodeID: plc(0), Content: []byte("k \"bob\"\n"), UpContext: []t.NodeID{t.RootNodeID}},
	})
	if err := p1.Apply(g); err != nil {
		tt.Fatal(err)
	}
	if err := p2.Apply(g); err != nil {
		tt.Fatal(err)
	}
	if got := g.NodeDeleterCount(a); got != 2 {
		tt.Fatalf("expected 2 recorded deleters for A, got %d", got)
	}

	if err := p2.Unapply(g); err != nil {
		tt.Fatalf("unapply p2: %v", err)
	}
	if g.IsLive(a) {
		tt.Fatal("node A resurrected by unapplying p2 although p1 (still applied) also deleted it")
	}
	if got := g.NodeDeleterCount(a); got != 1 {
		tt.Fatalf("expected 1 remaining deleter for A, got %d", got)
	}
	assertNoInvariantViolations(tt, g)

	if err := p1.Unapply(g); err != nil {
		tt.Fatalf("unapply p1: %v", err)
	}
	if !g.IsLive(a) {
		tt.Fatal("node A must be live again after both deleting patches are unapplied")
	}
	if string(g.NodeContent(a)) != "k \"v1\"\n" {
		tt.Fatalf("node A content lost: %q", g.NodeContent(a))
	}
	assertNoInvariantViolations(tt, g)
}

// A patch that only DELETES another patch's node contributes no edges, so
// the foreign-edge pre-flight alone cannot see it. Unapplying the
// node-introducing patch while the deleting patch is applied must be
// rejected; the reverse order must fully restore the empty state.
func TestUnapply_RefusesWhenDeleteOnlyDependentApplied(tt *testing.T) {
	g := New()
	base := patch.NewPatch("alice", "add A", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: plc(0), Content: []byte("line1\n"), UpContext: []t.NodeID{t.RootNodeID}},
	})
	if err := base.Apply(g); err != nil {
		tt.Fatal(err)
	}
	a := t.NodeID{Patch: base.Hash, Index: 0}
	del := patch.NewPatch("bob", "delete A", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindDeleteNode, NodeID: a},
	})
	if err := del.Apply(g); err != nil {
		tt.Fatal(err)
	}

	err := base.Unapply(g)
	if err == nil {
		tt.Fatal("Unapply(base) succeeded although dependent patch del is still applied")
	}
	if !strings.Contains(err.Error(), "tombstoned by another still-applied patch") {
		tt.Fatalf("unexpected rejection reason: %v", err)
	}
	// The rejected unapply must leave the state untouched.
	if !g.IsDeleted(a) || string(g.NodeContent(a)) != "line1\n" {
		tt.Fatalf("rejected Unapply mutated state: deleted=%v content=%q", g.IsDeleted(a), g.NodeContent(a))
	}
	assertNoInvariantViolations(tt, g)

	// Correct order: dependents first.
	if err := del.Unapply(g); err != nil {
		tt.Fatalf("unapply del: %v", err)
	}
	if !g.IsLive(a) {
		tt.Fatal("A must be live after unapplying its deleter")
	}
	if err := base.Unapply(g); err != nil {
		tt.Fatalf("unapply base: %v", err)
	}
	if g.HasNode(a) {
		tt.Fatal("A must be gone after unapplying its introducing patch")
	}
	assertNoInvariantViolations(tt, g)
}

// A live edge that duplicates a pseudo-edge removes the pseudo-edge on
// apply; unapplying the edge patch must re-mark the deleted component
// dirty so the pseudo-edge is restored and the live subgraph keeps its
// connectivity and order.
func TestUnapply_NewEdgeRestoresShadowedPseudoEdge(tt *testing.T) {
	g := New()
	base := patch.NewPatch("alice", "A,B,C", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: plc(0), Content: []byte("A\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: plc(1), Content: []byte("B\n"), UpContext: []t.NodeID{plc(0)}},
		{Kind: patch.ChangeKindNewNode, NodeID: plc(2), Content: []byte("C\n"), UpContext: []t.NodeID{plc(1)}},
	})
	if err := base.Apply(g); err != nil {
		tt.Fatal(err)
	}
	nA := t.NodeID{Patch: base.Hash, Index: 0}
	nB := t.NodeID{Patch: base.Hash, Index: 1}
	nC := t.NodeID{Patch: base.Hash, Index: 2}

	del := patch.NewPatch("bob", "del B", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindDeleteNode, NodeID: nB},
	})
	if err := del.Apply(g); err != nil {
		tt.Fatal(err)
	}
	if order := algo.LinearOrder(g); len(order) != 3 {
		tt.Fatalf("setup: expected root,A,C linear order via pseudo-edge, got %v", order)
	}

	edge := patch.NewPatch("carol", "edge A->C", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewEdge, Src: nA, Dest: nC},
	})
	if err := edge.Apply(g); err != nil {
		tt.Fatal(err)
	}
	if order := algo.LinearOrder(g); len(order) != 3 {
		tt.Fatalf("after live edge: expected linear order, got %v", order)
	}

	if err := edge.Unapply(g); err != nil {
		tt.Fatalf("unapply edge: %v", err)
	}
	if order := algo.LinearOrder(g); order == nil {
		tt.Fatal("after Unapply(edge) the graggle lost its linear order — pseudo-edge A->C not restored")
	}
	assertNoInvariantViolations(tt, g)
}

// canonicalObservableState projects the graggle onto its observable
// state: live nodes with contents, deleted nodes with their deleter
// sets, and the full edge multiset. Partition representatives, reason
// bookkeeping, and tombstone wall-clock times are deliberately excluded —
// they may differ structurally between construction orders while being
// semantically equivalent (their internal consistency is what
// qc.CheckInvariants verifies).
func canonicalObservableState(g *Graggle) string {
	var sb strings.Builder
	sb.WriteString("live:\n")
	for id := range g.AllLiveNodes() {
		fmt.Fprintf(&sb, "  %v %q\n", id, g.NodeContent(id))
	}
	sb.WriteString("deleted:\n")
	for id := range g.AllDeletedNodes() {
		fmt.Fprintf(&sb, "  %v %q deleters=%s\n", id, g.NodeContent(id), deleterList(g.deleters[id]))
	}
	sb.WriteString("edges:\n")
	var lines []string
	for _, src := range g.edges.Sources() {
		for _, e := range g.edges.Get(src) {
			lines = append(lines, fmt.Sprintf("  %v -%s-> %v by=%s\n", src, e.Kind, e.Dest, e.IntroducedBy))
		}
	}
	sort.Strings(lines)
	for _, l := range lines {
		sb.WriteString(l)
	}
	return sb.String()
}

// Inverse law: unapplying a suffix of the applied sequence in reverse
// order must leave exactly the state produced by replaying the prefix on
// a fresh graggle. This is the property the Unrecord workflow depends on.
func TestProperty_UnapplySuffixEqualsReplayPrefix(tt *testing.T) {
	for seed := int64(0); seed < 25; seed++ {
		rng := rand.New(rand.NewSource(seed))
		g := New()
		base := patch.NewPatch("test", "base", nil, []patch.Change{
			{Kind: patch.ChangeKindNewNode, NodeID: plc(0), Content: []byte("l0\n"), UpContext: []t.NodeID{t.RootNodeID}},
			{Kind: patch.ChangeKindNewNode, NodeID: plc(1), Content: []byte("l1\n"), UpContext: []t.NodeID{plc(0)}},
			{Kind: patch.ChangeKindNewNode, NodeID: plc(2), Content: []byte("l2\n"), UpContext: []t.NodeID{plc(1)}},
		})
		if err := base.Apply(g); err != nil {
			tt.Fatal(err)
		}
		applied := []*patch.Patch{base}

		liveNonRoot := func() (out []t.NodeID) {
			for id := range g.AllLiveNodes() {
				if id != t.RootNodeID {
					out = append(out, id)
				}
			}
			return
		}
		steps := 4 + rng.Intn(5)
		for k := 0; k < steps; k++ {
			live := liveNonRoot()
			if rng.Float64() < 0.65 || len(live) == 0 {
				up := t.RootNodeID
				if len(live) > 0 {
					up = live[rng.Intn(len(live))]
				}
				changes := []patch.Change{{
					Kind:      patch.ChangeKindNewNode,
					NodeID:    plc(0),
					Content:   []byte(fmt.Sprintf("seed%d_step%d\n", seed, k)),
					UpContext: []t.NodeID{up},
				}}
				p := patch.NewPatch("test", fmt.Sprintf("ins%d", k), patch.ComputeDependencies(changes), changes)
				if err := p.Apply(g); err != nil {
					tt.Fatalf("seed %d step %d insert: %v", seed, k, err)
				}
				applied = append(applied, p)
			} else {
				victim := live[rng.Intn(len(live))]
				changes := []patch.Change{{Kind: patch.ChangeKindDeleteNode, NodeID: victim}}
				p := patch.NewPatch("test", fmt.Sprintf("del%d", k), patch.ComputeDependencies(changes), changes)
				if err := p.Apply(g); err != nil {
					tt.Fatalf("seed %d step %d delete: %v", seed, k, err)
				}
				applied = append(applied, p)
			}
		}
		assertNoInvariantViolations(tt, g)

		cut := rng.Intn(len(applied) + 1)
		for i := len(applied) - 1; i >= cut; i-- {
			if err := applied[i].Unapply(g); err != nil {
				tt.Fatalf("seed %d: unapply #%d (%s): %v", seed, i, applied[i].Description, err)
			}
			assertNoInvariantViolations(tt, g)
		}

		g2 := New()
		for i := 0; i < cut; i++ {
			if err := applied[i].Apply(g2); err != nil {
				tt.Fatalf("seed %d: replay #%d: %v", seed, i, err)
			}
		}
		got, want := canonicalObservableState(g), canonicalObservableState(g2)
		if got != want {
			tt.Fatalf("seed %d cut %d: unapply-suffix state diverges from replay-prefix state\n--- unapplied:\n%s\n--- replayed:\n%s", seed, cut, got, want)
		}
	}
}

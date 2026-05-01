//go:build llm_generated_opus47

package store

import (
	"slices"
	"testing"

	"github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/algo"
	"github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/types"
)

// --- Integration: Complex Pseudo-Edge Scenarios ---

func TestPseudoEdge_BranchingDeletion(tt *testing.T) {
	// root -> a -> {b, c} -> d. Delete a.
	// Should create pseudo-edges root -> b and root -> c.
	g := New()
	a := nid("pe_branch", 0)
	b := nid("pe_branch", 1)
	c := nid("pe_branch", 2)
	d := nid("pe_branch", 3)

	g.AddNode(a, []byte("a\n"), ph("pe_branch"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("pe_branch"), []t.NodeID{a}, []t.NodeID{})
	g.AddNode(c, []byte("c\n"), ph("pe_branch"), []t.NodeID{a}, []t.NodeID{})
	g.AddNode(d, []byte("d\n"), ph("pe_branch"), []t.NodeID{b, c}, nil)

	g.DeleteNode(a)
	g.ResolvePseudoEdges()

	// root should now reach b and c.
	childSet := make(map[t.NodeID]struct{})
	for ch := range g.LiveChildren(t.RootNodeID) {
		childSet[ch] = struct{}{}
	}
	if _, ok := childSet[b]; !ok {
		tt.Fatal("expected pseudo-edge root -> b")
	}
	if _, ok := childSet[c]; !ok {
		tt.Fatal("expected pseudo-edge root -> c")
	}
}

func TestPseudoEdge_IdempotentResolve(tt *testing.T) {
	g := New()
	a := nid("pe_idem", 0)
	b := nid("pe_idem", 1)
	c := nid("pe_idem", 2)
	g.AddNode(a, []byte("a\n"), ph("pe_idem"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("pe_idem"), []t.NodeID{a}, nil)
	g.AddNode(c, []byte("c\n"), ph("pe_idem"), []t.NodeID{b}, nil)

	g.DeleteNode(b)
	g.ResolvePseudoEdges()

	r1 := string(g.Render())

	// Second resolve should be a no-op.
	g.ResolvePseudoEdges()
	r2 := string(g.Render())

	if r1 != r2 {
		tt.Fatalf("double resolve changed output: %q vs %q", r1, r2)
	}
}

func TestPseudoEdge_MultipleUndeletions(tt *testing.T) {
	// root -> a -> b -> c -> d -> e. Delete b, c, d.
	// Then undelete c (splits the deleted component).
	g := New()
	a := nid("pe_multi", 0)
	b := nid("pe_multi", 1)
	c := nid("pe_multi", 2)
	d := nid("pe_multi", 3)
	e := nid("pe_multi", 4)
	g.AddNode(a, []byte("a\n"), ph("pe_multi"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("pe_multi"), []t.NodeID{a}, nil)
	g.AddNode(c, []byte("c\n"), ph("pe_multi"), []t.NodeID{b}, nil)
	g.AddNode(d, []byte("d\n"), ph("pe_multi"), []t.NodeID{c}, nil)
	g.AddNode(e, []byte("e\n"), ph("pe_multi"), []t.NodeID{d}, nil)

	g.DeleteNode(b)
	g.DeleteNode(c)
	g.DeleteNode(d)
	g.ResolvePseudoEdges()

	// Should be root -> a -> e.
	order := algo.LinearOrder(g)
	if order == nil {
		tt.Fatal("expected linear order after triple delete")
	}

	// Undelete c (splits deleted component {b,c,d} into {b} and {d}).
	g.UndeleteNode(c)
	g.ResolvePseudoEdges()

	// After undeletion, the live nodes should be: root, a, c, e.
	liveNodes := slices.Collect(g.AllLiveNodes())
	if len(liveNodes) != 4 {
		tt.Fatalf("expected 4 live nodes, got %d", len(liveNodes))
	}
	if !g.IsLive(c) {
		tt.Fatal("c should be live after undelete")
	}

	// New pseudo-edges a->c (over {b}) and c->e (over {d}) must be present
	// so the live subgraph is linearly ordered as a -> c -> e.
	rendered := string(g.Render())
	if rendered != "a\nc\ne\n" {
		tt.Fatalf("expected linear render after split, got %q", rendered)
	}
	assertNoInvariantViolations(tt, g)
}

// --- Pseudo-Edge Multi-Reason Tests ---

func TestPseudoEdge_TwoReasons(tt *testing.T) {
	// Two separate deleted components both justify the same pseudo-edge.
	//
	//   root -> a -> b -> c -> d -> e
	//
	// Delete b (component {b}): pseudo-edge a -> c.
	// Delete d (component {d}): pseudo-edge c -> e.
	// Now also delete c: components {b} and {d} merge with {c} into {b,c,d}.
	// The single pseudo-edge a -> e should exist, justified by the merged component.
	g := New()
	a := nid("pe_2r", 0)
	b := nid("pe_2r", 1)
	c := nid("pe_2r", 2)
	d := nid("pe_2r", 3)
	e := nid("pe_2r", 4)
	g.AddNode(a, []byte("a\n"), ph("pe_2r"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("pe_2r"), []t.NodeID{a}, nil)
	g.AddNode(c, []byte("c\n"), ph("pe_2r"), []t.NodeID{b}, nil)
	g.AddNode(d, []byte("d\n"), ph("pe_2r"), []t.NodeID{c}, nil)
	g.AddNode(e, []byte("e\n"), ph("pe_2r"), []t.NodeID{d}, nil)

	g.DeleteNode(b)
	g.ResolvePseudoEdges()
	assertNoInvariantViolations(tt, g)

	g.DeleteNode(d)
	g.ResolvePseudoEdges()
	assertNoInvariantViolations(tt, g)

	// a -> c (over {b}) and c -> e (over {d}) should exist.
	order := algo.LinearOrder(g)
	if order == nil {
		tt.Fatal("expected linear order: root, a, c, e")
	}

	// Now delete c — merges all into one component {b,c,d}.
	g.DeleteNode(c)
	g.ResolvePseudoEdges()
	assertNoInvariantViolations(tt, g)

	// Should have pseudo-edge a -> e.
	order = algo.LinearOrder(g)
	if order == nil {
		tt.Fatal("expected linear order: root, a, e")
	}
	if len(order) != 3 {
		tt.Fatalf("expected 3 live nodes, got %d", len(order))
	}
}

func TestPseudoEdge_DoubleReason(tt *testing.T) {
	// A single pseudo-edge justified by two independent deleted components.
	//
	//   root -> a -> b1 -> c
	//            \-> b2 -/
	//
	// Delete both b1 and b2 (separate components). Both justify pseudo-edge a -> c.
	g := New()
	a := nid("pe_dr", 0)
	b1 := nid("pe_dr", 1)
	b2 := nid("pe_dr", 2)
	c := nid("pe_dr", 3)
	g.AddNode(a, []byte("a\n"), ph("pe_dr"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b1, []byte("b1\n"), ph("pe_dr"), []t.NodeID{a}, nil)
	g.AddNode(b2, []byte("b2\n"), ph("pe_dr"), []t.NodeID{a}, nil)
	g.AddNode(c, []byte("c\n"), ph("pe_dr"), []t.NodeID{b1, b2}, nil)

	g.DeleteNode(b1)
	g.ResolvePseudoEdges()
	assertNoInvariantViolations(tt, g)

	g.DeleteNode(b2)
	g.ResolvePseudoEdges()
	assertNoInvariantViolations(tt, g)

	// Both deleted components should justify pseudo-edge a -> c.
	pe := pseudoEdge{Src: a, Dest: c}
	reasons, ok := g.pseudoEdgeReasons[pe]
	if !ok {
		tt.Fatal("expected pseudo-edge a -> c to exist")
	}
	if len(reasons) < 2 {
		tt.Fatalf("expected at least 2 reasons for pseudo-edge a->c, got %d", len(reasons))
	}

	// Undelete b1 — pseudo-edge should survive because b2 still justifies it.
	g.UndeleteNode(b1)
	g.ResolvePseudoEdges()
	assertNoInvariantViolations(tt, g)

	// a -> c pseudo-edge should still exist (justified by {b2}).
	found := false
	for _, edge := range g.edges.Get(a) {
		if edge.Dest == c && edge.Kind == t.EdgePseudo {
			found = true
		}
	}
	if !found {
		tt.Fatal("pseudo-edge a->c should survive after undeleting b1 (b2 still justifies it)")
	}

	// Undelete b2 — now no reason remains, pseudo-edge should be removed.
	g.UndeleteNode(b2)
	g.ResolvePseudoEdges()
	assertNoInvariantViolations(tt, g)

	for _, edge := range g.edges.Get(a) {
		if edge.Dest == c && edge.Kind == t.EdgePseudo {
			tt.Fatal("pseudo-edge a->c should be gone after undeleting both b1 and b2")
		}
	}
}

func TestPseudoEdge_ReasonSurvivesPartialUndelete(tt *testing.T) {
	// Chain: root -> a -> x -> y -> b. Delete x and y.
	// Pseudo-edge a -> b justified by component {x,y}.
	// Undelete x: component splits to {y}. Pseudo-edge a -> x (live) is not needed,
	// but x -> b (over {y}) should be created.
	g := New()
	a := nid("pe_partial", 0)
	x := nid("pe_partial", 1)
	y := nid("pe_partial", 2)
	b := nid("pe_partial", 3)
	g.AddNode(a, []byte("a\n"), ph("pe_partial"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(x, []byte("x\n"), ph("pe_partial"), []t.NodeID{a}, nil)
	g.AddNode(y, []byte("y\n"), ph("pe_partial"), []t.NodeID{x}, nil)
	g.AddNode(b, []byte("b\n"), ph("pe_partial"), []t.NodeID{y}, nil)

	g.DeleteNode(x)
	g.DeleteNode(y)
	g.ResolvePseudoEdges()
	assertNoInvariantViolations(tt, g)

	// Should have pseudo-edge a -> b.
	order := algo.LinearOrder(g)
	if order == nil || len(order) != 3 {
		tt.Fatalf("expected linear order [root, a, b], got %v", order)
	}

	// Undelete x.
	g.UndeleteNode(x)
	g.ResolvePseudoEdges()

	// Now live nodes: root, a, x, b. Deleted: {y}.
	// Should have pseudo-edge x -> b (over {y}).
	if !g.IsLive(x) {
		tt.Fatal("x should be live")
	}

	// Check invariants.
	assertNoInvariantViolations(tt, g)
}

// --- Integration: Full Roundtrip ---

func TestRoundtrip_DiffApplyRender(tt *testing.T) {
	// Start with a file, diff against new content, apply, verify render.
	g := New()
	base := patch.NewPatch("test", "initial", nil, []patch.Change{
		{Kind: patch.ChangeNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("hello\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("world\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})
	base.Apply(g)

	// Current state.
	oldIDs := []t.NodeID{
		{Patch: base.Hash, Index: 0},
		{Patch: base.Hash, Index: 1},
	}
	oldContents := [][]byte{[]byte("hello\n"), []byte("world\n")}
	newLines := [][]byte{[]byte("hello\n"), []byte("beautiful\n"), []byte("world\n")}

	diff := patch.LineDiff(oldIDs, oldContents, newLines)
	p := patch.NewPatch("test", "add beautiful", []t.PatchHash{base.Hash}, diff.Changes)
	if err := p.Apply(g); err != nil {
		tt.Fatal(err)
	}

	rendered := string(g.Render())
	if rendered != "hello\nbeautiful\nworld\n" {
		tt.Fatalf("expected 'hello\\nbeautiful\\nworld\\n', got %q", rendered)
	}
}

func TestRoundtrip_DiffApplyUnapply(tt *testing.T) {
	g := New()
	base := patch.NewPatch("test", "initial", nil, []patch.Change{
		{Kind: patch.ChangeNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})
	base.Apply(g)

	originalRender := string(g.Render())

	oldIDs := []t.NodeID{
		{Patch: base.Hash, Index: 0},
		{Patch: base.Hash, Index: 1},
	}
	oldContents := [][]byte{[]byte("a\n"), []byte("b\n")}
	newLines := [][]byte{[]byte("a\n"), []byte("X\n"), []byte("b\n")}

	diff := patch.LineDiff(oldIDs, oldContents, newLines)
	p := patch.NewPatch("test", "insert X", []t.PatchHash{base.Hash}, diff.Changes)
	p.Apply(g)

	// Verify change applied.
	if string(g.Render()) == originalRender {
		tt.Fatal("render should have changed after apply")
	}

	// Unapply and verify restoration.
	if err := p.Unapply(g); err != nil {
		tt.Fatal(err)
	}
	restored := string(g.Render())
	if restored != originalRender {
		tt.Fatalf("unapply should restore original: expected %q, got %q", originalRender, restored)
	}
}
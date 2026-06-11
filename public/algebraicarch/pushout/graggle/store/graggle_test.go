//go:build llm_generated_opus47

package store

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/algo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// --- Basic Graggle Operations ---

func TestNewGraggle(tt *testing.T) {
	g := New()
	if !g.IsLive(t.RootNodeID) {
		tt.Fatal("root node should be live")
	}
	if g.nodes.Len() != 1 {
		tt.Fatalf("expected 1 node, got %d", g.nodes.Len())
	}
}

func TestAddNode(tt *testing.T) {
	g := New()
	id := nid("p1", 0)
	err := g.AddNode(id, []byte("hello\n"), ph("p1"), []t.NodeID{t.RootNodeID}, nil)
	if err != nil {
		tt.Fatal(err)
	}
	if !g.IsLive(id) {
		tt.Fatal("added node should be live")
	}
	if string(g.NodeContent(id)) != "hello\n" {
		tt.Fatalf("content mismatch: %q", string(g.NodeContent(id)))
	}
	// Check edge: root -> id.
	children := slices.Collect(g.LiveChildren(t.RootNodeID))
	if len(children) != 1 || children[0] != id {
		tt.Fatalf("expected root -> id edge, got %v", children)
	}
}

func TestAddNodeDuplicate(tt *testing.T) {
	g := New()
	id := nid("p1", 0)
	g.AddNode(id, []byte("a"), ph("p1"), []t.NodeID{t.RootNodeID}, nil)
	err := g.AddNode(id, []byte("b"), ph("p1"), []t.NodeID{t.RootNodeID}, nil)
	if err == nil {
		tt.Fatal("expected error for duplicate node")
	}
}

func TestDeleteNode(tt *testing.T) {
	g := New()
	id := nid("p1", 0)
	g.AddNode(id, []byte("hello\n"), ph("p1"), []t.NodeID{t.RootNodeID}, nil)
	err := g.DeleteNode(id, testDeleter)
	if err != nil {
		tt.Fatal(err)
	}
	if g.IsLive(id) {
		tt.Fatal("deleted node should not be live")
	}
	if !g.IsDeleted(id) {
		tt.Fatal("node should be in deleted set")
	}
}

func TestDeleteRootFails(tt *testing.T) {
	g := New()
	err := g.DeleteNode(t.RootNodeID, testDeleter)
	if err == nil {
		tt.Fatal("should not be able to delete root")
	}
}

func TestUndeleteNode(tt *testing.T) {
	g := New()
	id := nid("p1", 0)
	g.AddNode(id, []byte("hello\n"), ph("p1"), []t.NodeID{t.RootNodeID}, nil)
	g.DeleteNode(id, testDeleter)
	err := g.UndeleteNode(id, testDeleter)
	if err != nil {
		tt.Fatal(err)
	}
	if !g.IsLive(id) {
		tt.Fatal("undeleted node should be live")
	}
	if g.IsDeleted(id) {
		tt.Fatal("undeleted node should not be in deleted set")
	}
}

// --- Clone ---

func TestClone_DeepCopy(tt *testing.T) {
	g := New()
	p := patch.NewPatch("test", "add lines", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("first\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("second\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})
	p.Apply(g)

	clone := g.Clone()

	// Mutate original: delete a node.
	nodeID := t.NodeID{Patch: p.Hash, Index: 0}
	g.DeleteNode(nodeID, testDeleter)
	g.ResolvePseudoEdges()

	// Clone should be unaffected.
	if !clone.IsLive(nodeID) {
		tt.Fatal("clone should still have node as live")
	}
	if clone.IsDeleted(nodeID) {
		tt.Fatal("clone should not have node as deleted")
	}

	// Clone should still render the original content.
	rendered := string(clone.Render())
	if rendered != "first\nsecond\n" {
		tt.Fatalf("clone render mismatch: %q", rendered)
	}
}

func TestClone_PreservesEdges(tt *testing.T) {
	g := New()
	a := nid("clone_edge", 0)
	b := nid("clone_edge", 1)
	c := nid("clone_edge", 2)
	g.AddNode(a, []byte("a\n"), ph("clone_edge"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("clone_edge"), []t.NodeID{a}, nil)
	g.AddNode(c, []byte("c\n"), ph("clone_edge"), []t.NodeID{b}, nil)

	// Delete middle to create pseudo-edges.
	g.DeleteNode(b, testDeleter)
	g.ResolvePseudoEdges()

	clone := g.Clone()

	// Clone should have same linear order.
	origOrder := algo.LinearOrder(g)
	cloneOrder := algo.LinearOrder(clone)
	if origOrder == nil || cloneOrder == nil {
		tt.Fatal("both should have linear order")
	}
	if len(origOrder) != len(cloneOrder) {
		tt.Fatalf("order length mismatch: %d vs %d", len(origOrder), len(cloneOrder))
	}
	for i := range origOrder {
		if origOrder[i] != cloneOrder[i] {
			tt.Fatalf("order mismatch at position %d", i)
		}
	}
}

func TestClone_PreservesDeletedNodes(tt *testing.T) {
	g := New()
	a := nid("clone_del", 0)
	g.AddNode(a, []byte("a\n"), ph("clone_del"), []t.NodeID{t.RootNodeID}, nil)
	g.DeleteNode(a, testDeleter)

	clone := g.Clone()
	if !clone.IsDeleted(a) {
		tt.Fatal("clone should preserve deleted nodes")
	}
	if clone.IsLive(a) {
		tt.Fatal("deleted node should not be live in clone")
	}
}

func TestClone_EmptyGraggle(tt *testing.T) {
	g := New()
	clone := g.Clone()
	if clone.nodes.Len() != 1 {
		tt.Fatalf("expected 1 node (root), got %d", clone.nodes.Len())
	}
	if !clone.IsLive(t.RootNodeID) {
		tt.Fatal("clone should have live root")
	}
}

func TestClone_ContentIsolation(tt *testing.T) {
	g := New()
	a := nid("clone_iso", 0)
	g.AddNode(a, []byte("original\n"), ph("clone_iso"), []t.NodeID{t.RootNodeID}, nil)

	clone := g.Clone()

	// Mutate the content in the original.
	g.contents[a] = []byte("mutated\n")

	// Clone should be unaffected.
	if string(clone.NodeContent(a)) != "original\n" {
		tt.Fatalf("clone content should be isolated, got %q", string(clone.NodeContent(a)))
	}
}

// --- Linear Order ---

func TestLinearOrder_Simple(tt *testing.T) {
	g := New()
	a := nid("p1", 0)
	b := nid("p1", 1)
	c := nid("p1", 2)
	g.AddNode(a, []byte("a\n"), ph("p1"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("p1"), []t.NodeID{a}, nil)
	g.AddNode(c, []byte("c\n"), ph("p1"), []t.NodeID{b}, nil)

	order := algo.LinearOrder(g)
	if order == nil {
		tt.Fatal("expected linear order")
	}
	// Should be: root, a, b, c
	if len(order) != 4 {
		tt.Fatalf("expected 4 nodes in order, got %d", len(order))
	}
	if order[0] != t.RootNodeID || order[1] != a || order[2] != b || order[3] != c {
		tt.Fatalf("wrong order: %v", order)
	}
}

// --- Pseudo-edges ---

func TestPseudoEdge_DeleteMiddle(tt *testing.T) {
	// root -> a -> b -> c
	// Delete b => should create pseudo-edge root->a->c (a->c via pseudo)
	// Wait, let me think: root -> a -> b -> c, delete b.
	// a is live, c is live, b is deleted. Pseudo-edge: a -> c.
	g := New()
	a := nid("p1", 0)
	b := nid("p1", 1)
	c := nid("p1", 2)
	g.AddNode(a, []byte("a\n"), ph("p1"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("p1"), []t.NodeID{a}, nil)
	g.AddNode(c, []byte("c\n"), ph("p1"), []t.NodeID{b}, nil)

	g.DeleteNode(b, testDeleter)
	g.ResolvePseudoEdges()

	// Check that a has a child c (via pseudo-edge).
	children := slices.Collect(g.LiveChildren(a))
	found := false
	for _, ch := range children {
		if ch == c {
			found = true
			break
		}
	}
	if !found {
		tt.Fatal("expected pseudo-edge a -> c after deleting b")
	}

	// The linear order should now be: root, a, c
	order := algo.LinearOrder(g)
	if order == nil {
		tt.Fatal("expected linear order after deletion")
	}
	if len(order) != 3 {
		tt.Fatalf("expected 3 nodes, got %d", len(order))
	}
}

func TestPseudoEdge_DeleteLongMiddle(tt *testing.T) {
	// root -> a -> b -> c -> d -> e
	// Delete b, c, d => pseudo-edge a -> e
	g := New()
	a := nid("p1", 0)
	b := nid("p1", 1)
	c := nid("p1", 2)
	d := nid("p1", 3)
	e := nid("p1", 4)
	g.AddNode(a, []byte("a\n"), ph("p1"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("p1"), []t.NodeID{a}, nil)
	g.AddNode(c, []byte("c\n"), ph("p1"), []t.NodeID{b}, nil)
	g.AddNode(d, []byte("d\n"), ph("p1"), []t.NodeID{c}, nil)
	g.AddNode(e, []byte("e\n"), ph("p1"), []t.NodeID{d}, nil)

	g.DeleteNode(b, testDeleter)
	g.DeleteNode(c, testDeleter)
	g.DeleteNode(d, testDeleter)
	g.ResolvePseudoEdges()

	order := algo.LinearOrder(g)
	if order == nil {
		tt.Fatal("expected linear order")
	}
	// root, a, e
	if len(order) != 3 {
		tt.Fatalf("expected 3 nodes, got %d: %v", len(order), order)
	}
	if order[1] != a || order[2] != e {
		tt.Fatalf("expected [root, a, e], got %v", order)
	}
}

func TestPseudoEdge_UndeleteRemovesPseudo(tt *testing.T) {
	// root -> a -> b -> c. Delete b (pseudo a->c). Undelete b.
	g := New()
	a := nid("p1", 0)
	b := nid("p1", 1)
	c := nid("p1", 2)
	g.AddNode(a, []byte("a\n"), ph("p1"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("p1"), []t.NodeID{a}, nil)
	g.AddNode(c, []byte("c\n"), ph("p1"), []t.NodeID{b}, nil)

	g.DeleteNode(b, testDeleter)
	g.ResolvePseudoEdges()
	g.UndeleteNode(b, testDeleter)
	g.ResolvePseudoEdges()

	order := algo.LinearOrder(g)
	if order == nil {
		tt.Fatal("expected linear order")
	}
	if len(order) != 4 {
		tt.Fatalf("expected 4 nodes, got %d", len(order))
	}
}

// --- Patch Apply/Unapply ---

func TestPatchApply(tt *testing.T) {
	g := New()
	p := patch.NewPatch("test", "add lines", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("first\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("second\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}, DownContext: nil},
	})

	if err := p.Apply(g); err != nil {
		tt.Fatal(err)
	}

	order := algo.LinearOrder(g)
	if order == nil {
		tt.Fatal("expected linear order")
	}
	if len(order) != 3 { // root + 2 lines
		tt.Fatalf("expected 3 nodes, got %d", len(order))
	}

	rendered := string(g.Render())
	if rendered != "first\nsecond\n" {
		tt.Fatalf("wrong render: %q", rendered)
	}
}

func TestPatchUnapply(tt *testing.T) {
	g := New()
	p := patch.NewPatch("test", "add lines", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("first\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("second\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})

	p.Apply(g)
	if err := p.Unapply(g); err != nil {
		tt.Fatal(err)
	}

	if g.nodes.Len() != 1 { // only root
		tt.Fatalf("expected 1 node after unapply, got %d", g.nodes.Len())
	}
}

func TestPatchApplyDelete(tt *testing.T) {
	g := New()
	// First patch: add 3 lines.
	p1 := patch.NewPatch("test", "add lines", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 2}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}}},
	})
	p1.Apply(g)

	// Second patch: delete middle line.
	lineB := t.NodeID{Patch: p1.Hash, Index: 1}
	p2 := patch.NewPatch("test", "delete b", []t.PatchHash{p1.Hash}, []patch.Change{
		{Kind: patch.ChangeKindDeleteNode, NodeID: lineB},
	})
	p2.Apply(g)

	rendered := string(g.Render())
	if rendered != "a\nc\n" {
		tt.Fatalf("expected 'a\\nc\\n', got %q", rendered)
	}
}

// --- Merge / Pushout Tests ---

func TestMerge_NoConflict(tt *testing.T) {
	// Base: root -> a -> c
	// Patch 1: insert b between a and c (from base).
	// Patch 2: insert d after c (from base).
	// After applying both: root -> a -> b -> c -> d (no conflict).
	g := New()
	base := patch.NewPatch("test", "base", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})
	base.Apply(g)

	lineA := t.NodeID{Patch: base.Hash, Index: 0}
	lineC := t.NodeID{Patch: base.Hash, Index: 1}

	p1 := patch.NewPatch("user1", "insert b", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("b\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})
	p2 := patch.NewPatch("user2", "insert d", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("d\n"),
			UpContext: []t.NodeID{lineC}},
	})

	if err := p1.Apply(g); err != nil {
		tt.Fatal(err)
	}
	if err := p2.Apply(g); err != nil {
		tt.Fatal(err)
	}

	rendered := string(g.Render())
	if rendered != "a\nb\nc\nd\n" {
		tt.Fatalf("expected clean merge, got %q", rendered)
	}
	if algo.HasConflicts(g) {
		tt.Fatal("should not have conflicts")
	}
}

func TestMerge_OrderConflict(tt *testing.T) {
	// Base: root -> a -> c
	// Patch 1: insert X between a and c.
	// Patch 2: insert Y between a and c.
	// Both insert at the same position => order conflict.
	g := New()
	base := patch.NewPatch("test", "base", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})
	base.Apply(g)

	lineA := t.NodeID{Patch: base.Hash, Index: 0}
	lineC := t.NodeID{Patch: base.Hash, Index: 1}

	p1 := patch.NewPatch("user1", "insert X", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("X\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})
	p2 := patch.NewPatch("user2", "insert Y", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("Y\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})

	p1.Apply(g)
	p2.Apply(g)

	if !algo.HasConflicts(g) {
		tt.Fatal("should have order conflict")
	}

	rendered := string(g.Render())
	if !strings.Contains(rendered, "order conflict") {
		tt.Fatalf("expected conflict markers, got %q", rendered)
	}
	if !strings.Contains(rendered, "X\n") || !strings.Contains(rendered, "Y\n") {
		tt.Fatalf("conflict should contain both sides, got %q", rendered)
	}
	// a and c should still appear.
	if !strings.Contains(rendered, "a\n") || !strings.Contains(rendered, "c\n") {
		tt.Fatalf("non-conflicting lines should appear, got %q", rendered)
	}
}

func TestMerge_DeleteVsEdit_Zombie(tt *testing.T) {
	// Base: root -> a -> b -> c
	// Patch 1: delete b.
	// Patch 2: insert X with up_context=b (depends on b being alive).
	// After both: X becomes a "zombie" -- its context was deleted.
	// The system should handle this gracefully.
	g := New()
	base := patch.NewPatch("test", "base", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 2}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}}},
	})
	base.Apply(g)

	lineB := t.NodeID{Patch: base.Hash, Index: 1}
	lineC := t.NodeID{Patch: base.Hash, Index: 2}

	// Patch 1: delete b.
	p1 := patch.NewPatch("user1", "delete b", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindDeleteNode, NodeID: lineB},
	})

	// Patch 2: insert X after b, before c.
	p2 := patch.NewPatch("user2", "insert X after b", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("X\n"),
			UpContext: []t.NodeID{lineB}, DownContext: []t.NodeID{lineC}},
	})

	p1.Apply(g)
	p2.Apply(g)

	// X should still appear in the output (its context b is deleted, but X itself is live).
	rendered := string(g.Render())
	if !strings.Contains(rendered, "X\n") {
		tt.Fatalf("zombie-context node X should still appear in output, got %q", rendered)
	}
	if !strings.Contains(rendered, "a\n") {
		tt.Fatalf("line a should appear, got %q", rendered)
	}
}

func TestMerge_Commutativity(tt *testing.T) {
	// Applying patches p1 then p2 should give the same result as p2 then p1.
	makeBase := func() (*Graggle, *patch.Patch) {
		g := New()
		base := patch.NewPatch("test", "base", nil, []patch.Change{
			{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
			{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
		})
		base.Apply(g)
		return g, base
	}

	g1, base := makeBase()
	lineA := t.NodeID{Patch: base.Hash, Index: 0}
	lineC := t.NodeID{Patch: base.Hash, Index: 1}

	p1 := patch.NewPatch("user1", "insert b", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("b\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})
	p2 := patch.NewPatch("user2", "insert d", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("d\n"),
			UpContext: []t.NodeID{lineC}},
	})

	// Order 1: p1 then p2.
	p1.Apply(g1)
	p2.Apply(g1)
	r1 := g1.Render()

	// Order 2: p2 then p1.
	g2, _ := makeBase()
	p2.Apply(g2)
	p1.Apply(g2)
	r2 := g2.Render()

	if !bytes.Equal(r1, r2) {
		tt.Fatalf("commutativity violated:\norder 1: %q\norder 2: %q", string(r1), string(r2))
	}
}

func TestMerge_ConflictCommutativity(tt *testing.T) {
	// Even with conflicts, applying in either order should give the same set
	// of live nodes (though rendering order of conflicting sides may differ,
	// the conflict structure should be equivalent).
	makeBase := func() (*Graggle, *patch.Patch) {
		g := New()
		base := patch.NewPatch("test", "base", nil, []patch.Change{
			{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
			{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
		})
		base.Apply(g)
		return g, base
	}

	g1, base := makeBase()
	lineA := t.NodeID{Patch: base.Hash, Index: 0}
	lineC := t.NodeID{Patch: base.Hash, Index: 1}

	p1 := patch.NewPatch("user1", "insert X", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("X\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})
	p2 := patch.NewPatch("user2", "insert Y", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("Y\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})

	p1.Apply(g1)
	p2.Apply(g1)

	g2, _ := makeBase()
	p2.Apply(g2)
	p1.Apply(g2)

	// Both should have conflicts.
	if !algo.HasConflicts(g1) || !algo.HasConflicts(g2) {
		tt.Fatal("both should have conflicts")
	}

	// Both should have the same set of live nodes.
	nodes1 := slices.Collect(g1.AllLiveNodes())
	nodes2 := slices.Collect(g2.AllLiveNodes())
	if len(nodes1) != len(nodes2) {
		tt.Fatalf("different node counts: %d vs %d", len(nodes1), len(nodes2))
	}
	set1 := make(map[t.NodeID]struct{})
	for _, n := range nodes1 {
		set1[n] = struct{}{}
	}
	for _, n := range nodes2 {
		if _, ok := set1[n]; !ok {
			tt.Fatalf("node %v in g2 but not g1", n)
		}
	}
}

// --- Conflict Resolution ---

func TestConflictResolution(tt *testing.T) {
	// Create a conflict, then resolve it by adding an ordering edge.
	g := New()
	base := patch.NewPatch("test", "base", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})
	base.Apply(g)

	lineA := t.NodeID{Patch: base.Hash, Index: 0}
	lineC := t.NodeID{Patch: base.Hash, Index: 1}

	p1 := patch.NewPatch("user1", "insert X", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("X\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})
	p2 := patch.NewPatch("user2", "insert Y", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("Y\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})

	p1.Apply(g)
	p2.Apply(g)

	if !algo.HasConflicts(g) {
		tt.Fatal("should have conflict before resolution")
	}

	// Resolve: add edge X -> Y.
	lineX := t.NodeID{Patch: p1.Hash, Index: 0}
	lineY := t.NodeID{Patch: p2.Hash, Index: 0}

	resolution := patch.NewPatch("resolver", "resolve X before Y", []t.PatchHash{p1.Hash, p2.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewEdge, Src: lineX, Dest: lineY},
	})
	resolution.Apply(g)

	if algo.HasConflicts(g) {
		tt.Fatal("should not have conflict after resolution")
	}

	rendered := string(g.Render())
	expected := "a\nX\nY\nc\n"
	if rendered != expected {
		tt.Fatalf("expected %q, got %q", expected, rendered)
	}
}

// --- Diff ---

func TestLineDiff_Insert(tt *testing.T) {
	oldIDs := []t.NodeID{nid("p1", 0), nid("p1", 1)}
	oldContents := [][]byte{[]byte("a\n"), []byte("c\n")}
	newLines := [][]byte{[]byte("a\n"), []byte("b\n"), []byte("c\n")}

	result := mustLineDiff(tt, oldIDs, oldContents, newLines)
	// Should have one NewNode for "b\n".
	newNodes := 0
	for _, c := range result.Changes {
		if c.Kind == patch.ChangeKindNewNode {
			newNodes++
			if string(c.Content) != "b\n" {
				tt.Fatalf("wrong content: %q", string(c.Content))
			}
		}
	}
	if newNodes != 1 {
		tt.Fatalf("expected 1 new node, got %d", newNodes)
	}
}

func TestLineDiff_Delete(tt *testing.T) {
	oldIDs := []t.NodeID{nid("p1", 0), nid("p1", 1), nid("p1", 2)}
	oldContents := [][]byte{[]byte("a\n"), []byte("b\n"), []byte("c\n")}
	newLines := [][]byte{[]byte("a\n"), []byte("c\n")}

	result := mustLineDiff(tt, oldIDs, oldContents, newLines)
	deletes := 0
	for _, c := range result.Changes {
		if c.Kind == patch.ChangeKindDeleteNode {
			deletes++
			if c.NodeID != nid("p1", 1) {
				tt.Fatalf("wrong deletion: %v", c.NodeID)
			}
		}
	}
	if deletes != 1 {
		tt.Fatalf("expected 1 deletion, got %d", deletes)
	}
}

func TestLineDiff_Replace(tt *testing.T) {
	oldIDs := []t.NodeID{nid("p1", 0), nid("p1", 1), nid("p1", 2)}
	oldContents := [][]byte{[]byte("a\n"), []byte("b\n"), []byte("c\n")}
	newLines := [][]byte{[]byte("a\n"), []byte("X\n"), []byte("c\n")}

	result := mustLineDiff(tt, oldIDs, oldContents, newLines)
	deletes := 0
	inserts := 0
	for _, c := range result.Changes {
		if c.Kind == patch.ChangeKindDeleteNode {
			deletes++
		}
		if c.Kind == patch.ChangeKindNewNode {
			inserts++
		}
	}
	if deletes != 1 || inserts != 1 {
		tt.Fatalf("expected 1 delete + 1 insert, got %d deletes + %d inserts", deletes, inserts)
	}
}

// --- Tarjan SCC ---

func TestTarjan_Linear(tt *testing.T) {
	g := New()
	a := nid("p1", 0)
	b := nid("p1", 1)
	g.AddNode(a, []byte("a\n"), ph("p1"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("p1"), []t.NodeID{a}, nil)

	sccs := algo.Tarjan(g)
	// Should have 3 SCCs (root, a, b), each of size 1.
	for _, scc := range sccs {
		if len(scc) != 1 {
			tt.Fatalf("expected all SCCs of size 1, got one of size %d", len(scc))
		}
	}
}

// --- Associativity ---

func TestMerge_Associativity(tt *testing.T) {
	// Given three independent patches from the same base,
	// (p1 merge p2) merge p3 should equal p1 merge (p2 merge p3).
	makeBase := func() (*Graggle, *patch.Patch) {
		g := New()
		base := patch.NewPatch("test", "base", nil, []patch.Change{
			{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("start\n"), UpContext: []t.NodeID{t.RootNodeID}},
			{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("end\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
		})
		base.Apply(g)
		return g, base
	}

	_, base := makeBase()
	lineStart := t.NodeID{Patch: base.Hash, Index: 0}
	lineEnd := t.NodeID{Patch: base.Hash, Index: 1}

	// p1: insert A after start.
	p1 := patch.NewPatch("u1", "insert A", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("A\n"),
			UpContext: []t.NodeID{lineStart}, DownContext: []t.NodeID{lineEnd}},
	})
	// p2: insert B after start (will conflict with A).
	p2 := patch.NewPatch("u2", "insert B", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("B\n"),
			UpContext: []t.NodeID{lineStart}, DownContext: []t.NodeID{lineEnd}},
	})
	// p3: insert Z after end.
	p3 := patch.NewPatch("u3", "insert Z", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("Z\n"),
			UpContext: []t.NodeID{lineEnd}},
	})

	// Order 1: (p1, p2, p3)
	g1, _ := makeBase()
	p1.Apply(g1)
	p2.Apply(g1)
	p3.Apply(g1)

	// Order 2: (p2, p3, p1)
	g2, _ := makeBase()
	p2.Apply(g2)
	p3.Apply(g2)
	p1.Apply(g2)

	// Order 3: (p3, p1, p2)
	g3, _ := makeBase()
	p3.Apply(g3)
	p1.Apply(g3)
	p2.Apply(g3)

	// All should have the same set of live nodes.
	nodes1 := slices.Collect(g1.AllLiveNodes())
	nodes2 := slices.Collect(g2.AllLiveNodes())
	nodes3 := slices.Collect(g3.AllLiveNodes())

	if len(nodes1) != len(nodes2) || len(nodes1) != len(nodes3) {
		tt.Fatalf("different node counts: %d, %d, %d", len(nodes1), len(nodes2), len(nodes3))
	}

	set := make(map[t.NodeID]struct{})
	for _, n := range nodes1 {
		set[n] = struct{}{}
	}
	for _, n := range nodes2 {
		if _, ok := set[n]; !ok {
			tt.Fatalf("node %v in g2 but not g1", n)
		}
	}
	for _, n := range nodes3 {
		if _, ok := set[n]; !ok {
			tt.Fatalf("node %v in g3 but not g1", n)
		}
	}
}

// --- Cherry-pick scenario ---

func TestCherryPick_NoConflict(tt *testing.T) {
	// Pijul's key advantage: cherry-picking a patch and later merging
	// does not cause conflicts, because patches have identity.
	g := New()
	base := patch.NewPatch("test", "base", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})
	base.Apply(g)

	lineA := t.NodeID{Patch: base.Hash, Index: 0}
	lineB := t.NodeID{Patch: base.Hash, Index: 1}

	// A patch that inserts X between a and b.
	pX := patch.NewPatch("dev", "insert X", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("X\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineB}},
	})

	// Branch 1: apply pX.
	g1 := g.Clone()
	pX.Apply(g1)

	// Branch 2: also apply pX (cherry-pick).
	g2 := g.Clone()
	pX.Apply(g2)

	// Now "merge" by applying any extra patches from g1 to g2.
	// Since pX is already applied to both, there's nothing to do.
	// The key point: the same patch applied twice is a no-op (idempotent by identity).

	r1 := string(g1.Render())
	r2 := string(g2.Render())
	if r1 != r2 {
		tt.Fatalf("cherry-picked branches should be identical:\n  g1: %q\n  g2: %q", r1, r2)
	}
	expected := "a\nX\nb\n"
	if r1 != expected {
		tt.Fatalf("expected %q, got %q", expected, r1)
	}
}

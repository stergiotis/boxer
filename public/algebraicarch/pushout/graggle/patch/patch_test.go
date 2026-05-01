//go:build llm_generated_opus47

package patch

import (
	"strings"
	"testing"

	"github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/store"
	t "github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/types"
)

// --- ComputeDependencies ---

func TestComputeDependencies_NewNode(tt *testing.T) {
	patchA := ph("depA")
	patchB := ph("depB")
	changes := []Change{
		{
			Kind:       ChangeNewNode,
			NodeID:     t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:    []byte("x\n"),
			UpContext:   []t.NodeID{{Patch: patchA, Index: 0}},
			DownContext: []t.NodeID{{Patch: patchB, Index: 0}},
		},
	}
	deps := ComputeDependencies(changes)
	if len(deps) != 2 {
		tt.Fatalf("expected 2 deps, got %d", len(deps))
	}
	depSet := make(map[t.PatchHash]struct{})
	for _, d := range deps {
		depSet[d] = struct{}{}
	}
	if _, ok := depSet[patchA]; !ok {
		tt.Fatal("should depend on patchA")
	}
	if _, ok := depSet[patchB]; !ok {
		tt.Fatal("should depend on patchB")
	}
}

func TestComputeDependencies_DeleteNode(tt *testing.T) {
	patchA := ph("depDel")
	changes := []Change{
		{Kind: ChangeDeleteNode, NodeID: t.NodeID{Patch: patchA, Index: 0}},
	}
	deps := ComputeDependencies(changes)
	if len(deps) != 1 || deps[0] != patchA {
		tt.Fatalf("expected [patchA], got %v", deps)
	}
}

func TestComputeDependencies_NewEdge(tt *testing.T) {
	patchA := ph("depEdgeA")
	patchB := ph("depEdgeB")
	changes := []Change{
		{Kind: ChangeNewEdge, Src: t.NodeID{Patch: patchA, Index: 0}, Dest: t.NodeID{Patch: patchB, Index: 0}},
	}
	deps := ComputeDependencies(changes)
	if len(deps) != 2 {
		tt.Fatalf("expected 2 deps, got %d", len(deps))
	}
}

func TestComputeDependencies_SkipsZeroHash(tt *testing.T) {
	// References to root (zero hash) should not appear as dependencies.
	changes := []Change{
		{
			Kind:     ChangeNewNode,
			NodeID:   t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:  []byte("x\n"),
			UpContext: []t.NodeID{t.RootNodeID}, // zero hash
		},
	}
	deps := ComputeDependencies(changes)
	if len(deps) != 0 {
		tt.Fatalf("expected 0 deps (root refs skipped), got %d: %v", len(deps), deps)
	}
}

func TestComputeDependencies_Deduplication(tt *testing.T) {
	patchA := ph("depDedup")
	changes := []Change{
		{
			Kind:       ChangeNewNode,
			NodeID:     t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:    []byte("x\n"),
			UpContext:   []t.NodeID{{Patch: patchA, Index: 0}},
			DownContext: []t.NodeID{{Patch: patchA, Index: 1}},
		},
	}
	deps := ComputeDependencies(changes)
	if len(deps) != 1 {
		tt.Fatalf("expected 1 dep (deduplicated), got %d", len(deps))
	}
}

func TestComputeDependencies_Empty(tt *testing.T) {
	deps := ComputeDependencies(nil)
	if len(deps) != 0 {
		tt.Fatalf("expected 0 deps for empty changes, got %d", len(deps))
	}
}

func TestComputeDependencies_SkipsPlaceholder(tt *testing.T) {
	// Placeholder hashes refer to "this patch" pre-fixup; they are not real
	// patch dependencies and must be excluded.
	changes := []Change{
		{
			Kind:      ChangeNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:   []byte("x\n"),
			UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}},
		},
	}
	deps := ComputeDependencies(changes)
	for _, d := range deps {
		if d.IsPlaceholder() {
			tt.Fatalf("placeholder must be excluded from deps, got %v", d)
		}
	}
}

// --- Patch Apply Error Paths ---

func TestPatchApply_MissingUpContext(tt *testing.T) {
	g := store.New()
	p := NewPatch("test", "bad context", nil, []Change{
		{
			Kind:     ChangeNewNode,
			NodeID:   t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:  []byte("x\n"),
			UpContext: []t.NodeID{nid("nonexistent", 0)},
		},
	})
	err := p.Apply(g)
	if err == nil {
		tt.Fatal("expected error for missing up-context")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		tt.Fatalf("unexpected error: %v", err)
	}
}

func TestPatchApply_MissingDownContext(tt *testing.T) {
	g := store.New()
	p := NewPatch("test", "bad context", nil, []Change{
		{
			Kind:        ChangeNewNode,
			NodeID:      t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:     []byte("x\n"),
			UpContext:    []t.NodeID{t.RootNodeID},
			DownContext:  []t.NodeID{nid("nonexistent", 0)},
		},
	})
	err := p.Apply(g)
	if err == nil {
		tt.Fatal("expected error for missing down-context")
	}
}

func TestPatchApply_DeleteNonexistent(tt *testing.T) {
	g := store.New()
	p := NewPatch("test", "bad delete", nil, []Change{
		{Kind: ChangeDeleteNode, NodeID: nid("nonexistent", 0)},
	})
	err := p.Apply(g)
	if err == nil {
		tt.Fatal("expected error for deleting nonexistent node")
	}
}

func TestPatchApply_DuplicateNode(tt *testing.T) {
	g := store.New()
	// First patch adds a node.
	p1 := NewPatch("test", "add node", nil, []Change{
		{Kind: ChangeNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("x\n"), UpContext: []t.NodeID{t.RootNodeID}},
	})
	p1.Apply(g)

	// Second patch tries to add a node with the same ID as one in p1.
	// We need to use p1's actual hash.
	p2Changes := []Change{
		{Kind: ChangeNewNode, NodeID: t.NodeID{Patch: p1.Hash, Index: 0}, Content: []byte("y\n"), UpContext: []t.NodeID{t.RootNodeID}},
	}
	p2 := &Patch{Changes: p2Changes, Hash: ph("p2_dup")}
	err := p2.Apply(g)
	if err == nil {
		tt.Fatal("expected error for duplicate node")
	}
}

func TestPatchApply_DeleteAlreadyDeleted(tt *testing.T) {
	// VENDOR DEVIATION: this test was inverted from the upstream
	// expectation. DeleteNode is idempotent in this fork; double-delete
	// is a no-op success rather than an error so that two patches can
	// legitimately delete the same node (the typical same-line conflict).
	g := store.New()
	p1 := NewPatch("test", "add", nil, []Change{
		{Kind: ChangeNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("x\n"), UpContext: []t.NodeID{t.RootNodeID}},
	})
	p1.Apply(g)

	nodeID := t.NodeID{Patch: p1.Hash, Index: 0}
	if err := g.DeleteNode(nodeID); err != nil {
		tt.Fatalf("first delete failed: %v", err)
	}
	if !g.IsDeleted(nodeID) {
		tt.Fatalf("first delete did not tombstone node")
	}

	// Apply a second patch that deletes the same node.
	p2 := &Patch{
		Hash: ph("p2_deldel"),
		Changes: []Change{
			{Kind: ChangeDeleteNode, NodeID: nodeID},
		},
	}
	if err := p2.Apply(g); err != nil {
		tt.Fatalf("double-delete should be idempotent, got error: %v", err)
	}
	if !g.IsDeleted(nodeID) {
		tt.Fatalf("node should still be tombstoned after double-delete")
	}
	if g.IsLive(nodeID) {
		tt.Fatalf("node should not be live after double-delete")
	}
}

func TestPatchUnapply_UndeleteNonDeleted(tt *testing.T) {
	g := store.New()
	p1 := NewPatch("test", "add", nil, []Change{
		{Kind: ChangeNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("x\n"), UpContext: []t.NodeID{t.RootNodeID}},
	})
	p1.Apply(g)

	// A patch that "deletes" the node — but we don't apply it, we try to unapply it
	// (which would try to undelete a node that was never deleted).
	nodeID := t.NodeID{Patch: p1.Hash, Index: 0}
	pDel := &Patch{
		Hash: ph("p_del_fake"),
		Changes: []Change{
			{Kind: ChangeDeleteNode, NodeID: nodeID},
		},
	}
	err := pDel.Unapply(g)
	if err == nil {
		tt.Fatal("expected error for undeleting a non-deleted node")
	}
}

// Unapply must refuse to remove a node that has incident edges from another
// patch — otherwise those edges would dangle. The caller has to unapply
// dependents first.
func TestPatchUnapply_RefusesWhenForeignEdgesExist(tt *testing.T) {
	g := store.New()

	pBase := NewPatch("base", "add a", nil, []Change{
		{
			Kind:      ChangeNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:   []byte("a\n"),
			UpContext: []t.NodeID{t.RootNodeID},
		},
	})
	if err := pBase.Apply(g); err != nil {
		tt.Fatal(err)
	}

	aID := t.NodeID{Patch: pBase.Hash, Index: 0}
	pDep := NewPatch("dep", "add b after a", []t.PatchHash{pBase.Hash}, []Change{
		{
			Kind:      ChangeNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:   []byte("b\n"),
			UpContext: []t.NodeID{aID},
		},
	})
	if err := pDep.Apply(g); err != nil {
		tt.Fatal(err)
	}

	// Now pDep introduced an edge from aID. Unapplying pBase would orphan
	// that edge.
	err := pBase.Unapply(g)
	if err == nil {
		tt.Fatal("expected unapply to refuse when dependents have foreign edges")
	}
	if !strings.Contains(err.Error(), "foreign") {
		tt.Fatalf("unexpected error: %v", err)
	}

	// After unapplying the dependent first, base unapply must succeed.
	if err := pDep.Unapply(g); err != nil {
		tt.Fatalf("unapply dep: %v", err)
	}
	if err := pBase.Unapply(g); err != nil {
		tt.Fatalf("unapply base after dep: %v", err)
	}
}

// --- Patch Hash ---

func TestPatchHash_Stability(tt *testing.T) {
	// Same changes should produce same hash.
	changes := []Change{
		{Kind: ChangeNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
	}
	p1 := NewPatch("alice", "test1", nil, changes)

	changes2 := []Change{
		{Kind: ChangeNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
	}
	p2 := NewPatch("bob", "test2", nil, changes2)

	// Author and description don't affect hash.
	if p1.Hash != p2.Hash {
		tt.Fatal("same changes should produce same hash regardless of metadata")
	}
}

func TestPatchHash_DifferentChanges(tt *testing.T) {
	p1 := NewPatch("test", "a", nil, []Change{
		{Kind: ChangeNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
	})
	p2 := NewPatch("test", "b", nil, []Change{
		{Kind: ChangeNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("b\n"), UpContext: []t.NodeID{t.RootNodeID}},
	})
	if p1.Hash == p2.Hash {
		tt.Fatal("different changes should produce different hash")
	}
}

// --- NewPatch Placeholder Fixup ---

func TestNewPatch_PlaceholderFixup(tt *testing.T) {
	p := NewPatch("test", "fixup", nil, []Change{
		{Kind: ChangeNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: ChangeNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})

	// After NewPatch, placeholders should be replaced with the real hash.
	for _, c := range p.Changes {
		if c.Kind == ChangeNewNode {
			if c.NodeID.Patch.IsPlaceholder() {
				tt.Fatal("NodeID should have been fixed up")
			}
			if c.NodeID.Patch != p.Hash {
				tt.Fatal("NodeID.Patch should equal the patch hash")
			}
			for _, ctx := range c.UpContext {
				if ctx.Patch.IsPlaceholder() {
					tt.Fatal("UpContext should have been fixed up")
				}
			}
		}
	}
}
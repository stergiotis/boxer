package dot_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/dot"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/store"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

func TestDot_Simple(tt *testing.T) {
	g := store.New()
	p := patch.NewPatch("test", "lines", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("hello\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("world\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})
	p.Apply(g)

	d := dot.Dot(g)
	if !strings.Contains(d, "digraph graggle") {
		tt.Fatal("should contain digraph header")
	}
	if !strings.Contains(d, "root") {
		tt.Fatal("should contain root node")
	}
	if !strings.Contains(d, "hello") {
		tt.Fatal("should contain node content")
	}
	if !strings.Contains(d, "->") {
		tt.Fatal("should contain edges")
	}
}

func TestDot_WithDeletedAndPseudo(tt *testing.T) {
	g := store.New()
	a := nid("dot1", 0)
	b := nid("dot1", 1)
	c := nid("dot1", 2)
	g.AddNode(a, []byte("a\n"), ph("dot1"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("b\n"), ph("dot1"), []t.NodeID{a}, nil)
	g.AddNode(c, []byte("c\n"), ph("dot1"), []t.NodeID{b}, nil)

	g.DeleteNode(b, ph("dot1_del"))
	g.ResolvePseudoEdges()

	d := dot.Dot(g)
	// Should have dashed style for deleted node.
	if !strings.Contains(d, "dashed") {
		tt.Fatal("should have dashed style for deleted node")
	}
	// Should have dotted style for pseudo-edge.
	if !strings.Contains(d, "dotted") {
		tt.Fatal("should have dotted style for pseudo-edge")
	}
	// Should have blue color for pseudo-edge.
	if !strings.Contains(d, "blue") {
		tt.Fatal("should have blue color for pseudo-edge")
	}
}

// TestDot_AllTypesExample builds a graph that exercises every node and edge type
// and writes the result to example.dot in the repo root (next to the Makefile).
//
// Node types:  root (diamond), live (solid box), deleted (dashed box)
// Edge types:  live (solid), deleted (dashed grey), pseudo (dotted blue)
//
// Scenario:
//
//	base file: root -> a -> b -> c -> d
//	user1 deletes b  (creates deleted node + deleted edges + pseudo-edge a->c)
//	user2 inserts X and Y between c and d at the same position (order conflict)
//	result: all three node types and all three edge types present.
func TestDot_AllTypesExample(tt *testing.T) {
	g := store.New()

	// Base: root -> a -> b -> c -> d
	base := patch.NewPatch("alice", "initial file", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("line a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("line b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 2}, Content: []byte("line c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 3}, Content: []byte("line d\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 2}}},
	})
	base.Apply(g)

	lineB := t.NodeID{Patch: base.Hash, Index: 1}
	lineC := t.NodeID{Patch: base.Hash, Index: 2}
	lineD := t.NodeID{Patch: base.Hash, Index: 3}

	// User 1: delete b -> creates deleted node, deleted edges, pseudo-edge a->c.
	pDel := patch.NewPatch("bob", "delete b", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindDeleteNode, NodeID: lineB},
	})
	pDel.Apply(g)

	// User 2: insert X between c and d.
	pX := patch.NewPatch("carol", "insert X", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("line X\n"),
			UpContext: []t.NodeID{lineC}, DownContext: []t.NodeID{lineD}},
	})
	pX.Apply(g)

	// User 3: insert Y between c and d (same position -> order conflict with X).
	pY := patch.NewPatch("dave", "insert Y", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("line Y\n"),
			UpContext: []t.NodeID{lineC}, DownContext: []t.NodeID{lineD}},
	})
	pY.Apply(g)

	d := dot.Dot(g)

	// Verify all node types present.
	if !strings.Contains(d, "diamond") {
		tt.Fatal("missing root node (diamond)")
	}
	if !strings.Contains(d, "lightyellow") {
		tt.Fatal("missing live nodes (lightyellow)")
	}
	if !strings.Contains(d, "fontcolor=grey40") {
		tt.Fatal("missing deleted node")
	}

	// Verify all edge types present.
	if !strings.Contains(d, "style=solid") {
		tt.Fatal("missing live edges (solid)")
	}
	if !strings.Contains(d, "style=dashed") {
		tt.Fatal("missing deleted edges (dashed)")
	}
	if !strings.Contains(d, "style=dotted") {
		tt.Fatal("missing pseudo-edges (dotted)")
	}

	// Write to example.dot next to the Makefile.
	outPath := filepath.Join("..", "..", "example.dot")
	if err := os.WriteFile(outPath, []byte(d), 0644); err != nil {
		tt.Fatalf("failed to write example.dot: %v", err)
	}
	tt.Logf("wrote %s (%d bytes)", outPath, len(d))
}

func TestDot_Conflict(tt *testing.T) {
	g := store.New()
	base := patch.NewPatch("test", "base", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})
	base.Apply(g)

	lineA := t.NodeID{Patch: base.Hash, Index: 0}
	lineC := t.NodeID{Patch: base.Hash, Index: 1}

	p1 := patch.NewPatch("u1", "insert X", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("X\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})
	p2 := patch.NewPatch("u2", "insert Y", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("Y\n"),
			UpContext: []t.NodeID{lineA}, DownContext: []t.NodeID{lineC}},
	})
	p1.Apply(g)
	p2.Apply(g)

	d := dot.Dot(g)
	// Both X and Y should appear.
	if !strings.Contains(d, "X") || !strings.Contains(d, "Y") {
		tt.Fatalf("conflict graph should show both sides, got:\n%s", d)
	}
}

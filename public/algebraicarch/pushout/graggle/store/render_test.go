//go:build llm_generated_opus47

package store

import (
	"strings"
	"testing"

	"github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/types"
)

func TestRender_EmptyGraggle(tt *testing.T) {
	g := New()
	rendered := string(g.Render())
	if rendered != "" {
		tt.Fatalf("empty graggle should render empty, got %q", rendered)
	}
}

func TestRender_SingleLine(tt *testing.T) {
	g := New()
	a := nid("render1", 0)
	g.AddNode(a, []byte("hello\n"), ph("render1"), []t.NodeID{t.RootNodeID}, nil)
	rendered := string(g.Render())
	if rendered != "hello\n" {
		tt.Fatalf("expected 'hello\\n', got %q", rendered)
	}
}

func TestRender_MultipleLines(tt *testing.T) {
	g := New()
	p := patch.NewPatch("test", "lines", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 2}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}}},
	})
	p.Apply(g)
	rendered := string(g.Render())
	if rendered != "a\nb\nc\n" {
		tt.Fatalf("expected 'a\\nb\\nc\\n', got %q", rendered)
	}
}

func TestRender_OrderConflictMarkers(tt *testing.T) {
	g := New()
	a := nid("render_oc", 0)
	b := nid("render_oc", 1)
	g.AddNode(a, []byte("X\n"), ph("render_oc"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("Y\n"), ph("render_oc"), []t.NodeID{t.RootNodeID}, nil)

	rendered := string(g.Render())
	if !strings.Contains(rendered, "order conflict") {
		tt.Fatalf("expected order conflict markers, got %q", rendered)
	}
	if !strings.Contains(rendered, "X\n") || !strings.Contains(rendered, "Y\n") {
		tt.Fatalf("both sides should appear, got %q", rendered)
	}
}

func TestRender_CycleConflictMarkers(tt *testing.T) {
	g := New()
	a := nid("render_cc", 0)
	b := nid("render_cc", 1)
	g.AddNode(a, []byte("A\n"), ph("render_cc"), []t.NodeID{t.RootNodeID}, nil)
	g.AddNode(b, []byte("B\n"), ph("render_cc"), []t.NodeID{a}, nil)
	g.AddEdge(b, a, ph("render_cc_back"))

	rendered := string(g.Render())
	if !strings.Contains(rendered, "cycle conflict") {
		tt.Fatalf("expected cycle conflict markers, got %q", rendered)
	}
}

func TestRender_ZombieNodeVisible(tt *testing.T) {
	// Delete context but the child node itself should still render.
	g := New()
	base := patch.NewPatch("test", "base", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 2}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}}},
	})
	base.Apply(g)

	lineB := t.NodeID{Patch: base.Hash, Index: 1}

	// Insert X with up_context=b.
	pInsert := patch.NewPatch("user", "add X after b", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("X\n"),
			UpContext: []t.NodeID{lineB}, DownContext: []t.NodeID{{Patch: base.Hash, Index: 2}}},
	})
	pInsert.Apply(g)

	// Delete b.
	pDel := patch.NewPatch("user", "delete b", []t.PatchHash{base.Hash}, []patch.Change{
		{Kind: patch.ChangeKindDeleteNode, NodeID: lineB},
	})
	pDel.Apply(g)

	rendered := string(g.Render())
	if !strings.Contains(rendered, "X\n") {
		tt.Fatalf("zombie-context node X should still render, got %q", rendered)
	}
}

func TestRenderLines_SplitCorrectness(tt *testing.T) {
	g := New()
	p := patch.NewPatch("test", "lines", nil, []patch.Change{
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("line1\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("line2\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
	})
	p.Apply(g)

	lines := g.RenderLines()
	if len(lines) != 2 {
		tt.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "line1\n" || lines[1] != "line2\n" {
		tt.Fatalf("wrong lines: %v", lines)
	}
}

func TestRenderLines_Empty(tt *testing.T) {
	g := New()
	lines := g.RenderLines()
	if lines != nil {
		tt.Fatalf("empty graggle should return nil lines, got %v", lines)
	}
}

// Render of a deep conflict graggle exercises the iterative renderDFS at
// depth that would have blown the stack with the recursive version. Each
// level i owns three distinct NodeID indices (3i, 3i+1, 3i+2) for left,
// right, and the merge anchor — distinct namespaces avoid any collision.
func TestRender_DeepConflictNoStackOverflow(tt *testing.T) {
	const n = 5_000
	g := New()
	prev := t.RootNodeID
	for i := 0; i < n; i++ {
		left := nid("deep_conf", uint64(3*i))
		right := nid("deep_conf", uint64(3*i+1))
		next := nid("deep_conf", uint64(3*i+2))
		if err := g.AddNode(left, []byte("L\n"), ph("deep_conf"), []t.NodeID{prev}, nil); err != nil {
			tt.Fatal(err)
		}
		if err := g.AddNode(right, []byte("R\n"), ph("deep_conf"), []t.NodeID{prev}, nil); err != nil {
			tt.Fatal(err)
		}
		if err := g.AddNode(next, []byte("x\n"), ph("deep_conf"), []t.NodeID{left, right}, nil); err != nil {
			tt.Fatal(err)
		}
		prev = next
	}
	out := g.Render()
	if len(out) == 0 {
		tt.Fatal("expected non-empty render of deep conflict")
	}
	if !strings.Contains(string(out), ">>>>>>> order conflict") {
		tt.Fatal("expected order conflict marker in render")
	}
}

// Render of a long linear chain exercises Tarjan plus the linear render
// path; the conflict path is exercised by TestRender_DeepConflictNoStackOverflow.
func TestRender_DeepLinearNoStackOverflow(tt *testing.T) {
	const n = 100_000
	g := New()
	prev := t.RootNodeID
	for i := 0; i < n; i++ {
		id := nid("deep_lin", uint64(i))
		if err := g.AddNode(id, []byte("x\n"), ph("deep_lin"), []t.NodeID{prev}, nil); err != nil {
			tt.Fatal(err)
		}
		prev = id
	}
	out := g.Render()
	if len(out) != n*2 { // each line is "x\n"
		tt.Fatalf("expected %d bytes, got %d", n*2, len(out))
	}
}
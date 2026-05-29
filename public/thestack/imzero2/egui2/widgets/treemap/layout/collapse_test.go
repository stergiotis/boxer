//go:build llm_generated_opus47

package layout

import (
	"testing"
)

func TestCollapsePaths_NilRoot(t *testing.T) {
	if got := CollapsePaths(nil, "/"); got != nil {
		t.Errorf("CollapsePaths(nil) = %+v, want nil", got)
	}
}

func TestCollapsePaths_Leaf_CopiedUnchanged(t *testing.T) {
	n := &Node{Name: "leaf", Size: 42}
	out := CollapsePaths(n, "/")
	if out == n {
		t.Errorf("CollapsePaths returned the input pointer; must be a fresh copy")
	}
	if out.Name != "leaf" || out.Size != 42 || len(out.Children) != 0 {
		t.Errorf("leaf copy: got %+v", out)
	}
}

func TestCollapsePaths_LinearChain_FoldedIntoOneNode(t *testing.T) {
	// a -> b -> c (each parent has exactly one child)
	leaf := &Node{Name: "c", Size: 9}
	mid := &Node{Name: "b", Children: []*Node{leaf}}
	root := &Node{Name: "a", Children: []*Node{mid}}

	out := CollapsePaths(root, "/")
	if out.Name != "a/b/c" {
		t.Errorf("collapsed name: got %q want %q", out.Name, "a/b/c")
	}
	if out.Size != 9 {
		t.Errorf("collapsed size: got %v want 9 (from deepest)", out.Size)
	}
	if len(out.Children) != 0 {
		t.Errorf("collapsed leaf should have no children; got %d", len(out.Children))
	}
}

func TestCollapsePaths_BranchingPreserved(t *testing.T) {
	// a -> b -> {c, d}: chain a->b collapses; b's children become out's children.
	c := &Node{Name: "c", Size: 3}
	d := &Node{Name: "d", Size: 5}
	mid := &Node{Name: "b", Children: []*Node{c, d}}
	root := &Node{Name: "a", Children: []*Node{mid}}

	out := CollapsePaths(root, ".")
	if out.Name != "a.b" {
		t.Errorf("name: got %q want %q", out.Name, "a.b")
	}
	if len(out.Children) != 2 {
		t.Fatalf("children: got %d want 2", len(out.Children))
	}
	if out.Children[0].Name != "c" || out.Children[1].Name != "d" {
		t.Errorf("children names: got %q,%q want c,d",
			out.Children[0].Name, out.Children[1].Name)
	}
	// TotalSize must still reflect the leaves.
	if got := out.TotalSize(); got != 8 {
		t.Errorf("TotalSize: got %v want 8", got)
	}
}

func TestCollapsePaths_BranchingAtRoot_NoCollapse(t *testing.T) {
	// root with 2 children; no chain to collapse at the top.
	a := &Node{Name: "a", Size: 1}
	b := &Node{Name: "b", Size: 2}
	root := &Node{Name: "r", Children: []*Node{a, b}}

	out := CollapsePaths(root, "/")
	if out.Name != "r" {
		t.Errorf("root name: got %q want %q", out.Name, "r")
	}
	if len(out.Children) != 2 {
		t.Fatalf("children: got %d want 2", len(out.Children))
	}
	if out.Children[0].Name != "a" || out.Children[1].Name != "b" {
		t.Errorf("child names not preserved")
	}
}

func TestCollapsePaths_DeepNested_RecursiveCollapse(t *testing.T) {
	// r -> {x -> y -> leaf1, z}: subtree under x collapses, z stays.
	leaf1 := &Node{Name: "leaf1", Size: 4}
	y := &Node{Name: "y", Children: []*Node{leaf1}}
	x := &Node{Name: "x", Children: []*Node{y}}
	z := &Node{Name: "z", Size: 7}
	root := &Node{Name: "r", Children: []*Node{x, z}}

	out := CollapsePaths(root, "/")
	if out.Name != "r" {
		t.Errorf("root name: got %q want r", out.Name)
	}
	if len(out.Children) != 2 {
		t.Fatalf("root children: got %d want 2", len(out.Children))
	}
	xs := out.Children[0]
	if xs.Name != "x/y/leaf1" {
		t.Errorf("first child collapsed name: got %q want %q", xs.Name, "x/y/leaf1")
	}
	if zs := out.Children[1]; zs.Name != "z" {
		t.Errorf("second child name: got %q want z", zs.Name)
	}
}

func TestCollapsePaths_DoesNotMutateInput(t *testing.T) {
	leaf := &Node{Name: "c", Size: 9}
	mid := &Node{Name: "b", Children: []*Node{leaf}}
	root := &Node{Name: "a", Children: []*Node{mid}}

	_ = CollapsePaths(root, "/")

	if root.Name != "a" {
		t.Errorf("root.Name mutated: %q", root.Name)
	}
	if len(root.Children) != 1 || root.Children[0] != mid {
		t.Errorf("root.Children mutated")
	}
	if mid.Name != "b" || mid.Children[0] != leaf {
		t.Errorf("mid mutated")
	}
	if leaf.Name != "c" || leaf.Size != 9 {
		t.Errorf("leaf mutated")
	}
}

func TestCollapsePaths_NoSharedPointersWithInput(t *testing.T) {
	leaf := &Node{Name: "c", Size: 1}
	mid := &Node{Name: "b", Children: []*Node{leaf, {Name: "d", Size: 2}}}
	root := &Node{Name: "a", Children: []*Node{mid}}

	out := CollapsePaths(root, "/")
	if out == root {
		t.Errorf("returned root pointer aliases input")
	}
	for _, ch := range out.Children {
		if ch == leaf || ch == mid {
			t.Errorf("returned tree shares a pointer with input")
		}
	}
}

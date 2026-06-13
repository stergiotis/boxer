package layout

// CollapsePaths returns a transformed copy of the tree rooted at root in
// which every chain of single-child nodes is folded into one node whose Name
// is the chain of original names joined by sep.
//
// Useful for hierarchies where deep linear segments are common — Go package
// paths, filesystem trees, Java packages — so a row of nested 1-child
// containers does not eat up screen space with empty header chrome.
//
// The transform is a pure copy: the input tree is not mutated, and the
// returned tree shares no node pointers with the input. (Important: callers
// using pointer-equality for navigation or selection must use the returned
// tree's pointers, not the originals.)
//
// Size is preserved on the merged node (taken from the deepest node in the
// chain — the only one that originally had any leaves under it). Children
// of the deepest node become children of the merged node, themselves
// recursively collapsed.
//
// Edge cases:
//   - nil root → returns nil.
//   - Leaf (no children) → returned as a fresh copy.
//   - Branching points (≥2 children) → not merged; their children are
//     recursively collapsed.
func CollapsePaths(root *Node, sep string) *Node {
	if root == nil {
		return nil
	}
	name, deepest := walkChain(root, sep)
	out := &Node{Name: name, Size: deepest.Size}
	if len(deepest.Children) > 0 {
		out.Children = make([]*Node, len(deepest.Children))
		for i, ch := range deepest.Children {
			out.Children[i] = CollapsePaths(ch, sep)
		}
	}
	return out
}

// walkChain follows single-child links from n down, accumulating names with
// sep, and returns the joined name plus the deepest node reached (the first
// one with anything other than exactly one child).
func walkChain(n *Node, sep string) (joined string, deepest *Node) {
	joined = n.Name
	cur := n
	for len(cur.Children) == 1 {
		cur = cur.Children[0]
		joined += sep + cur.Name
	}
	return joined, cur
}

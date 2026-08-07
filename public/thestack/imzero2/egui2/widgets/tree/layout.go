package tree

// Row is one visible line of the outline: a node, how deep it sits, and the
// three facts a renderer needs to draw its disclosure control and its indent
// guides without walking the tree again.
//
// A Row is what the renderer iterates, and — because the rows are a dense
// slice — what a virtualised host indexes into. That is the whole reason
// flattening is a separate step: once a hierarchy is a row sequence, showing
// only rows 40..60 is a slice expression rather than a traversal, which is
// what lets the renderer gate on egui_table's visible range (ADR-0176 SD4).
type Row struct {
	// Node indexes [Tree.Labels].
	Node int32
	// Depth is 0 for a root and one more than its parent's otherwise. It is
	// the indent level, not a pixel count — the renderer multiplies.
	Depth int32
	// HasChildren is whether this node has any children at all, which is what
	// decides if a disclosure control is drawn. It is independent of Expanded:
	// a collapsed interior node has children and shows a closed control.
	HasChildren bool
	// Expanded is whether this node's children follow it in the row sequence.
	// Always false for a leaf.
	Expanded bool
	// IsLastChild is whether this node is the final one among its siblings —
	// including at depth 0, where the siblings are the roots.
	//
	// Carried from M1 although nothing reads it yet: it is exactly what indent
	// guides need (the vertical line from a parent stops at its last child),
	// and ADR-0176 SD9 defers the guides rather than the field, so adding them
	// later needs no change here or in any caller.
	IsLastChild bool
}

// Flatten walks t in depth-first pre-order and appends one [Row] per visible
// node to dst, descending only into nodes st reports as expanded. It returns
// the extended slice.
//
// Pass dst as a retained slice's dst[:0] to reuse its backing array across
// frames; the row count is bounded by the node count, so a host that flattens
// every frame settles on one allocation.
//
// Roots appear in input order, and so do siblings. No ordering of t itself is
// required — a parent may appear after its child — because the child lists are
// built in one pass before the walk.
//
// A nil st is treated as fully collapsed, which makes "show me just the roots"
// a call with no state to construct.
//
// The error is [Tree.Validate]'s: on a structurally broken tree Flatten
// returns dst unextended rather than a partial outline, because the failure
// modes it rejects — a dangling parent index, a parent cycle — are the ones
// that would otherwise produce a plausible-looking tree missing arbitrary
// subtrees, or hang the walk.
func Flatten(t Tree, st *State, dst []Row) ([]Row, error) {
	if err := t.Validate(); err != nil {
		return dst, err
	}
	n := t.Len()
	if n == 0 {
		return dst, nil
	}

	// Child lists in input order, built as a CSR-style pair: counts, prefix
	// sums, then a fill pass. Two int32 slices for the whole forest rather
	// than a [][]int32 with a slice header and an allocation per interior
	// node — the walk only ever reads a contiguous run per parent.
	//
	// Roots are counted under a virtual slot at index n, so the same three
	// passes cover them and the root scan below is a run like any other.
	starts := make([]int32, n+2)
	for i := range n {
		p := t.Parents[i]
		if p == -1 {
			p = int32(n)
		}
		starts[p+1]++
	}
	for i := 1; i < len(starts); i++ {
		starts[i] += starts[i-1]
	}
	kids := make([]int32, n)
	fill := make([]int32, n+1)
	copy(fill, starts[:n+1])
	for i := range n {
		p := t.Parents[i]
		if p == -1 {
			p = int32(n)
		}
		kids[fill[p]] = int32(i)
		fill[p]++
	}

	// Explicit stack, not recursion: depth is bounded only by the node count,
	// and a 20k-node degenerate chain — a recursive CTE that returned a list —
	// would blow the goroutine stack on a widget doing nothing unusual.
	type frame struct {
		node  int32
		depth int32
		last  bool
	}
	stack := make([]frame, 0, 32)

	// Push the roots in reverse so the first root is popped first; same trick
	// for children below.
	rootBegin, rootEnd := starts[n], starts[n+1]
	for i := rootEnd - 1; i >= rootBegin; i-- {
		stack = append(stack, frame{node: kids[i], depth: 0, last: i == rootEnd-1})
	}

	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		begin, end := starts[f.node], starts[f.node+1]
		hasKids := end > begin
		open := hasKids && st != nil && st.IsExpanded(f.node)

		dst = append(dst, Row{
			Node:        f.node,
			Depth:       f.depth,
			HasChildren: hasKids,
			Expanded:    open,
			IsLastChild: f.last,
		})
		if !open {
			continue
		}
		for i := end - 1; i >= begin; i-- {
			stack = append(stack, frame{node: kids[i], depth: f.depth + 1, last: i == end-1})
		}
	}
	return dst, nil
}

// RowOf returns the index into rows of the row showing node, or -1 when that
// node is not currently visible. Linear, and meant for the one-off lookups a
// host does on an event — revealing a selection, scrolling to a search hit —
// not for a per-row call inside a render loop.
func RowOf(rows []Row, node int32) int {
	for i := range rows {
		if rows[i].Node == node {
			return i
		}
	}
	return -1
}

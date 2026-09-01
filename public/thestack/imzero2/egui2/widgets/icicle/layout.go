package icicle

import (
	"sort"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Node is one laid-out rectangle. X is in the tree's own value units and Y in
// row units, one row per depth, both already in plot convention (larger y is
// higher on screen) — so a renderer projects straight through the frame
// transform without knowing the orientation (ADR-0160 SD2).
type Node struct {
	// Index is the node's position in the source Tree, which is how a host
	// looks up anything the layout does not carry.
	Index int32
	// Parent indexes Layout.Nodes, not the source Tree; -1 for a root.
	Parent int32
	// Label is copied from the source tree.
	Label string
	// Depth is the row index: 0 for a root.
	Depth int32
	// Self is the node's own value, Total includes its subtree. The
	// difference is the width of the rectangle no child covers.
	Self  float64
	Total float64
	// X0 < X1 in value units; X1-X0 == Total.
	X0, X1 float64
	// Y0 < Y1 in row units, exactly one row apart.
	Y0, Y1 float64
}

// Layout is the positioned result: nodes in pre-order, indexed by row.
type Layout struct {
	// Nodes is in pre-order (a parent precedes its descendants), so a
	// renderer that draws in slice order draws parents before children.
	Nodes []Node
	// Rows indexes Nodes by depth, each row sorted by ascending X0. Pre-order
	// with left-to-right siblings produces that ordering for free, which is
	// what lets a hit test binary-search a row.
	Rows [][]int32
	// Orientation is the one Compute was given, kept so a host can map
	// between a row coordinate and a depth without repeating the sign rule.
	Orientation OrientationE
	// Report describes the tree that was laid out.
	Report Report
}

// Compute lays out t. The returned layout is independent of t: it copies the
// labels and values it needs, so the caller may reuse the input columns.
func Compute(t Tree, o Options) (*Layout, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	n := t.Len()
	depth := computeDepths(t.Parents)
	total := computeTotals(t.Self, t.Parents, depth)

	grand := 0.0
	for i := range n {
		if t.Parents[i] == -1 {
			grand += total[i]
		}
	}
	if !(grand > 0) {
		return nil, eb.Build().Float64("total", grand).Errorf("tree total value leaves nothing to lay out")
	}

	childStart, childList := buildChildIndex(t.Parents)
	roots := make([]int32, 0, 8)
	for i := range n {
		if t.Parents[i] == -1 {
			roots = append(roots, int32(i))
		}
	}
	sortSiblings(roots, t.Labels, total, o.Order)
	for i := range n {
		sortSiblings(childList[childStart[i]:childStart[i+1]], t.Labels, total, o.Order)
	}

	lay := &Layout{
		Orientation: o.Orientation,
		Nodes:       make([]Node, 0, n),
		Report:      Report{Unit: o.Unit, Total: grand},
	}
	threshold := o.MinFraction * grand

	// Pre-order walk with an explicit stack: a profile can be thousands of
	// frames deep, and the placement of a subtree needs nothing from its
	// siblings, so there is no reason to put that on the goroutine stack.
	type frame struct {
		node   int32
		parent int32 // index into lay.Nodes
		x      float64
	}
	stack := make([]frame, 0, 64)
	// offs holds one sibling group's x offsets. Offsets accumulate left to
	// right but the group is pushed right to left (so it pops in reading
	// order), so they have to be materialised — this buffer is reused across
	// the whole walk rather than allocated per node.
	offs := make([]float64, 0, 32)

	// Roots sit side by side. They are never pruned, however small: a layout
	// with nothing in it is worse than one that shows a sliver.
	offs = offs[:0]
	rx := 0.0
	for _, r := range roots {
		offs = append(offs, rx)
		rx += total[r]
	}
	for i := len(roots) - 1; i >= 0; i-- {
		stack = append(stack, frame{node: roots[i], parent: -1, x: offs[i]})
	}

	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		d := depth[f.node]
		y0, y1 := rowSpan(d, o.Orientation)
		self := t.Self[f.node]
		lay.Nodes = append(lay.Nodes, Node{
			Index:  f.node,
			Parent: f.parent,
			Label:  t.Labels[f.node],
			Depth:  d,
			Self:   self,
			Total:  total[f.node],
			X0:     f.x,
			X1:     f.x + total[f.node],
			Y0:     y0,
			Y1:     y1,
		})
		me := int32(len(lay.Nodes) - 1)
		for len(lay.Rows) <= int(d) {
			lay.Rows = append(lay.Rows, nil)
		}
		lay.Rows[d] = append(lay.Rows[d], me)

		// Children fill the parent from the left; whatever they do not cover
		// is the parent's own value, showing as the uncovered tail on the
		// right.
		kids := childList[childStart[f.node]:childStart[f.node+1]]
		offs = offs[:0]
		cx := f.x
		for _, k := range kids {
			offs = append(offs, cx)
			cx += total[k]
		}
		// Pushed right to left so they pop in reading order, which is what
		// leaves each row sorted by x.
		for i := len(kids) - 1; i >= 0; i-- {
			k := kids[i]
			if total[k] < threshold {
				// The space stays reserved and nothing is emitted into it, so
				// pruning is purely subtractive: turning it on never shifts a
				// sibling that survived it.
				lay.Report.Pruned += countSubtree(k, childStart, childList)
				lay.Report.PrunedValue += total[k]
				continue
			}
			stack = append(stack, frame{node: k, parent: me, x: offs[i]})
		}
	}

	lay.Report.Nodes = len(lay.Nodes)
	lay.Report.Rows = len(lay.Rows)
	lay.Report.MaxDepth = len(lay.Rows) - 1
	return lay, nil
}

// rowSpan places depth d on the row axis. The orientation is nothing but the
// sign: an icicle grows downward from y=0, a flamegraph upward.
func rowSpan(d int32, orient OrientationE) (y0 float64, y1 float64) {
	if orient == OrientFlame {
		return float64(d), float64(d + 1)
	}
	return -float64(d + 1), -float64(d)
}

// RowDist is a row coordinate's distance from the root edge, in row units —
// the depth before flooring, and the inverse of rowSpan's sign.
//
// It is exported because it is the whole of the orientation convention read
// backwards, and a renderer resolving its own visible row window needs that
// without wanting a depth: writing the sign rule out a second time is how one
// copy gets fixed and the other does not.
func (l *Layout) RowDist(y float64) float64 {
	if l == nil || l.Orientation == OrientFlame {
		return y
	}
	return -y
}

// DepthAt maps a row coordinate back to a depth, undoing rowSpan. ok is false
// for a coordinate outside the laid-out rows.
func (l *Layout) DepthAt(y float64) (depth int, ok bool) {
	if l == nil || len(l.Rows) == 0 {
		return 0, false
	}
	// Row d spans [d, d+1) going up, or [-(d+1), -d) going down; either way
	// the depth is the floor of the distance from the root edge. Truncation
	// is the floor here because that distance is never negative.
	dist := l.RowDist(y)
	// Both bounds are checked before the conversion, and the lower one is
	// written as a negated >= so that a NaN fails it. A float the int range
	// cannot hold converts to an implementation-defined value — INT_MIN on
	// amd64 — which would then pass any bound expressed as `d >= len(Rows)`
	// and index the row slice out of range.
	if !(dist >= 0) || dist >= float64(len(l.Rows)) {
		return 0, false
	}
	return int(dist), true
}

// NodeAt resolves a plot-space point to a node index, or -1. It maps y to a
// row by arithmetic and binary-searches that row by x, so it costs O(log n)
// and stays correct at any zoom — it never touches pixels (ADR-0160 SD5).
func (l *Layout) NodeAt(x float64, y float64) int {
	d, ok := l.DepthAt(y)
	if !ok {
		return -1
	}
	row := l.Rows[d]
	// The first node whose X1 is strictly greater than x; that is the only
	// candidate, because siblings abut without overlapping.
	i := sort.Search(len(row), func(k int) bool { return l.Nodes[row[k]].X1 > x })
	if i >= len(row) {
		return -1
	}
	if nd := &l.Nodes[row[i]]; x >= nd.X0 && x < nd.X1 {
		return int(row[i])
	}
	return -1
}

// PathTo returns the ancestor chain of a layout node, root first and including
// the node itself — what a host needs for a breadcrumb or a tooltip. It
// returns nil for an out-of-range index.
func (l *Layout) PathTo(i int) []int32 {
	if l == nil || i < 0 || i >= len(l.Nodes) {
		return nil
	}
	var rev []int32
	for v := int32(i); v >= 0; v = l.Nodes[v].Parent {
		rev = append(rev, v)
	}
	for a, b := 0, len(rev)-1; a < b; a, b = a+1, b-1 {
		rev[a], rev[b] = rev[b], rev[a]
	}
	return rev
}

// computeDepths walks each node up to a root, memoizing as it unwinds so the
// whole pass is linear. Validate has already ruled out cycles.
func computeDepths(parents []int32) []int32 {
	n := len(parents)
	depth := make([]int32, n)
	for i := range depth {
		depth[i] = -1
	}
	chain := make([]int32, 0, 32)
	for i := range n {
		if depth[i] >= 0 {
			continue
		}
		chain = chain[:0]
		v := int32(i)
		for v >= 0 && depth[v] < 0 {
			chain = append(chain, v)
			v = parents[v]
		}
		// base is the depth of the first node already known; -1 when the walk
		// ran off the top, which makes the last node pushed a root at 0.
		base := int32(-1)
		if v >= 0 {
			base = depth[v]
		}
		for j := len(chain) - 1; j >= 0; j-- {
			base++
			depth[chain[j]] = base
		}
	}
	return depth
}

// computeTotals rolls self values up the tree. Accumulating deepest-first
// means every child is final before its parent is read, without recursion.
func computeTotals(self []float64, parents []int32, depth []int32) []float64 {
	n := len(self)
	total := make([]float64, n)
	copy(total, self)
	maxD := int32(0)
	for _, d := range depth {
		if d > maxD {
			maxD = d
		}
	}
	// Counting sort by depth, then read back deepest-first.
	counts := make([]int32, maxD+2)
	for _, d := range depth {
		counts[d+1]++
	}
	for i := 1; i < len(counts); i++ {
		counts[i] += counts[i-1]
	}
	order := make([]int32, n)
	cursor := make([]int32, maxD+1)
	for i := range n {
		d := depth[i]
		order[counts[d]+cursor[d]] = int32(i)
		cursor[d]++
	}
	for i := n - 1; i >= 0; i-- {
		v := order[i]
		if p := parents[v]; p >= 0 {
			total[p] += total[v]
		}
	}
	return total
}

// buildChildIndex returns a flat children index: childList[start[i]:start[i+1]]
// are node i's children, in input order.
func buildChildIndex(parents []int32) (start []int32, childList []int32) {
	n := len(parents)
	start = make([]int32, n+1)
	for _, p := range parents {
		if p >= 0 {
			start[p+1]++
		}
	}
	for i := 1; i <= n; i++ {
		start[i] += start[i-1]
	}
	childList = make([]int32, start[n])
	cursor := make([]int32, n)
	for i := range n {
		if p := parents[i]; p >= 0 {
			childList[start[p]+cursor[p]] = int32(i)
			cursor[p]++
		}
	}
	return start, childList
}

// sortSiblings arranges one sibling group in place. Every order breaks ties by
// input position, so a layout is reproducible for a given tree.
func sortSiblings(group []int32, labels []string, total []float64, order OrderE) {
	switch order {
	case OrderLabel:
		sort.SliceStable(group, func(a, b int) bool {
			return labels[group[a]] < labels[group[b]]
		})
	case OrderValueDesc:
		sort.SliceStable(group, func(a, b int) bool {
			return total[group[a]] > total[group[b]]
		})
	}
}

// countSubtree counts a pruned node and everything under it, for the report.
func countSubtree(root int32, start []int32, childList []int32) int {
	count := 0
	stack := []int32{root}
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		stack = append(stack, childList[start[v]:start[v+1]]...)
	}
	return count
}

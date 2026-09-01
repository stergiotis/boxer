// Package icicle lays out a hierarchy as an icicle plot: one row per depth,
// each node a rectangle whose width is its value, children abutting under
// their parent. Inverted — root at the bottom, growing up — the same layout is
// a flamegraph (ADR-0160).
//
// The package is UI-free. This file declares the input model and its
// validation; layout.go turns a Tree into positioned geometry and answers hit
// tests against it; ./view — the only half that imports the egui2 bindings —
// draws that geometry as implot custom items. The split mirrors sankey
// (ADR-0159) and layeredgraph (ADR-0069).
//
// Geometry is emitted in plot space, not in a unit box: x carries the value in
// its own units, so an implot x axis reads as samples, bytes or seconds and
// auto-fit means something, while y is in row units, one per depth. A renderer
// projects through the frame transform and lets implot own pan and zoom
// (ADR-0160 SD2).
package icicle

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Tree is the input hierarchy in columnar form: three parallel slices, one
// entry per node. It is deliberately not a pointer tree — the data this widget
// renders arrives flat (a profile's stacks, a recursive query's rows), and
// demanding a tree would make every producer build one first (ADR-0160 SD1).
//
// A node's total is its own Self plus the total of its children, so a parent's
// rectangle is wider than its children's and the uncovered remainder is its
// self value. That is the whole reason this type does not reuse treemap's
// layout.Node, whose parents derive their size from children alone: a function
// with both self samples and callees would lose the self time.
type Tree struct {
	// Labels is what the renderer draws for each node.
	Labels []string
	// Parents indexes each node's parent, or -1 for a root. Several roots are
	// allowed and are laid out side by side at depth 0 — a forest is the
	// natural shape of a per-goroutine or per-thread profile, and inventing a
	// virtual root would state a total no sample supports.
	//
	// No ordering is required: a parent may appear after its child.
	Parents []int32
	// Self is the value attributed to the node itself, excluding its
	// children. Must be finite and non-negative; a rectangle cannot have
	// negative width, and a differential flamegraph needs a diff model this
	// cut does not have (ADR-0160 SD8).
	Self []float64
}

// Len is the node count, which every column shares.
func (t Tree) Len() int { return len(t.Labels) }

// OrientationE selects which way the rows grow. Both are one layout; only the
// sign of the row coordinate differs.
type OrientationE uint8

const (
	// OrientIcicle puts the root at the top and grows downward — the icicle
	// plot proper (Kruskal & Landwehr, 1983).
	OrientIcicle OrientationE = iota
	// OrientFlame puts the root at the bottom and grows upward — the
	// flamegraph orientation.
	OrientFlame
)

// OrderE selects how siblings are arranged left to right.
type OrderE uint8

const (
	// OrderValueDesc puts the widest sibling first. The default: it reads
	// left-heavy, so the eye finds the expensive path without hunting.
	OrderValueDesc OrderE = iota
	// OrderLabel sorts siblings by label. Slightly worse to scan and better to
	// compare — the same tree laid out twice puts a given frame in the same
	// place, which is what makes two captures visually diffable.
	OrderLabel
	// OrderInput keeps the order the children appear in the input.
	OrderInput
)

// Options tunes the layout. The zero value is usable: an icicle, widest child
// first, nothing pruned.
type Options struct {
	// Orientation selects icicle (root at top) or flamegraph (root at bottom).
	Orientation OrientationE
	// Order arranges siblings left to right.
	Order OrderE
	// MinFraction prunes any subtree whose total is below this fraction of the
	// grand total, counting what it dropped in the Report. 0 keeps everything.
	//
	// This is resolution-independent pruning, deliberately distinct from the
	// view's sub-pixel culling: pruning changes the layout and is reproducible,
	// culling only skips what cannot be seen at the current zoom
	// (ADR-0160 SD7).
	MinFraction float64
	// Unit labels the value axis; carried through to the Report untouched.
	Unit string
}

// Report is what the layout can tell the host about the tree it just laid out.
// It is descriptive, not advisory — nothing in it stops a plot rendering.
type Report struct {
	// Unit is Options.Unit, carried through for labelling.
	Unit string
	// Total is the summed total of every root, i.e. the full width of the plot.
	Total float64
	// Rows is the depth count: 1 for a tree of roots alone.
	Rows int
	// Nodes is how many nodes survived pruning and appear in the layout.
	Nodes int
	// Pruned counts nodes dropped by Options.MinFraction, and PrunedValue is
	// the value they carried. A host that wants to be honest about a pruned
	// picture can say how much is missing.
	Pruned      int
	PrunedValue float64
	// MaxDepth is the deepest row index present, i.e. Rows-1.
	MaxDepth int
}

// Validate reports the first structural problem with t, or nil. Compute calls
// it, so calling it separately is only useful to check input before laying out.
func (t Tree) Validate() error {
	n := len(t.Labels)
	if n == 0 {
		return eh.Errorf("tree has no nodes")
	}
	if len(t.Parents) != n || len(t.Self) != n {
		return eb.Build().Int("labels", n).Int("parents", len(t.Parents)).Int("selfValues", len(t.Self)).
			Errorf("column lengths disagree")
	}
	for i := range n {
		p := t.Parents[i]
		if p == int32(i) {
			return eb.Build().Int("node", i).Errorf("node is its own parent")
		}
		if p < -1 || int(p) >= n {
			return eb.Build().Int("node", i).Int32("parent", p).Int("nodes", n).Errorf("node parent is out of range")
		}
		if v := t.Self[i]; math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return eb.Build().Int("node", i).Float64("value", v).Errorf("node self value must be finite and >= 0")
		}
	}
	return t.checkAcyclic()
}

// checkAcyclic walks every node to a root, memoizing the nodes already known
// to reach one. Each node is entered at most twice across the whole run, so
// this stays linear; the walk length is bounded by the node count, which is
// what catches a cycle without a separate colour array.
func (t Tree) checkAcyclic() error {
	n := len(t.Parents)
	grounded := make([]bool, n)
	path := make([]int32, 0, 16)
	for i := range n {
		if grounded[i] {
			continue
		}
		path = path[:0]
		v := int32(i)
		for v != -1 && !grounded[v] {
			if len(path) > n {
				return eb.Build().Int("node", i).Errorf("node lies on a parent cycle")
			}
			path = append(path, v)
			v = t.Parents[v]
		}
		for _, w := range path {
			grounded[w] = true
		}
	}
	return nil
}

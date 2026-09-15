package explain

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TreeOptions are the fit's knobs (ADR-0235 §SD1).
type TreeOptions struct {
	// MaxDepth bounds the tree; a rule has at most MaxDepth terms before
	// merging. 0 means the default.
	MaxDepth int
	// MinLeaf is the smallest number of fitted rows either side of a split
	// may hold. 0 means the default.
	MinLeaf int
}

const (
	defaultMaxDepth = 6
	defaultMinLeaf  = 5
	// maxMaxDepth caps a caller's depth: the tree is walked recursively and
	// a rule past this reads as nothing.
	maxMaxDepth = 32
)

func (inst TreeOptions) withDefaults() TreeOptions {
	if inst.MaxDepth <= 0 {
		inst.MaxDepth = defaultMaxDepth
	}
	if inst.MaxDepth > maxMaxDepth {
		inst.MaxDepth = maxMaxDepth
	}
	if inst.MinLeaf <= 0 {
		inst.MinLeaf = defaultMinLeaf
	}
	return inst
}

// Node is one node of a Tree. Rows whose value at Feature is at most
// Threshold go Left, the rest Right. A leaf has Feature −1 and no children.
type Node struct {
	Depth int32
	// Parent is the index of the node's parent, −1 at the root.
	Parent    int32
	Feature   int32
	Threshold float64
	Left      int32
	Right     int32
	// Label is the majority label among the fitted rows reaching the node,
	// the lower label on a tie.
	Label int32
	// Rows is the number of fitted rows reaching the node; Counts is that
	// number by label.
	Rows   int32
	Counts []int32
}

// IsLeaf reports whether the node was not split.
func (inst *Node) IsLeaf() bool {
	return inst.Feature < 0
}

// Tree is a threshold tree fitted to a labelling. Nodes[0] is the root and
// every child follows its parent. Because the fit is greedy top-down, the
// tree cut at any depth is the tree the same fit would have grown to that
// depth, which is why Rules and Fidelity take a depth rather than the fit
// being repeated.
type Tree struct {
	Nodes []Node
	// NumLabels is one past the largest label seen.
	NumLabels int
	// NumFeatures is the matrix's column count.
	NumFeatures int
	// Leaf is, per input row, the index of the deepest node it reaches, or
	// −1 for a row that was not fitted (its label is negative).
	Leaf []int32
	// Fitted is the number of rows with a non-negative label; the rest were
	// left out of the fit.
	Fitted int
	// Depth is the deepest node's depth.
	Depth      int
	Options    TreeOptions
	Truncation algo.Truncation
}

// FitTree fits a threshold tree to labels over the row-major matrix x with
// d columns: CART with the Gini impurity, splits at the midpoints of
// adjacent distinct values, the best split by impurity decrease with ties
// going to the lower feature then the lower threshold. Rows whose label is
// negative are left out; the rest carry labels in [0, NumLabels). The fit
// stops at a node when it is pure, when it is shallower than MaxDepth by
// nothing, when no split leaves MinLeaf rows on both sides, or when no split
// lowers the impurity. A cancelled context stops splitting — the nodes not
// yet visited become leaves — and the Truncation says so.
func FitTree(ctx context.Context, x []float64, d int, labels []int32, opts TreeOptions) (t *Tree, err error) {
	opts = opts.withDefaults()
	n := len(labels)
	if d <= 0 || len(x) != n*d {
		err = eb.Build().Int("rows", n).Int("cols", d).Int("len", len(x)).Errorf("explain: the matrix does not have rows × cols values")
		return
	}
	k := 0
	fitted := make([]int32, 0, n)
	for r, lb := range labels {
		if lb < 0 {
			continue
		}
		fitted = append(fitted, int32(r))
		k = max(k, int(lb)+1)
	}
	if k == 0 {
		err = eb.Build().Int("rows", n).Errorf("explain: no row carries a label")
		return
	}
	t = &Tree{
		NumLabels:   k,
		NumFeatures: d,
		Leaf:        make([]int32, n),
		Fitted:      len(fitted),
		Options:     opts,
	}
	for r := range t.Leaf {
		t.Leaf[r] = -1
	}
	f := &fitter{
		ctx:     ctx,
		x:       x,
		d:       d,
		labels:  labels,
		opts:    opts,
		t:       t,
		rows:    fitted,
		orders:  make([][]int32, d),
		inNode:  make([]bool, n),
		scratch: make([]int32, len(fitted)),
		left:    make([]int32, k),
		right:   make([]int32, k),
	}
	// Each feature is sorted once over the fitted rows, by value then by
	// row; a node's order for the feature is this order filtered to its
	// rows, so a node costs O(d·n) rather than d sorts.
	for fi := range d {
		order := slices.Clone(fitted)
		col := func(r int32) float64 { return x[int(r)*d+fi] }
		slices.SortFunc(order, func(a, b int32) int {
			if c := cmp.Compare(col(a), col(b)); c != 0 {
				return c
			}
			return cmp.Compare(a, b)
		})
		f.orders[fi] = order
	}
	f.grow(0, len(fitted), 0, -1)
	if f.cancelled {
		t.Truncation = algo.Truncation{Truncated: true, By: algo.LimitContext}
	}
	return
}

// FitOneVsRest fits a tree that separates one label from every other row
// (ADR-0235 §SD1a): rows carrying target become label 1 and all the rest,
// noise included, label 0, and FitTree runs over that. The question it
// answers is "what is in this cluster" against the whole picture, which is
// why noise is on the rest side here and left out of the multi-label fit.
// RulesFor(depth, 1) then reads the target's rules.
func FitOneVsRest(ctx context.Context, x []float64, d int, labels []int32, target int32, opts TreeOptions) (t *Tree, err error) {
	binary := make([]int32, len(labels))
	found := false
	for r, lb := range labels {
		if lb == target {
			binary[r] = 1
			found = true
		}
	}
	if !found {
		err = eb.Build().Int32("label", target).Errorf("explain: no row carries the label")
		return
	}
	return FitTree(ctx, x, d, binary, opts)
}

type fitter struct {
	ctx    context.Context
	x      []float64
	d      int
	labels []int32
	opts   TreeOptions
	t      *Tree
	// rows is the fitted rows, partitioned in place as the tree grows; a
	// node owns rows[lo:hi].
	rows []int32
	// orders is, per feature, the fitted rows by value then row; inNode
	// marks the rows of the node being split, so a feature's order over
	// the node is a filtered walk.
	orders    [][]int32
	inNode    []bool
	scratch   []int32
	left      []int32
	right     []int32
	cancelled bool
}

// grow appends the node over rows[lo:hi] at depth and, when it splits,
// its children after it. Returns the node's index.
func (inst *fitter) grow(lo, hi int, depth int32, parent int32) (node int32) {
	t := inst.t
	node = int32(len(t.Nodes))
	counts := make([]int32, t.NumLabels)
	for _, r := range inst.rows[lo:hi] {
		counts[inst.labels[r]]++
	}
	label, top := int32(0), int32(-1)
	for lb, c := range counts {
		if c > top {
			label, top = int32(lb), c
		}
	}
	t.Nodes = append(t.Nodes, Node{
		Depth: depth, Parent: parent, Feature: -1, Left: -1, Right: -1,
		Label: label, Rows: int32(hi - lo), Counts: counts,
	})
	t.Depth = max(t.Depth, int(depth))

	m := hi - lo
	if !inst.cancelled && inst.ctx.Err() != nil {
		inst.cancelled = true
	}
	if inst.cancelled || int(depth) >= inst.opts.MaxDepth || m < 2*inst.opts.MinLeaf || int(top) == m {
		inst.assignLeaf(lo, hi, node)
		return
	}
	feature, threshold, nLeft, ok := inst.bestSplit(lo, hi, counts)
	if !ok {
		inst.assignLeaf(lo, hi, node)
		return
	}
	// Partition rows[lo:hi] stably: the rows at or under the threshold
	// first, in their current order, then the rest.
	seg := inst.rows[lo:hi]
	l, r := 0, nLeft
	for _, row := range seg {
		if inst.x[int(row)*inst.d+int(feature)] <= threshold {
			inst.scratch[l] = row
			l++
		} else {
			inst.scratch[r] = row
			r++
		}
	}
	copy(seg, inst.scratch[:m])
	t.Nodes[node].Feature = feature
	t.Nodes[node].Threshold = threshold
	left := inst.grow(lo, lo+nLeft, depth+1, node)
	right := inst.grow(lo+nLeft, hi, depth+1, node)
	t.Nodes[node].Left = left
	t.Nodes[node].Right = right
	return
}

func (inst *fitter) assignLeaf(lo, hi int, node int32) {
	for _, r := range inst.rows[lo:hi] {
		inst.t.Leaf[r] = node
	}
}

// bestSplit scans every feature over the node's rows for the split with
// the largest Gini decrease that leaves MinLeaf rows on both sides.
func (inst *fitter) bestSplit(lo, hi int, counts []int32) (feature int32, threshold float64, nLeft int, ok bool) {
	m := hi - lo
	parent := gini(counts, m)
	best := 0.0
	seg := inst.rows[lo:hi]
	for _, r := range seg {
		inst.inNode[r] = true
	}
	defer func() {
		for _, r := range seg {
			inst.inNode[r] = false
		}
	}()
	order := inst.scratch[:m]
	for f := range inst.d {
		order = order[:0]
		for _, r := range inst.orders[f] {
			if inst.inNode[r] {
				order = append(order, r)
			}
		}
		for lb := range inst.left {
			inst.left[lb] = 0
		}
		copy(inst.right, counts)
		// The two impurities are kept as sums of squared counts, so moving
		// one row across the candidate threshold is O(1): the left side's
		// n·(1 − Σc²/n²) is n − Σc²/n.
		sl, sr := 0.0, 0.0
		for _, c := range counts {
			sr += float64(c) * float64(c)
		}
		for i := 0; i+1 < m; i++ {
			lb := inst.labels[order[i]]
			sl += 2*float64(inst.left[lb]) + 1
			sr -= 2*float64(inst.right[lb]) - 1
			inst.left[lb]++
			inst.right[lb]--
			nl := i + 1
			nr := m - nl
			if nl < inst.opts.MinLeaf || nr < inst.opts.MinLeaf {
				continue
			}
			a, b := inst.x[int(order[i])*inst.d+f], inst.x[int(order[i+1])*inst.d+f]
			if a == b {
				continue
			}
			gain := parent - ((float64(nl)-sl/float64(nl))+(float64(nr)-sr/float64(nr)))/float64(m)
			if gain > best {
				best = gain
				feature = int32(f)
				threshold = shortestBetween(a, b)
				nLeft = nl
				ok = true
			}
		}
	}
	return
}

// shortestBetween returns the decimal with the fewest significant digits
// in [a, b), a < b, preferring the one nearest the midpoint at that
// precision: the split "x <= t" partitions the rows exactly as the
// midpoint would, and t reads — and round-trips through SQL — as 37
// rather than 37.150000000000006.
func shortestBetween(a, b float64) float64 {
	mid := a + (b-a)/2
	for digits := 1; digits <= 17; digits++ {
		for _, cand := range [...]float64{
			roundSig(mid, digits), roundSig(a, digits), roundSig(b, digits),
		} {
			if cand >= a && cand < b {
				return cand
			}
		}
	}
	return mid
}

// roundSig rounds v to digits significant decimal digits by way of its
// shortest text form, so the result is the value that text parses to.
func roundSig(v float64, digits int) float64 {
	r, err := strconv.ParseFloat(strconv.FormatFloat(v, 'g', digits, 64), 64)
	if err != nil {
		return v
	}
	return r
}

// gini is the Gini impurity of counts over n rows.
func gini(counts []int32, n int) float64 {
	if n == 0 {
		return 0
	}
	s := 0.0
	for _, c := range counts {
		p := float64(c) / float64(n)
		s += p * p
	}
	return 1 - s
}

// Depth-cut helpers ----------------------------------------------------------

// cut reports whether node is a leaf of the tree cut at depth.
func (inst *Tree) cut(node int32, depth int) bool {
	nd := &inst.Nodes[node]
	return nd.IsLeaf() || int(nd.Depth) >= depth
}

// LeafAt returns the node a row reaches in the tree cut at depth, or −1
// for a row that was not fitted. The tree keeps no matrix, so the node is
// found from the row's full-depth leaf by its ancestry: the leaf descends
// from the node the row reaches at any shallower cut.
func (inst *Tree) LeafAt(row int, depth int) (node int32) {
	if row < 0 || row >= len(inst.Leaf) || inst.Leaf[row] < 0 {
		return -1
	}
	node = inst.Leaf[row]
	for int(inst.Nodes[node].Depth) > depth {
		node = inst.Nodes[node].Parent
	}
	return
}

// Fidelity is the share of fitted rows whose label the tree cut at depth
// predicts, as a count and the fitted total.
func (inst *Tree) Fidelity(depth int) (agree, fitted int) {
	fitted = inst.Fitted
	if len(inst.Nodes) == 0 {
		return
	}
	inst.walk(0, depth, func(node int32) {
		agree += int(inst.Nodes[node].Counts[inst.Nodes[node].Label])
	})
	return
}

// walk calls visit on every leaf of the tree cut at depth, in pre-order.
func (inst *Tree) walk(node int32, depth int, visit func(node int32)) {
	if inst.cut(node, depth) {
		visit(node)
		return
	}
	nd := &inst.Nodes[node]
	inst.walk(nd.Left, depth, visit)
	inst.walk(nd.Right, depth, visit)
}

// Rules -----------------------------------------------------------------------

// Term is one comparison of a rule: the value at Feature is above
// Threshold when Above, otherwise at most Threshold.
type Term struct {
	Feature   int32
	Above     bool
	Threshold float64
}

// Rule is a leaf read as a conjunction of terms, one term per (feature,
// side) with the tightest bound along the path.
type Rule struct {
	Terms []Term
	// Node is the leaf the rule describes.
	Node int32
	// Label is the leaf's majority label; Rows the fitted rows the rule
	// covers; Hits those of them carrying Label.
	Label int32
	Rows  int32
	Hits  int32
	// Precision is Hits over Rows; Recall is Hits over every fitted row
	// carrying Label.
	Precision float32
	Recall    float32
}

// Rules reads the leaves of the tree cut at depth as rules, in pre-order.
// A leaf no fitted row reaches is skipped.
func (inst *Tree) Rules(depth int) (rules []Rule) {
	if len(inst.Nodes) == 0 {
		return
	}
	root := &inst.Nodes[0]
	var path []Term
	var rec func(node int32)
	rec = func(node int32) {
		nd := &inst.Nodes[node]
		if inst.cut(node, depth) {
			if nd.Rows == 0 {
				return
			}
			hits := nd.Counts[nd.Label]
			r := Rule{
				Terms:     mergeTerms(path),
				Node:      node,
				Label:     nd.Label,
				Rows:      nd.Rows,
				Hits:      hits,
				Precision: float32(hits) / float32(nd.Rows),
			}
			if total := root.Counts[nd.Label]; total > 0 {
				r.Recall = float32(hits) / float32(total)
			}
			rules = append(rules, r)
			return
		}
		path = append(path, Term{Feature: nd.Feature, Above: false, Threshold: nd.Threshold})
		rec(nd.Left)
		path[len(path)-1].Above = true
		rec(nd.Right)
		path = path[:len(path)-1]
	}
	rec(0)
	return
}

// RulesFor is Rules(depth) narrowed to the leaves predicting label.
func (inst *Tree) RulesFor(depth int, label int32) (rules []Rule) {
	for _, r := range inst.Rules(depth) {
		if r.Label == label {
			rules = append(rules, r)
		}
	}
	return
}

// Coverage sums a rule set read off one tree — leaves are disjoint, so the
// rows and hits add — into the rows it covers, the hits among them, and
// precision and recall against the label's total (recall from the first
// rule's, since Recall is Hits over that total).
func Coverage(rules []Rule) (rows, hits int32, precision, recall float32) {
	var total float32
	for _, r := range rules {
		rows += r.Rows
		hits += r.Hits
		if r.Recall > 0 && total == 0 {
			total = float32(r.Hits) / r.Recall
		}
	}
	if rows > 0 {
		precision = float32(hits) / float32(rows)
	}
	if total > 0 {
		recall = float32(hits) / total
	}
	return
}

// SQL writes the rule as a predicate: terms joined by AND, `<=` and `>`,
// thresholds in their shortest exact form; a rule with no terms is `true`.
// name spells a feature as a column reference.
func (inst Rule) SQL(name func(feature int32) string) string {
	if len(inst.Terms) == 0 {
		return "true"
	}
	var b strings.Builder
	for i, t := range inst.Terms {
		if i > 0 {
			b.WriteString(" AND ")
		}
		b.WriteString(name(t.Feature))
		if t.Above {
			b.WriteString(" > ")
		} else {
			b.WriteString(" <= ")
		}
		b.WriteString(strconv.FormatFloat(t.Threshold, 'g', -1, 64))
	}
	return b.String()
}

// SQL writes a rule set as the disjunction of its rules, each in
// parentheses when there is more than one; an empty set is `false`.
func SQL(rules []Rule, name func(feature int32) string) string {
	switch len(rules) {
	case 0:
		return "false"
	case 1:
		return rules[0].SQL(name)
	}
	var b strings.Builder
	for i, r := range rules {
		if i > 0 {
			b.WriteString(" OR ")
		}
		b.WriteString("(")
		b.WriteString(r.SQL(name))
		b.WriteString(")")
	}
	return b.String()
}

// mergeTerms keeps, per feature and side, the tightest bound: the lowest
// upper bound and the highest lower bound. Terms come out ordered by
// feature, the lower bound before the upper.
func mergeTerms(path []Term) (terms []Term) {
	if len(path) == 0 {
		return []Term{}
	}
	terms = make([]Term, 0, len(path))
	for _, t := range path {
		found := false
		for i := range terms {
			if terms[i].Feature != t.Feature || terms[i].Above != t.Above {
				continue
			}
			found = true
			if t.Above {
				terms[i].Threshold = max(terms[i].Threshold, t.Threshold)
			} else {
				terms[i].Threshold = min(terms[i].Threshold, t.Threshold)
			}
		}
		if !found {
			terms = append(terms, t)
		}
	}
	slices.SortFunc(terms, func(a, b Term) int {
		if c := cmp.Compare(a.Feature, b.Feature); c != 0 {
			return c
		}
		return cmp.Compare(boolInt(b.Above), boolInt(a.Above))
	})
	return
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Format writes the rule's terms as text: name(feature) and value(feature,
// threshold) supply the spellings; a rule with no terms reads as "always".
func (inst Rule) Format(name func(feature int32) string, value func(feature int32, v float64) string) string {
	if len(inst.Terms) == 0 {
		return "always"
	}
	var b strings.Builder
	for i, t := range inst.Terms {
		if i > 0 {
			b.WriteString(" ∧ ")
		}
		op := " ≤ "
		if t.Above {
			op = " > "
		}
		b.WriteString(name(t.Feature))
		b.WriteString(op)
		b.WriteString(value(t.Feature, t.Threshold))
	}
	return b.String()
}

// String is Format with the feature index and %.4g as spellings.
func (inst Rule) String() string {
	return inst.Format(
		func(f int32) string { return fmt.Sprintf("x%d", f) },
		func(_ int32, v float64) string { return fmt.Sprintf("%.4g", v) })
}

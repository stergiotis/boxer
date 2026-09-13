package algo

import (
	"context"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"math"
	"sort"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// HDBSCANOptions tunes [HDBSCAN].
type HDBSCANOptions struct {
	// MinClusterSize is the smallest group that counts as a cluster rather
	// than as points falling out of one (Campello, Moulavi & Sander 2013).
	// Default 5.
	MinClusterSize int
	// AllowSingleCluster lets the root of a connected graph — every point —
	// be selected when it is more stable than its children; off, that root
	// is never a cluster, which is the reference behaviour. A disconnected
	// graph has a root per component and each competes on its own.
	AllowSingleCluster bool
}

func (o HDBSCANOptions) withDefaults() HDBSCANOptions {
	if o.MinClusterSize < 2 {
		o.MinClusterSize = 5
	}
	return o
}

// HDBSCANResult is the partition: a label per slot, −1 for noise, and a
// stability per label.
type HDBSCANResult struct {
	Label []int32
	// Probability is the strength of a point's membership in its cluster,
	// its λ over the cluster's largest λ; 0 for noise.
	Probability []float32
	// Stability is indexed by label: the excess of mass the cluster was
	// selected on.
	Stability   []float32
	NumClusters int
	// Heights are the merge distances of the single-linkage hierarchy under
	// mutual reachability, ascending: one per merge, n minus the number of
	// components in all.
	Heights    []float32
	Truncation Truncation
}

// HDBSCAN clusters the vertices of a distance-weighted undirected graph
// (ADR-0230 §SD3): each arc's distance is raised to the mutual reachability
// distance max(core[s], core[d], w), a minimum spanning forest is built by
// Kruskal over the arcs in that order, the single-linkage hierarchy it
// implies is condensed by MinClusterSize, and clusters are selected by
// stability, the excess of mass over λ = 1/distance. Over a neighbour graph
// rather than the complete graph the forest disconnects only what the
// graph disconnects. core is the per-slot core distance — the producer's
// CoreDist — or nil for plain single linkage. The result is a function of
// the input alone: ties in distance break by slot pair.
func HDBSCAN(ctx context.Context, g *csr.Graph, core []float32, opts HDBSCANOptions) (r HDBSCANResult, err error) {
	opts = opts.withDefaults()
	n := g.NumVertices()
	if !g.IsWeighted() {
		err = eb.Build().Errorf("hdbscan: the graph carries no distances")
		return
	}
	if g.IsDirected() {
		err = eb.Build().Errorf("hdbscan: the graph must be undirected")
		return
	}
	if core != nil && len(core) != n {
		err = eb.Build().Int("core", len(core)).Int("vertices", n).Errorf("hdbscan: one core distance per slot required")
		return
	}
	r.Label = make([]int32, n)
	r.Probability = make([]float32, n)
	fill(r.Label, -1)
	if n == 0 {
		return
	}

	// Mutual reachability over each undirected edge once.
	type edge struct {
		s, d int32
		w    float32
	}
	edges := make([]edge, 0, g.NumArcs()/2+1)
	offs := g.OutOffsets()
	tgts := g.OutTargets()
	for s := range n {
		ws := g.OutWeights(int32(s))
		for a := offs[s]; a < offs[s+1]; a++ {
			d := tgts[a]
			if d == int32(s) {
				continue
			}
			if d < int32(s) {
				continue // its mirror is taken from d's row
			}
			w := ws[a-offs[s]]
			if core != nil {
				w = max(w, core[s], core[d])
			}
			edges = append(edges, edge{int32(s), d, w})
		}
	}
	sort.Slice(edges, func(a, b int) bool {
		ea, eb2 := edges[a], edges[b]
		if ea.w != eb2.w {
			return ea.w < eb2.w
		}
		lo1, hi1 := min(ea.s, ea.d), max(ea.s, ea.d)
		lo2, hi2 := min(eb2.s, eb2.d), max(eb2.s, eb2.d)
		if lo1 != lo2 {
			return lo1 < lo2
		}
		return hi1 < hi2
	})
	if engine.ContextDone(ctx) {
		r.Truncation = truncatedBy(LimitContext)
		return
	}

	// Kruskal, building the single-linkage dendrogram as it goes: leaves
	// 0..n-1 are the slots, internal node n+t is the t-th merge. A merge's
	// height is its edge's mutual reachability distance.
	parent := make([]int32, n)     // union–find
	repNode := make([]int32, n)    // dendrogram node currently representing a root
	left := make([]int32, 0, n-1)  // per internal node
	right := make([]int32, 0, n-1) // per internal node
	height := make([]float32, 0, n-1)
	size := make([]int32, 2*n-1)
	for i := range n {
		parent[i] = int32(i)
		repNode[i] = int32(i)
		size[i] = 1
	}
	find := func(v int32) int32 {
		for parent[v] != v {
			parent[v] = parent[parent[v]]
			v = parent[v]
		}
		return v
	}
	for _, e := range edges {
		a, b := find(e.s), find(e.d)
		if a == b {
			continue
		}
		node := int32(n + len(left))
		la, lb := repNode[a], repNode[b]
		left = append(left, la)
		right = append(right, lb)
		height = append(height, e.w)
		size[node] = size[la] + size[lb]
		// Union by size; the root's representative becomes the new node.
		if size[la] < size[lb] {
			a, b = b, a
		}
		parent[b] = a
		repNode[a] = node
	}
	r.Heights = height
	// Roots of the forest, in slot order of their smallest member — the
	// order find visits them in.
	var roots []int32
	seen := make([]bool, n)
	for i := range n {
		rt := find(int32(i))
		if !seen[rt] {
			seen[rt] = true
			roots = append(roots, repNode[rt])
		}
	}

	// λ = 1/distance; a zero distance takes a finite λ above every other so
	// coincident points merge first without an infinite stability.
	var minPos float32 = math.MaxFloat32
	for _, h := range height {
		if h > 0 && h < minPos {
			minPos = h
		}
	}
	lambdaOf := func(h float32) float32 {
		if h > 0 {
			return 1 / h
		}
		if minPos == math.MaxFloat32 {
			return 1
		}
		return 2 / minPos
	}

	// Condense: a virtual root at λ = 0 whose children are the forest's
	// trees. Clusters are numbered top-down, so a child's id exceeds its
	// parent's. A split keeps a side as its own cluster when it holds at
	// least MinClusterSize points; smaller sides fall out point by point.
	m := int32(opts.MinClusterSize)
	const root = 0
	cParent := []int32{-1}
	cBirth := []float32{0}
	cSize := []int32{int32(n)}
	pointCluster := make([]int32, n)
	pointLambda := make([]float32, n)
	fill(pointCluster, root)
	type item struct {
		node    int32
		cluster int32
	}
	var stack []item
	var leafStack []int32
	fallOut := func(sub int32, cluster int32, lam float32) {
		leafStack = append(leafStack[:0], sub)
		for len(leafStack) > 0 {
			v := leafStack[len(leafStack)-1]
			leafStack = leafStack[:len(leafStack)-1]
			if int(v) < n {
				pointCluster[v] = cluster
				pointLambda[v] = lam
				continue
			}
			t := int(v) - n
			leafStack = append(leafStack, left[t], right[t])
		}
	}
	newCluster := func(par int32, lam float32, sz int32) int32 {
		id := int32(len(cParent))
		cParent = append(cParent, par)
		cBirth = append(cBirth, lam)
		cSize = append(cSize, sz)
		return id
	}
	// The virtual root holds the forest and is never a cluster; the cluster
	// of a connected graph's single tree is the reference's root, excluded
	// unless AllowSingleCluster.
	excluded := int32(root)
	for _, rt := range roots {
		if size[rt] >= m {
			c := newCluster(root, 0, size[rt])
			if len(roots) == 1 {
				excluded = c
			}
			if int(rt) < n {
				pointCluster[rt] = c
				pointLambda[rt] = 0
			} else {
				stack = append(stack, item{rt, c})
			}
		} else {
			fallOut(rt, root, 0)
		}
	}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		t := int(it.node) - n
		l, rr := left[t], right[t]
		lam := lambdaOf(height[t])
		ls, rs := size[l], size[rr]
		descend := func(child int32, cluster int32) {
			if int(child) < n {
				pointCluster[child] = cluster
				pointLambda[child] = lam
				return
			}
			stack = append(stack, item{child, cluster})
		}
		switch {
		case ls >= m && rs >= m:
			descend(l, newCluster(it.cluster, lam, ls))
			descend(rr, newCluster(it.cluster, lam, rs))
		case ls >= m:
			fallOut(rr, it.cluster, lam)
			descend(l, it.cluster)
		case rs >= m:
			fallOut(l, it.cluster, lam)
			descend(rr, it.cluster)
		default:
			fallOut(l, it.cluster, lam)
			fallOut(rr, it.cluster, lam)
		}
	}
	// A point that a continuing cluster's leaf reached keeps that cluster
	// with the split's λ; a point whose subtree never split (a cluster that
	// is a single leaf) has λ = 0 — it is the cluster's whole mass.
	if engine.ContextDone(ctx) {
		r.Truncation = truncatedBy(LimitContext)
		return
	}

	// Stability: Σ over members (λ_leave − λ_birth), where a child
	// cluster's members leave at the child's birth.
	nc := len(cParent)
	stab := make([]float64, nc)
	for i := range n {
		c := pointCluster[i]
		stab[c] += float64(pointLambda[i] - cBirth[c])
	}
	for c := 1; c < nc; c++ {
		p := cParent[c]
		stab[p] += float64(cSize[c]) * float64(cBirth[c]-cBirth[p])
	}

	// Excess of mass, children before parents. Ids increase top-down, so a
	// descending sweep sees every child before its parent.
	selected := make([]bool, nc)
	childSum := make([]float64, nc)
	for c := nc - 1; c >= 0; c-- {
		if c == root || (int32(c) == excluded && !opts.AllowSingleCluster) {
			selected[c] = false
			if p := cParent[c]; p >= 0 {
				childSum[p] += childSum[c]
			}
			continue
		}
		if childSum[c] > stab[c] {
			stab[c] = childSum[c]
			selected[c] = false
		} else {
			selected[c] = true
		}
		if p := cParent[c]; p >= 0 {
			childSum[p] += stab[c]
		}
	}
	// A selected cluster hides its selected descendants.
	for c := 1; c < nc; c++ {
		if !selected[c] {
			continue
		}
		for p := cParent[c]; p >= 0; p = cParent[p] {
			if selected[p] {
				selected[c] = false
				break
			}
		}
	}

	// Labels in cluster-id order; a point takes its nearest selected
	// ancestor, or noise.
	labelOf := make([]int32, nc)
	fill(labelOf, -1)
	for c := range nc {
		if selected[c] {
			labelOf[c] = int32(r.NumClusters)
			r.Stability = append(r.Stability, float32(stab[c]))
			r.NumClusters++
		}
	}
	maxLambda := make([]float32, r.NumClusters)
	for i := range n {
		for c := pointCluster[i]; c >= 0; c = cParent[c] {
			if lb := labelOf[c]; lb >= 0 {
				r.Label[i] = lb
				maxLambda[lb] = max(maxLambda[lb], pointLambda[i])
				break
			}
		}
	}
	for i := range n {
		lb := r.Label[i]
		if lb < 0 || maxLambda[lb] <= 0 {
			continue
		}
		r.Probability[i] = pointLambda[i] / maxLambda[lb]
	}
	return
}

package csr

import (
	"encoding/binary"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/zeebo/xxh3"
)

// Options selects how an edge list is read into a [Graph].
type Options struct {
	// Directed keeps each arc as declared. When false every edge is stored in
	// both rows and the in- and out-adjacency coincide.
	Directed bool
}

// Graph is the compressed-sparse-row container (ADR-0226 §SD1). Slot i holds
// the i-th smallest id, so slots are ordered as ids are. Rows are sorted by
// target slot, hence by target id.
type Graph struct {
	ids  []uint64
	slot map[uint64]int32

	directed bool

	outStart []int32
	outList  []int32
	outW     []float32 // nil when the graph is unweighted

	inStart []int32
	inList  []int32
	inW     []float32

	numEdges  int64 // distinct edges after collapse; an undirected edge counts once
	selfLoops int32
	collapsed int64 // parallel edges folded into a surviving arc

	fingerprint uint64
}

// BuildE reads an edge list into a [Graph]. src and dst are parallel; w is
// either nil (unweighted) or parallel to them. Ids need no particular range
// or order. A parallel edge collapses into the arc it duplicates and its
// weight is added to the survivor's. A self-loop is kept as an arc of its
// vertex and counted in [Graph.SelfLoops].
func BuildE(src, dst []uint64, w []float32, opts Options) (g *Graph, err error) {
	if len(src) != len(dst) {
		err = eb.Build().Int("src", len(src)).Int("dst", len(dst)).Errorf("source and target columns differ in length")
		return
	}
	if w != nil && len(w) != len(src) {
		err = eb.Build().Int("weights", len(w)).Int("edges", len(src)).Errorf("weight column length differs from the edge count")
		return
	}
	m := len(src)

	// Slots in ascending id order.
	ids := make([]uint64, 0, 2*m)
	ids = append(ids, src...)
	ids = append(ids, dst...)
	radixSortUint64(ids, nil)
	ids = slices.Compact(ids)
	ids = slices.Clip(ids)
	n := len(ids)
	slot := make(map[uint64]int32, n)
	for i, id := range ids {
		slot[id] = int32(i)
	}

	// Arcs as slot pairs; an undirected graph declares both directions.
	arcs := m
	if !opts.Directed {
		arcs = 2 * m
	}
	as := make([]int32, 0, arcs)
	ad := make([]int32, 0, arcs)
	var aw []float32
	if w != nil {
		aw = make([]float32, 0, arcs)
	}
	var selfLoops int32
	for i := range m {
		s, d := slot[src[i]], slot[dst[i]]
		if s == d {
			selfLoops++
		}
		as = append(as, s)
		ad = append(ad, d)
		if w != nil {
			aw = append(aw, w[i])
		}
		if !opts.Directed && s != d {
			as = append(as, d)
			ad = append(ad, s)
			if w != nil {
				aw = append(aw, w[i])
			}
		}
	}

	outStart, outList, outW, collapsedOut := rowsE(n, as, ad, aw)
	g = &Graph{
		ids:       ids,
		slot:      slot,
		directed:  opts.Directed,
		outStart:  outStart,
		outList:   outList,
		outW:      outW,
		selfLoops: selfLoops,
	}
	if opts.Directed {
		g.inStart, g.inList, g.inW, _ = rowsE(n, ad, as, aw)
		g.numEdges = int64(len(outList))
		g.collapsed = collapsedOut
	} else {
		g.inStart, g.inList, g.inW = outStart, outList, outW
		// A non-loop edge occupies two arcs; a loop occupies one.
		g.numEdges = (int64(len(outList)) + int64(g.selfLoopsDistinct())) / 2
		g.collapsed = int64(m) - g.numEdges
	}
	g.fingerprint = g.computeFingerprint()
	return
}

// rowsE builds one CSR side by two stable counting sorts — by target, then
// by source — so each row comes out sorted by target without a comparison
// sort; a final linear pass collapses duplicate arcs, summing their weights.
// collapsed counts the arcs folded away.
func rowsE(n int, as, ad []int32, aw []float32) (start, list []int32, wts []float32, collapsed int64) {
	m := len(as)
	// Pass 1: order arcs by target.
	count := make([]int32, n+1)
	for _, d := range ad {
		count[d+1]++
	}
	for i := range n {
		count[i+1] += count[i]
	}
	byDstS := make([]int32, m)
	byDstD := make([]int32, m)
	var byDstW []float32
	if aw != nil {
		byDstW = make([]float32, m)
	}
	for i, d := range ad {
		p := count[d]
		count[d]++
		byDstS[p] = as[i]
		byDstD[p] = d
		if aw != nil {
			byDstW[p] = aw[i]
		}
	}
	// Pass 2: order by source, stably, so targets stay ascending within a row.
	start = make([]int32, n+1)
	for _, s := range byDstS {
		start[s+1]++
	}
	for i := range n {
		start[i+1] += start[i]
	}
	list = make([]int32, m)
	if aw != nil {
		wts = make([]float32, m)
	}
	fill := make([]int32, n)
	copy(fill, start[:n])
	for i, s := range byDstS {
		p := fill[s]
		fill[s]++
		list[p] = byDstD[i]
		if aw != nil {
			wts[p] = byDstW[i]
		}
	}
	// Pass 3: collapse duplicates in place, compacting rows.
	w := int32(0)
	for v := range n {
		lo, hi := start[v], start[v+1]
		start[v] = w
		for i := lo; i < hi; i++ {
			if i > lo && list[i] == list[i-1] {
				if wts != nil {
					wts[w-1] += wts[i]
				}
				collapsed++
				continue
			}
			list[w] = list[i]
			if wts != nil {
				wts[w] = wts[i]
			}
			w++
		}
	}
	start[n] = w
	list = slices.Clip(list[:w])
	if wts != nil {
		wts = slices.Clip(wts[:w])
	}
	return
}

// selfLoopsDistinct counts vertices with a self-loop arc after collapse.
func (g *Graph) selfLoopsDistinct() int32 {
	var c int32
	for v := range len(g.ids) {
		row := g.outList[g.outStart[v]:g.outStart[v+1]]
		if _, ok := slices.BinarySearch(row, int32(v)); ok {
			c++
		}
	}
	return c
}

// computeFingerprint hashes the canonical topology: directedness, ids, and
// the out-adjacency. Weights are excluded — the fingerprint names the graph
// an algorithm walked, not the values on it.
func (g *Graph) computeFingerprint() uint64 {
	h := xxh3.New()
	var buf [8]byte
	if g.directed {
		buf[0] = 1
	}
	_, _ = h.Write(buf[:1])
	const chunk = 8192
	scratch := make([]byte, 0, chunk*8)
	flush := func() {
		_, _ = h.Write(scratch)
		scratch = scratch[:0]
	}
	for _, id := range g.ids {
		scratch = binary.LittleEndian.AppendUint64(scratch, id)
		if len(scratch) == cap(scratch) {
			flush()
		}
	}
	flush()
	for _, s := range g.outStart {
		scratch = binary.LittleEndian.AppendUint32(scratch, uint32(s))
		if len(scratch) == cap(scratch) {
			flush()
		}
	}
	flush()
	for _, t := range g.outList {
		scratch = binary.LittleEndian.AppendUint32(scratch, uint32(t))
		if len(scratch) == cap(scratch) {
			flush()
		}
	}
	flush()
	return h.Sum64()
}

// NumVertices is the slot count.
func (g *Graph) NumVertices() int { return len(g.ids) }

// NumEdges counts distinct edges after collapse; an undirected edge counts
// once.
func (g *Graph) NumEdges() int64 { return g.numEdges }

// NumArcs counts adjacency entries in the out-rows: every directed arc, or
// both directions of every undirected edge.
func (g *Graph) NumArcs() int { return len(g.outList) }

// SelfLoops counts self-loop edges in the input, before collapse.
func (g *Graph) SelfLoops() int32 { return g.selfLoops }

// Collapsed counts input edges that duplicated an earlier edge.
func (g *Graph) Collapsed() int64 { return g.collapsed }

// IsDirected reports whether arcs were kept as declared.
func (g *Graph) IsDirected() bool { return g.directed }

// IsWeighted reports whether a weight column was supplied.
func (g *Graph) IsWeighted() bool { return g.outW != nil }

// Fingerprint identifies the topology: two graphs with equal fingerprints
// have the same ids, directedness and adjacency.
func (g *Graph) Fingerprint() uint64 { return g.fingerprint }

// IDs returns the id of every slot, ascending. The slice is shared; do not
// modify it.
func (g *Graph) IDs() []uint64 { return g.ids }

// ID returns the id at slot v.
func (g *Graph) ID(v int32) uint64 { return g.ids[v] }

// Slot resolves an id; ok is false when the id is not a vertex.
func (g *Graph) Slot(id uint64) (v int32, ok bool) {
	v, ok = g.slot[id]
	return
}

// Out returns the out-neighbour slots of v, ascending. Shared; do not modify.
func (g *Graph) Out(v int32) []int32 { return g.outList[g.outStart[v]:g.outStart[v+1]] }

// In returns the in-neighbour slots of v, ascending. Shared; do not modify.
func (g *Graph) In(v int32) []int32 { return g.inList[g.inStart[v]:g.inStart[v+1]] }

// OutWeights returns the weights parallel to [Graph.Out], or nil when
// unweighted.
func (g *Graph) OutWeights(v int32) []float32 {
	if g.outW == nil {
		return nil
	}
	return g.outW[g.outStart[v]:g.outStart[v+1]]
}

// InWeights returns the weights parallel to [Graph.In], or nil when
// unweighted.
func (g *Graph) InWeights(v int32) []float32 {
	if g.inW == nil {
		return nil
	}
	return g.inW[g.inStart[v]:g.inStart[v+1]]
}

// OutDegree is the out-row length of v.
func (g *Graph) OutDegree(v int32) int32 { return g.outStart[v+1] - g.outStart[v] }

// InDegree is the in-row length of v.
func (g *Graph) InDegree(v int32) int32 { return g.inStart[v+1] - g.inStart[v] }

// OutOffsets exposes the out-row offsets (length NumVertices+1) for callers
// that sweep rows without a method call per vertex. Shared; do not modify.
func (g *Graph) OutOffsets() []int32 { return g.outStart }

// OutTargets exposes the flat out-target array. Shared; do not modify.
func (g *Graph) OutTargets() []int32 { return g.outList }

// InOffsets exposes the in-row offsets. Shared; do not modify.
func (g *Graph) InOffsets() []int32 { return g.inStart }

// InTargets exposes the flat in-target array. Shared; do not modify.
func (g *Graph) InTargets() []int32 { return g.inList }

// HasArc reports whether the arc s→d is stored.
func (g *Graph) HasArc(s, d int32) bool {
	_, ok := slices.BinarySearch(g.Out(s), d)
	return ok
}

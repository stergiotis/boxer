package rutter

// bucketEntry is one backward label a target left at an ancestor.
type bucketEntry struct {
	target int32
	dist   uint32
}

// ManyToMany fills out, row-major with len(targets) columns, with the
// distance from each source to each target under w, [Inf] where none
// (ADR-0256 §SD5). Every target's backward chain leaves a bucket entry at
// each ancestor it labels; every source's forward chain scans the buckets
// on its ancestors. q is the scratch used for the chains.
func (inst *CCHQuery) ManyToMany(w *Weights, sources, targets []int32, out []uint32) {
	c := inst.cch
	n := c.g.NumNodes()
	for i := range out {
		out[i] = Inf
	}
	// Backward chains: collect (ancestor, target, distance) triples.
	var nodes []int32
	var entries []bucketEntry
	for j, t := range targets {
		inst.begin()
		inst.setB(t, 0, -1)
		for v := t; v >= 0; v = c.parent[v] {
			inst.relaxB(w, v, Inf)
			if d := inst.labelB(v); d != Inf {
				nodes = append(nodes, v)
				entries = append(entries, bucketEntry{target: int32(j), dist: d})
			}
		}
	}
	// Counting sort into per-node buckets.
	first := make([]int32, n+1)
	for _, v := range nodes {
		first[v+1]++
	}
	for v := range n {
		first[v+1] += first[v]
	}
	buckets := make([]bucketEntry, len(entries))
	fill := make([]int32, n)
	copy(fill, first[:n])
	for i, v := range nodes {
		buckets[fill[v]] = entries[i]
		fill[v]++
	}
	// Forward chains against the buckets.
	cols := len(targets)
	for i, s := range sources {
		inst.begin()
		inst.setF(s, 0, -1)
		row := out[i*cols : (i+1)*cols]
		for v := s; v >= 0; v = c.parent[v] {
			inst.relaxF(w, v, Inf)
			d := inst.labelF(v)
			if d == Inf {
				continue
			}
			for _, e := range buckets[first[v]:first[v+1]] {
				if nd := addSat(d, e.dist); nd < row[e.target] {
					row[e.target] = nd
				}
			}
		}
	}
}

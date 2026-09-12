package nav

import "slices"

// This file answers graph questions over the universe, from the adjacency
// the derivation already builds (ADR-0225 §SD1). The questions are the ones
// a consumer otherwise keeps a second adjacency to answer: who is next to
// this node, how many edges does it have, what is connected to what, and how
// do I get from here to there.
//
// Every query reads the **universe**, not the picture: hidden nodes, stubs
// and nodes no mode would show all count, because these are questions about
// the graph rather than about the view. A caller that wants the picture
// filters what comes back with Visible. Unlike FocusNodes and HiddenNodes,
// the results are the caller's to keep: a query runs on a gesture, not every
// frame, and its scratch is its own, so one may safely run inside the Style
// hook while Declare is on the stack.

// dirAllows reports whether an adjacency entry, out saying the edge leaves
// the node the entry belongs to, is walked in dir.
func dirAllows(dir DirectionE, out bool) bool {
	switch dir {
	case DirectionOut:
		return out
	case DirectionIn:
		return !out
	}
	return true
}

// Neighbours returns the distinct nodes joined to id by an edge in dir, in
// ascending id order. Parallel edges name their neighbour once; a self-loop
// names nothing, since the adjacency does not carry one. An unknown id has
// no neighbours.
func (n *Navigator) Neighbours(id uint64, dir DirectionE) []uint64 {
	s, ok := n.slot[id]
	if !ok {
		return nil
	}
	n.ensureAdjacency()
	var out []uint64
	for a := n.adjStart[s]; a < n.adjStart[s+1]; a++ {
		if !dirAllows(dir, n.adjOut[a]) {
			continue
		}
		// The adjacency is sorted by neighbour id, so the entries of one
		// neighbour are contiguous and a duplicate is the previous one.
		nid := n.ids[n.adjTo[a]]
		if len(out) > 0 && out[len(out)-1] == nid {
			continue
		}
		out = append(out, nid)
	}
	return out
}

// Degree counts the edges at id in dir. Parallel edges count once each, so
// this is the graph-theoretic degree rather than the neighbour count, and a
// self-loop counts for nothing — the adjacency leaves it out. An unknown id
// has degree 0.
func (n *Navigator) Degree(id uint64, dir DirectionE) int {
	s, ok := n.slot[id]
	if !ok {
		return 0
	}
	n.ensureAdjacency()
	if dir == DirectionBoth {
		return int(n.adjStart[s+1] - n.adjStart[s])
	}
	cnt := 0
	for a := n.adjStart[s]; a < n.adjStart[s+1]; a++ {
		if dirAllows(dir, n.adjOut[a]) {
			cnt++
		}
	}
	return cnt
}

// Components returns the connected components of the universe, ignoring
// edge direction, as ascending id slices ordered by their smallest member.
// A node with no edges is its own component, so every known id appears
// exactly once.
func (n *Navigator) Components() [][]uint64 {
	cnt := len(n.ids)
	if cnt == 0 {
		return nil
	}
	n.ensureAdjacency()
	seen := make([]bool, cnt)
	queue := make([]int32, 0, cnt)
	var comps [][]uint64
	for s := 0; s < cnt; s++ {
		if seen[s] {
			continue
		}
		seen[s] = true
		queue = append(queue[:0], int32(s))
		comp := []uint64{n.ids[s]}
		for head := 0; head < len(queue); head++ {
			cur := queue[head]
			for a := n.adjStart[cur]; a < n.adjStart[cur+1]; a++ {
				to := n.adjTo[a]
				if seen[to] {
					continue
				}
				seen[to] = true
				comp = append(comp, n.ids[to])
				queue = append(queue, to)
			}
		}
		slices.Sort(comp)
		comps = append(comps, comp)
	}
	slices.SortFunc(comps, func(a, b []uint64) int {
		switch {
		case a[0] < b[0]:
			return -1
		case a[0] > b[0]:
			return 1
		}
		return 0
	})
	return comps
}

// ShortestPath returns a path from src to dst with the fewest hops, src and
// dst included, walking edges in dir. It is nil when either id is unknown or
// no path exists, and one element long when src is dst. Ties are broken by
// the adjacency's id order, so the path is a function of the topology alone.
func (n *Navigator) ShortestPath(src, dst uint64, dir DirectionE) []uint64 {
	from, okF := n.slot[src]
	to, okT := n.slot[dst]
	if !okF || !okT {
		return nil
	}
	if from == to {
		return []uint64{src}
	}
	n.ensureAdjacency()
	prev := make([]int32, len(n.ids))
	for i := range prev {
		prev[i] = -1
	}
	prev[from] = from
	queue := make([]int32, 0, len(n.ids))
	queue = append(queue, from)
	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		for a := n.adjStart[cur]; a < n.adjStart[cur+1]; a++ {
			if !dirAllows(dir, n.adjOut[a]) {
				continue
			}
			nxt := n.adjTo[a]
			if prev[nxt] != -1 {
				continue
			}
			prev[nxt] = cur
			if nxt == to {
				return walkBack(n.ids, prev, from, to)
			}
			queue = append(queue, nxt)
		}
	}
	return nil
}

// walkBack turns a BFS predecessor array into the path from src to dst.
func walkBack(ids []uint64, prev []int32, src, dst int32) []uint64 {
	var rev []uint64
	for s := dst; ; s = prev[s] {
		rev = append(rev, ids[s])
		if s == src {
			break
		}
	}
	slices.Reverse(rev)
	return rev
}

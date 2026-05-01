//go:build llm_generated_opus47

package types

import "slices"

// UnionFind implements a disjoint-set data structure for tracking connected
// components of deleted nodes. Used for pseudo-edge resolution.
type UnionFind struct {
	parent map[NodeID]NodeID
	rank   map[NodeID]int
}

func NewUnionFind() *UnionFind {
	return &UnionFind{
		parent: make(map[NodeID]NodeID),
		rank:   make(map[NodeID]int),
	}
}

func (uf *UnionFind) Add(id NodeID) {
	if _, ok := uf.parent[id]; !ok {
		uf.parent[id] = id
		uf.rank[id] = 0
	}
}

func (uf *UnionFind) Remove(id NodeID) {
	delete(uf.parent, id)
	delete(uf.rank, id)
}

func (uf *UnionFind) Contains(id NodeID) bool {
	_, ok := uf.parent[id]
	return ok
}

// Find returns the representative of id's component, with path compression.
// If id is not in the union-find, returns id without inserting it. If a
// parent pointer leads to a node that has been removed, the chain is
// re-rooted at the last surviving ancestor — Remove leaves orphan parent
// pointers behind, and Find must heal them rather than silently writing
// the zero NodeID.
func (uf *UnionFind) Find(id NodeID) NodeID {
	if _, ok := uf.parent[id]; !ok {
		return id
	}
	for {
		p, ok := uf.parent[id]
		if !ok {
			// id's parent slot was removed mid-walk: heal by re-rooting.
			uf.parent[id] = id
			uf.rank[id] = 0
			return id
		}
		if p == id {
			return id
		}
		gp, gpOK := uf.parent[p]
		if !gpOK {
			// Parent p has been removed. Treat id as the new root of this
			// branch — the surviving ancestor is gone.
			uf.parent[id] = id
			uf.rank[id] = 0
			return id
		}
		// Path compression toward grandparent.
		uf.parent[id] = gp
		id = gp
	}
}

func (uf *UnionFind) Union(a, b NodeID) {
	ra := uf.Find(a)
	rb := uf.Find(b)
	if ra == rb {
		return
	}
	// Union by rank
	if uf.rank[ra] < uf.rank[rb] {
		ra, rb = rb, ra
	}
	uf.parent[rb] = ra
	if uf.rank[ra] == uf.rank[rb] {
		uf.rank[ra]++
	}
}

func (uf *UnionFind) SameSet(a, b NodeID) bool {
	return uf.Find(a) == uf.Find(b)
}

// Representatives returns one representative per connected component, in
// CompareNodeID order so iteration is deterministic.
func (uf *UnionFind) Representatives() []NodeID {
	reps := make(map[NodeID]struct{})
	for id := range uf.parent {
		reps[uf.Find(id)] = struct{}{}
	}
	out := make([]NodeID, 0, len(reps))
	for r := range reps {
		out = append(out, r)
	}
	slices.SortFunc(out, CompareNodeID)
	return out
}

// Members returns all members of the component containing id, in
// CompareNodeID order so iteration is deterministic.
func (uf *UnionFind) Members(id NodeID) []NodeID {
	rep := uf.Find(id)
	var out []NodeID
	for member := range uf.parent {
		if uf.Find(member) == rep {
			out = append(out, member)
		}
	}
	slices.SortFunc(out, CompareNodeID)
	return out
}
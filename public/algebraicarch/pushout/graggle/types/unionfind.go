//go:build llm_generated_opus47

package types

import "slices"

// UnionFind implements a disjoint-set data structure for tracking connected
// components of deleted nodes. Used for pseudo-edge resolution.
type UnionFind struct {
	parent map[NodeID]NodeID
	rank   map[NodeID]int32
}

func NewUnionFind() (inst *UnionFind) {
	inst = &UnionFind{
		parent: make(map[NodeID]NodeID),
		rank:   make(map[NodeID]int32),
	}
	return
}

func (inst *UnionFind) Add(id NodeID) {
	_, ok := inst.parent[id]
	if !ok {
		inst.parent[id] = id
		inst.rank[id] = 0
	}
}

func (inst *UnionFind) Remove(id NodeID) {
	delete(inst.parent, id)
	delete(inst.rank, id)
}

func (inst *UnionFind) Contains(id NodeID) (b bool) {
	_, b = inst.parent[id]
	return
}

// Find returns the representative of id's component, with path compression.
// If id is not in the union-find, returns id without inserting it. If a
// parent pointer leads to a node that has been removed, the chain is
// re-rooted at the last surviving ancestor — Remove leaves orphan parent
// pointers behind, and Find must heal them rather than silently writing
// the zero NodeID.
func (inst *UnionFind) Find(id NodeID) (rep NodeID) {
	_, ok := inst.parent[id]
	if !ok {
		rep = id
		return
	}
	for {
		p, ok := inst.parent[id]
		if !ok {
			// id's parent slot was removed mid-walk: heal by re-rooting.
			inst.parent[id] = id
			inst.rank[id] = 0
			rep = id
			return
		}
		if p == id {
			rep = id
			return
		}
		gp, gpOK := inst.parent[p]
		if !gpOK {
			// Parent p has been removed. Treat id as the new root of this
			// branch — the surviving ancestor is gone.
			inst.parent[id] = id
			inst.rank[id] = 0
			rep = id
			return
		}
		// Path compression toward grandparent.
		inst.parent[id] = gp
		id = gp
	}
}

func (inst *UnionFind) Union(a, b NodeID) {
	ra := inst.Find(a)
	rb := inst.Find(b)
	if ra == rb {
		return
	}
	// Union by rank
	if inst.rank[ra] < inst.rank[rb] {
		ra, rb = rb, ra
	}
	inst.parent[rb] = ra
	if inst.rank[ra] == inst.rank[rb] {
		inst.rank[ra]++
	}
}

func (inst *UnionFind) SameSet(a, b NodeID) (b2 bool) {
	b2 = inst.Find(a) == inst.Find(b)
	return
}

// Representatives returns one representative per connected component, in
// CompareNodeID order so iteration is deterministic.
func (inst *UnionFind) Representatives() (out []NodeID) {
	reps := make(map[NodeID]struct{})
	for id := range inst.parent {
		reps[inst.Find(id)] = struct{}{}
	}
	out = make([]NodeID, 0, len(reps))
	for r := range reps {
		out = append(out, r)
	}
	slices.SortFunc(out, CompareNodeID)
	return
}

// Members returns all members of the component containing id, in
// CompareNodeID order so iteration is deterministic.
func (inst *UnionFind) Members(id NodeID) (out []NodeID) {
	rep := inst.Find(id)
	for member := range inst.parent {
		if inst.Find(member) == rep {
			out = append(out, member)
		}
	}
	slices.SortFunc(out, CompareNodeID)
	return
}

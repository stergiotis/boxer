//go:build llm_generated_opus47

package types

import (
	"sort"
	"testing"
)

func TestUnionFind_AddAndContains(t *testing.T) {
	uf := NewUnionFind()
	a := nid("uf1", 0)
	b := nid("uf1", 1)

	if uf.Contains(a) {
		t.Fatal("should not contain unadded node")
	}

	uf.Add(a)
	if !uf.Contains(a) {
		t.Fatal("should contain added node")
	}
	if uf.Contains(b) {
		t.Fatal("should not contain node b")
	}

	// Adding twice is a no-op.
	uf.Add(a)
	if !uf.Contains(a) {
		t.Fatal("should still contain a after double add")
	}
}

func TestUnionFind_FindSelf(t *testing.T) {
	uf := NewUnionFind()
	a := nid("uf2", 0)
	uf.Add(a)
	if uf.Find(a) != a {
		t.Fatal("find on singleton should return itself")
	}
}

func TestUnionFind_UnionAndSameSet(t *testing.T) {
	uf := NewUnionFind()
	a := nid("uf3", 0)
	b := nid("uf3", 1)
	c := nid("uf3", 2)

	uf.Add(a)
	uf.Add(b)
	uf.Add(c)

	if uf.SameSet(a, b) {
		t.Fatal("a and b should not be in same set before union")
	}

	uf.Union(a, b)
	if !uf.SameSet(a, b) {
		t.Fatal("a and b should be in same set after union")
	}
	if uf.SameSet(a, c) {
		t.Fatal("a and c should not be in same set")
	}

	// Union a and c (transitively connects b and c).
	uf.Union(a, c)
	if !uf.SameSet(b, c) {
		t.Fatal("b and c should be in same set after transitive union")
	}
}

func TestUnionFind_UnionIdempotent(t *testing.T) {
	uf := NewUnionFind()
	a := nid("uf4", 0)
	b := nid("uf4", 1)
	uf.Add(a)
	uf.Add(b)

	uf.Union(a, b)
	rep1 := uf.Find(a)
	uf.Union(a, b) // same union again
	rep2 := uf.Find(a)
	if rep1 != rep2 {
		t.Fatal("repeated union should not change representative")
	}
}

func TestUnionFind_PathCompression(t *testing.T) {
	// Build a chain: a -> b -> c -> d (via sequential unions).
	uf := NewUnionFind()
	a := nid("uf5", 0)
	b := nid("uf5", 1)
	c := nid("uf5", 2)
	d := nid("uf5", 3)
	uf.Add(a)
	uf.Add(b)
	uf.Add(c)
	uf.Add(d)

	uf.Union(a, b)
	uf.Union(b, c)
	uf.Union(c, d)

	// All should share same representative.
	rep := uf.Find(a)
	for _, n := range []NodeID{b, c, d} {
		if uf.Find(n) != rep {
			t.Fatalf("node %v has different rep after chain union", n)
		}
	}
}

func TestUnionFind_Representatives(t *testing.T) {
	uf := NewUnionFind()
	a := nid("uf6", 0)
	b := nid("uf6", 1)
	c := nid("uf6", 2)
	d := nid("uf6", 3)
	uf.Add(a)
	uf.Add(b)
	uf.Add(c)
	uf.Add(d)

	uf.Union(a, b) // {a,b}, {c}, {d}
	uf.Union(c, d) // {a,b}, {c,d}

	reps := uf.Representatives()
	if len(reps) != 2 {
		t.Fatalf("expected 2 representatives, got %d", len(reps))
	}
}

func TestUnionFind_Members(t *testing.T) {
	uf := NewUnionFind()
	a := nid("uf7", 0)
	b := nid("uf7", 1)
	c := nid("uf7", 2)
	uf.Add(a)
	uf.Add(b)
	uf.Add(c)

	uf.Union(a, b)

	members := uf.Members(a)
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}

	// c is alone.
	membersC := uf.Members(c)
	if len(membersC) != 1 || membersC[0] != c {
		t.Fatalf("expected c alone, got %v", membersC)
	}
}

func TestUnionFind_Remove(t *testing.T) {
	uf := NewUnionFind()
	a := nid("uf8", 0)
	b := nid("uf8", 1)
	uf.Add(a)
	uf.Add(b)
	uf.Union(a, b)

	uf.Remove(a)
	if uf.Contains(a) {
		t.Fatal("removed node should not be contained")
	}
	// b should still be present.
	if !uf.Contains(b) {
		t.Fatal("b should still be contained after removing a")
	}
}

func TestUnionFind_RemoveRepresentative(t *testing.T) {
	uf := NewUnionFind()
	a := nid("uf9", 0)
	b := nid("uf9", 1)
	c := nid("uf9", 2)
	uf.Add(a)
	uf.Add(b)
	uf.Add(c)
	uf.Union(a, b)
	uf.Union(a, c)

	rep := uf.Find(a)
	uf.Remove(rep)

	// The remaining nodes should still be in the UF.
	remaining := 0
	for _, n := range []NodeID{a, b, c} {
		if uf.Contains(n) {
			remaining++
		}
	}
	if remaining != 2 {
		t.Fatalf("expected 2 remaining after removing rep, got %d", remaining)
	}
}

func TestUnionFind_Empty(t *testing.T) {
	uf := NewUnionFind()
	reps := uf.Representatives()
	if len(reps) != 0 {
		t.Fatal("empty UF should have no representatives")
	}
}

// Find on an absent id must not insert a phantom entry into the parent map.
func TestUnionFind_FindAbsentDoesNotPollute(t *testing.T) {
	uf := NewUnionFind()
	missing := nid("uf_missing", 0)

	rep := uf.Find(missing)
	if rep != missing {
		t.Fatalf("Find on absent id should return id, got %v", rep)
	}
	if uf.Contains(missing) {
		t.Fatal("Find on absent id must not register it in the union-find")
	}
}

// After Remove, peers that pointed at the removed node must heal rather than
// drift into the zero NodeID.
func TestUnionFind_FindAfterRemoveOfParent(t *testing.T) {
	uf := NewUnionFind()
	a := nid("uf_rm", 0)
	b := nid("uf_rm", 1)
	c := nid("uf_rm", 2)
	uf.Add(a)
	uf.Add(b)
	uf.Add(c)
	uf.Union(a, b)
	uf.Union(a, c)

	// a, b, c share a root. Remove the root.
	root := uf.Find(a)
	uf.Remove(root)

	// Find on the surviving peers must not return the zero NodeID and must
	// not pollute the parent map by inserting that zero NodeID.
	zero := NodeID{}
	for _, id := range []NodeID{a, b, c} {
		if id == root {
			continue
		}
		got := uf.Find(id)
		if got == zero && id != zero {
			t.Fatalf("Find(%v) returned zero NodeID after parent removal", id)
		}
	}
	if uf.Contains(zero) {
		t.Fatal("Find healed by writing zero NodeID into parent map")
	}
}

func TestUnionFind_MembersAfterMultipleUnions(t *testing.T) {
	uf := NewUnionFind()
	nodes := make([]NodeID, 5)
	for i := range nodes {
		nodes[i] = nid("uf10", uint64(i))
		uf.Add(nodes[i])
	}

	// Union all into one set.
	for i := 1; i < len(nodes); i++ {
		uf.Union(nodes[0], nodes[i])
	}

	members := uf.Members(nodes[0])
	sort.Slice(members, func(i, j int) bool {
		return CompareNodeID(members[i], members[j]) < 0
	})
	sort.Slice(nodes, func(i, j int) bool {
		return CompareNodeID(nodes[i], nodes[j]) < 0
	})

	if len(members) != len(nodes) {
		t.Fatalf("expected %d members, got %d", len(nodes), len(members))
	}
	for i := range members {
		if members[i] != nodes[i] {
			t.Fatalf("member mismatch at %d", i)
		}
	}
}

//go:build llm_generated_opus47

package types

import (
	"testing"
)

// --- NodeSet ---

func TestNodeSet_AddContainsRemove(t *testing.T) {
	s := NewNodeSet()
	a := nid("ns1", 0)
	b := nid("ns1", 1)

	s.Add(a)
	if !s.Contains(a) {
		t.Fatal("should contain a")
	}
	if s.Contains(b) {
		t.Fatal("should not contain b")
	}

	s.Remove(a)
	if s.Contains(a) {
		t.Fatal("should not contain a after remove")
	}
}

func TestNodeSet_RemoveNonexistent(t *testing.T) {
	s := NewNodeSet()
	a := nid("ns2", 0)
	// Should not panic.
	s.Remove(a)
}

func TestNodeSet_Len(t *testing.T) {
	s := NewNodeSet()
	if s.Len() != 0 {
		t.Fatal("empty set should have len 0")
	}
	s.Add(nid("ns3", 0))
	s.Add(nid("ns3", 1))
	if s.Len() != 2 {
		t.Fatalf("expected len 2, got %d", s.Len())
	}
	// Add duplicate.
	s.Add(nid("ns3", 0))
	if s.Len() != 2 {
		t.Fatal("duplicate add should not increase len")
	}
}

func TestNodeSet_Items_Deterministic(t *testing.T) {
	s := NewNodeSet()
	nodes := []NodeID{nid("ns4", 2), nid("ns4", 0), nid("ns4", 1)}
	for _, n := range nodes {
		s.Add(n)
	}

	items1 := s.Items()
	items2 := s.Items()
	if len(items1) != 3 || len(items2) != 3 {
		t.Fatal("should have 3 items")
	}
	for i := range items1 {
		if items1[i] != items2[i] {
			t.Fatal("Items() should be deterministic")
		}
	}
	// Verify sorted order.
	for i := 1; i < len(items1); i++ {
		if CompareNodeID(items1[i-1], items1[i]) >= 0 {
			t.Fatal("Items() should be sorted")
		}
	}
}

// --- MultiMap ---

func TestMultiMap_AddGet(t *testing.T) {
	mm := NewMultiMap()
	src := nid("mm1", 0)
	dest := nid("mm1", 1)
	e := Edge{Dest: dest, Kind: EdgeLive, IntroducedBy: ph("mm1")}

	mm.Add(src, e)
	edges := mm.Get(src)
	if len(edges) != 1 || edges[0] != e {
		t.Fatalf("expected edge, got %v", edges)
	}

	// Get on non-existent source.
	if len(mm.Get(nid("mm1", 99))) != 0 {
		t.Fatal("should return empty for nonexistent source")
	}
}

func TestMultiMap_Remove(t *testing.T) {
	mm := NewMultiMap()
	src := nid("mm2", 0)
	e1 := Edge{Dest: nid("mm2", 1), Kind: EdgeLive, IntroducedBy: ph("mm2")}
	e2 := Edge{Dest: nid("mm2", 2), Kind: EdgeDeleted, IntroducedBy: ph("mm2")}

	mm.Add(src, e1)
	mm.Add(src, e2)
	mm.Remove(src, e1)

	edges := mm.Get(src)
	if len(edges) != 1 || edges[0] != e2 {
		t.Fatalf("expected only e2, got %v", edges)
	}
}

func TestMultiMap_RemoveLastEdge(t *testing.T) {
	mm := NewMultiMap()
	src := nid("mm3", 0)
	e := Edge{Dest: nid("mm3", 1), Kind: EdgeLive, IntroducedBy: ph("mm3")}

	mm.Add(src, e)
	mm.Remove(src, e)

	// Source key should be cleaned up.
	if len(mm.Sources()) != 0 {
		t.Fatal("sources should be empty after removing last edge")
	}
}

func TestMultiMap_Has(t *testing.T) {
	mm := NewMultiMap()
	src := nid("mm4", 0)
	e := Edge{Dest: nid("mm4", 1), Kind: EdgeLive, IntroducedBy: ph("mm4")}
	eOther := Edge{Dest: nid("mm4", 1), Kind: EdgeDeleted, IntroducedBy: ph("mm4")}

	mm.Add(src, e)
	if !mm.Has(src, e) {
		t.Fatal("should have exact edge")
	}
	if mm.Has(src, eOther) {
		t.Fatal("should not have edge with different kind")
	}
}

func TestMultiMap_HasEdgeTo(t *testing.T) {
	mm := NewMultiMap()
	src := nid("mm5", 0)
	dest := nid("mm5", 1)
	e := Edge{Dest: dest, Kind: EdgeDeleted, IntroducedBy: ph("mm5")}

	mm.Add(src, e)
	if !mm.HasEdgeTo(src, dest) {
		t.Fatal("should have edge to dest regardless of kind")
	}
	if mm.HasEdgeTo(src, nid("mm5", 99)) {
		t.Fatal("should not have edge to unknown dest")
	}
}

func TestMultiMap_HasLiveEdgeTo(t *testing.T) {
	mm := NewMultiMap()
	src := nid("mm6", 0)
	dest := nid("mm6", 1)

	mm.Add(src, Edge{Dest: dest, Kind: EdgeDeleted, IntroducedBy: ph("mm6")})
	if mm.HasLiveEdgeTo(src, dest) {
		t.Fatal("deleted edge should not count as live")
	}

	mm.Add(src, Edge{Dest: dest, Kind: EdgeLive, IntroducedBy: ph("mm6")})
	if !mm.HasLiveEdgeTo(src, dest) {
		t.Fatal("should have live edge")
	}
}

func TestMultiMap_Sources(t *testing.T) {
	mm := NewMultiMap()
	s1 := nid("mm7", 0)
	s2 := nid("mm7", 1)
	mm.Add(s1, Edge{Dest: nid("mm7", 2), Kind: EdgeLive, IntroducedBy: ph("mm7")})
	mm.Add(s2, Edge{Dest: nid("mm7", 3), Kind: EdgeLive, IntroducedBy: ph("mm7")})

	sources := mm.Sources()
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
}

// --- CompareNodeID ---

func TestCompareNodeID(t *testing.T) {
	a := nid("cmp_a", 0)
	b := nid("cmp_b", 0)
	a2 := nid("cmp_a", 1)

	if CompareNodeID(a, a) != 0 {
		t.Fatal("same node should compare equal")
	}
	// Different patches.
	cmp := CompareNodeID(a, b)
	if cmp == 0 {
		t.Fatal("different patches should not compare equal")
	}
	// Same patch, different index.
	if CompareNodeID(a, a2) >= 0 {
		t.Fatal("lower index should come first")
	}
	// Reverse.
	if CompareNodeID(a2, a) <= 0 {
		t.Fatal("higher index should come after")
	}
}

// --- PatchHash ---

func TestPatchHash_String(t *testing.T) {
	h := ph("test_hash")
	s := h.String()
	if len(s) != 16 { // 8 bytes * 2 hex chars
		t.Fatalf("expected 16-char string, got %d: %q", len(s), s)
	}
}

func TestPatchHash_IsZero(t *testing.T) {
	var zero PatchHash
	if !zero.IsZero() {
		t.Fatal("zero value should be zero")
	}
	nonZero := ph("something")
	if nonZero.IsZero() {
		t.Fatal("non-zero hash should not be zero")
	}
}

func TestPatchHash_IsPlaceholder(t *testing.T) {
	if !PlaceholderHash.IsPlaceholder() {
		t.Fatal("PlaceholderHash should be placeholder")
	}
	if ph("something").IsPlaceholder() {
		t.Fatal("regular hash should not be placeholder")
	}
	var zero PatchHash
	if zero.IsPlaceholder() {
		t.Fatal("zero hash should not be placeholder")
	}
}

// --- EdgeKind ---

func TestEdgeKind_String(t *testing.T) {
	tests := []struct {
		kind EdgeKind
		want string
	}{
		{EdgeLive, "live"},
		{EdgeDeleted, "deleted"},
		{EdgePseudo, "pseudo"},
		{EdgeKind(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("EdgeKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

// --- NodeID ---

func TestNodeID_String(t *testing.T) {
	id := nid("str_test", 42)
	s := id.String()
	if s == "" {
		t.Fatal("NodeID.String() should not be empty")
	}
	// Should contain the index.
	if len(s) < 3 {
		t.Fatalf("string too short: %q", s)
	}
}

// --- HashBytes ---

func TestHashBytes_Deterministic(t *testing.T) {
	h1 := HashBytes([]byte("hello"))
	h2 := HashBytes([]byte("hello"))
	if h1 != h2 {
		t.Fatal("same input should produce same hash")
	}
	h3 := HashBytes([]byte("world"))
	if h1 == h3 {
		t.Fatal("different input should produce different hash")
	}
}
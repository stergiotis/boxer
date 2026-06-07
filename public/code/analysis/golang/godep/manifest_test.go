package godep

import (
	"slices"
	"testing"
)

// testIdx builds a small deterministic graph for the neighborhood tests:
//
//	1 ──▶ 2 ──▶ 4
//	│           ▲
//	└──▶ 3 ──▶ ─┘
//	     └──▶ 5(stdlib)
//
// All nodes are internal except 5, which is stdlib so the Include-filter tests
// have something to drop.
func testIdx() *Index {
	mk := func(id uint64, class string, imports ...uint64) PackageNode {
		return PackageNode{Id: id, Class: class, NumImports: uint32(len(imports)), Imports: imports}
	}
	m := &Manifest{Packages: []PackageNode{
		mk(1, ClassInternal, 2, 3),
		mk(2, ClassInternal, 4),
		mk(3, ClassInternal, 4, 5),
		mk(4, ClassInternal),
		mk(5, ClassStdlib),
	}}
	return m.BuildIndex()
}

// keys returns the sorted ids of a reached set, for order-independent compares.
func keys(reached map[uint64]int) []uint64 {
	out := make([]uint64, 0, len(reached))
	for id := range reached {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func eq(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNeighborhood_DepthAndDirection(t *testing.T) {
	idx := testIdx()
	cases := []struct {
		name  string
		root  uint64
		depth int
		dir   Direction
		want  []uint64
	}{
		{"imports depth1", 1, 1, DirImports, []uint64{1, 2, 3}},
		{"imports depth2", 1, 2, DirImports, []uint64{1, 2, 3, 4, 5}},
		{"depth0 is root only", 1, 0, DirImports, []uint64{1}},
		{"importers depth1", 4, 1, DirImporters, []uint64{2, 3, 4}},
		{"importers depth2", 4, 2, DirImporters, []uint64{1, 2, 3, 4}},
		{"both depth1", 2, 1, DirBoth, []uint64{1, 2, 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := keys(idx.Neighborhood(tc.root, tc.depth, tc.dir))
			if !eq(got, tc.want) {
				t.Errorf("Neighborhood(%d, %d, %v) = %v, want %v", tc.root, tc.depth, tc.dir, got, tc.want)
			}
		})
	}
}

func TestNeighborhood_HopDistances(t *testing.T) {
	idx := testIdx()
	reached := idx.Neighborhood(1, 2, DirImports)
	want := map[uint64]int{1: 0, 2: 1, 3: 1, 4: 2, 5: 2}
	for id, d := range want {
		if reached[id] != d {
			t.Errorf("hop distance for %d = %d, want %d", id, reached[id], d)
		}
	}
}

func TestBoundedNeighborhood_CapTruncates(t *testing.T) {
	idx := testIdx()
	reached, truncated := idx.BoundedNeighborhood(1, NeighborhoodOpts{
		MaxDepth: 3, Dir: DirImports, MaxNodes: 3,
	})
	if got := keys(reached); !eq(got, []uint64{1, 2, 3}) {
		t.Fatalf("capped reached = %v, want [1 2 3] (root + closest frontier)", got)
	}
	// 4 and 5 were discovered at depth 2 but dropped by the cap.
	if truncated != 2 {
		t.Errorf("truncated = %d, want 2 (nodes 4 and 5 elided)", truncated)
	}
}

func TestBoundedNeighborhood_CapZeroIsUnbounded(t *testing.T) {
	idx := testIdx()
	reached, truncated := idx.BoundedNeighborhood(1, NeighborhoodOpts{
		MaxDepth: 9, Dir: DirImports, MaxNodes: 0,
	})
	if got := keys(reached); !eq(got, []uint64{1, 2, 3, 4, 5}) {
		t.Errorf("uncapped reached = %v, want all five", got)
	}
	if truncated != 0 {
		t.Errorf("truncated = %d, want 0 when uncapped", truncated)
	}
}

func TestBoundedNeighborhood_IncludePrunesStdlib(t *testing.T) {
	idx := testIdx()
	noStd := func(p *PackageNode) bool { return p.Class != ClassStdlib }
	reached, truncated := idx.BoundedNeighborhood(1, NeighborhoodOpts{
		MaxDepth: 2, Dir: DirImports, Include: noStd,
	})
	if got := keys(reached); !eq(got, []uint64{1, 2, 3, 4}) {
		t.Errorf("filtered reached = %v, want [1 2 3 4] (5 is stdlib)", got)
	}
	// A filtered node is not a truncation — it was deliberately excluded.
	if truncated != 0 {
		t.Errorf("truncated = %d, want 0 (filtering is not capping)", truncated)
	}
}

// The root is always admitted, even when Include would reject it — otherwise
// focusing a stdlib package with "hide stdlib" on would show an empty graph.
func TestBoundedNeighborhood_RootAlwaysAdmitted(t *testing.T) {
	idx := testIdx()
	noStd := func(p *PackageNode) bool { return p.Class != ClassStdlib }
	reached, _ := idx.BoundedNeighborhood(5, NeighborhoodOpts{
		MaxDepth: 1, Dir: DirImporters, Include: noStd,
	})
	if _, ok := reached[5]; !ok {
		t.Fatal("root 5 (stdlib) missing from its own filtered neighborhood")
	}
	if got := keys(reached); !eq(got, []uint64{3, 5}) {
		t.Errorf("reached = %v, want [3 5] (root + its internal importer)", got)
	}
}

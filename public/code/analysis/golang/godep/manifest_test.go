package godep

import "testing"

// testIdx builds a small deterministic graph:
//
//	1 ──▶ 2 ──▶ 4
//	│           ▲
//	└──▶ 3 ──▶ ─┘
//	     └──▶ 5(stdlib)
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

// The index is an id→node lookup over the manifest's own backing array, so a
// looked-up node is the stored one and not a copy — wasmsurvey's classifier
// reads Class and Imports straight off it.
func TestBuildIndexResolvesNodes(t *testing.T) {
	idx := testIdx()

	p, ok := idx.Node(3)
	if !ok {
		t.Fatalf("Node(3): not found")
	}
	if p.Class != ClassInternal {
		t.Errorf("Node(3).Class: got %q want %q", p.Class, ClassInternal)
	}
	if len(p.Imports) != 2 || p.Imports[0] != 4 || p.Imports[1] != 5 {
		t.Errorf("Node(3).Imports: got %v want [4 5]", p.Imports)
	}

	if p, ok := idx.Node(5); !ok || p.Class != ClassStdlib {
		t.Errorf("Node(5): got (%v, %v) want a stdlib node", p, ok)
	}
	if _, ok := idx.Node(999); ok {
		t.Errorf("Node(999): reported present")
	}
}

// An empty manifest yields a usable, empty index rather than a nil map.
func TestBuildIndexEmptyManifest(t *testing.T) {
	idx := (&Manifest{}).BuildIndex()
	if _, ok := idx.Node(1); ok {
		t.Errorf("Node on an empty index: reported present")
	}
}

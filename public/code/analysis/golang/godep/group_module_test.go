package godep_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
)

// testRoot is the root module of the synthetic graph below; first-party import
// paths are under it.
const testRoot = "example.com/m"

// Package ids for the synthetic graph. Small distinct integers stand in for the
// real FNV-1a-64(ImportPath) ids — the Index keys on whatever Id a node carries.
const (
	idAlpha    = 1  // apps/alpha            (first-party app)
	idAlphaSub = 3  // apps/alpha/sub        (same app, sub-package)
	idBeta     = 2  // apps/beta             (first-party app)
	idLib      = 10 // public/lib/util       (first-party library)
	idFoo      = 20 // github.com/foo/pkg    (external, module github.com/foo)
	idGrpc     = 21 // github.com/grpc/pkg   (external, module github.com/grpc)
	idFmt      = 30 // fmt                   (stdlib)
)

// testManifest builds a small but representative graph:
//
//	apps/alpha ─▶ apps/beta        (the keelson-rule VIOLATION)
//	apps/alpha ─▶ public/lib
//	apps/alpha/sub ─▶ public/lib   (folds into apps/alpha; weights the edge to 2)
//	apps/beta  ─▶ public/lib
//	public/lib ─▶ github.com/foo, fmt
//	github.com/foo ─▶ github.com/grpc   (grpc is pulled in only transitively)
func testManifest() (m godep.Manifest) {
	pkg := func(id uint64, path, mod, class string, imports ...uint64) godep.PackageNode {
		return godep.PackageNode{
			Id:         id,
			ImportPath: path,
			ModulePath: mod,
			Class:      class,
			NumImports: uint32(len(imports)),
			Imports:    imports,
		}
	}
	m.Run = godep.CollectionRun{RootModulePath: testRoot}
	m.Packages = []godep.PackageNode{
		pkg(idAlpha, testRoot+"/apps/alpha", testRoot, godep.ClassInternal, idBeta, idLib),
		pkg(idAlphaSub, testRoot+"/apps/alpha/sub", testRoot, godep.ClassInternal, idLib),
		pkg(idBeta, testRoot+"/apps/beta", testRoot, godep.ClassInternal, idLib),
		pkg(idLib, testRoot+"/public/lib/util", testRoot, godep.ClassInternal, idFoo, idFmt),
		pkg(idFoo, "github.com/foo/pkg", "github.com/foo", godep.ClassExternal, idGrpc),
		pkg(idGrpc, "github.com/grpc/pkg", "github.com/grpc", godep.ClassExternal),
		pkg(idFmt, "fmt", "std", godep.ClassStdlib),
	}
	return m
}

func TestGroupOf(t *testing.T) {
	m := testManifest()
	byPath := make(map[string]*godep.PackageNode)
	for i := range m.Packages {
		byPath[m.Packages[i].ImportPath] = &m.Packages[i]
	}
	cases := []struct {
		path  string
		depth int
		want  godep.GroupKey
	}{
		{testRoot + "/apps/alpha", 2, "apps/alpha"},
		{testRoot + "/apps/alpha/sub", 2, "apps/alpha"}, // sub-package folds into the app
		{testRoot + "/apps/beta", 2, "apps/beta"},
		{testRoot + "/public/lib/util", 2, "public/lib"}, // depth 2 truncates the path
		{testRoot + "/public/lib/util", 3, "public/lib/util"},
		{testRoot + "/public/lib/util", 1, "public"},
		{"github.com/foo/pkg", 2, "github.com/foo"}, // external groups by module, ignoring depth
		{"github.com/grpc/pkg", 3, "github.com/grpc"},
		{"fmt", 2, godep.StdlibGroup},
	}
	for _, tc := range cases {
		p, ok := byPath[tc.path]
		if !ok {
			t.Fatalf("test bug: no package %q", tc.path)
		}
		if got := godep.GroupOf(p, testRoot, godep.GroupingOpts{InternalDepth: tc.depth}); got != tc.want {
			t.Errorf("GroupOf(%q, depth=%d) = %q, want %q", tc.path, tc.depth, got, tc.want)
		}
	}
}

func TestBuildGroupGraph(t *testing.T) {
	m := testManifest()
	idx := m.BuildIndex()
	gg := idx.BuildGroupGraph(testRoot, godep.GroupingOpts{InternalDepth: 2}, nil)

	wantNodes := map[godep.GroupKey]string{
		"apps/alpha":      godep.ClassInternal,
		"apps/beta":       godep.ClassInternal,
		"public/lib":      godep.ClassInternal,
		"github.com/foo":  godep.ClassExternal,
		"github.com/grpc": godep.ClassExternal,
		godep.StdlibGroup: godep.ClassStdlib,
	}
	if len(gg.Nodes) != len(wantNodes) {
		t.Fatalf("group node count = %d, want %d (%v)", len(gg.Nodes), len(wantNodes), gg.Nodes)
	}
	for key, wantClass := range wantNodes {
		n, ok := gg.Node(key)
		if !ok {
			t.Errorf("missing group node %q", key)
			continue
		}
		if n.Class != wantClass {
			t.Errorf("group %q class = %q, want %q", key, n.Class, wantClass)
		}
	}
	// apps/alpha folds in apps/alpha/sub, so it has two member packages.
	if n, _ := gg.Node("apps/alpha"); n.NumPkgs != 2 {
		t.Errorf("apps/alpha NumPkgs = %d, want 2", n.NumPkgs)
	}

	edgeWeight := func(from, to godep.GroupKey) int {
		for _, e := range gg.Edges {
			if e.From == from && e.To == to {
				return e.Weight
			}
		}
		return -1
	}
	// apps/alpha -> public/lib carries both alpha and alpha/sub: weight 2.
	if w := edgeWeight("apps/alpha", "public/lib"); w != 2 {
		t.Errorf("edge apps/alpha->public/lib weight = %d, want 2", w)
	}
	if w := edgeWeight("apps/alpha", "apps/beta"); w != 1 {
		t.Errorf("edge apps/alpha->apps/beta weight = %d, want 1", w)
	}
	if w := edgeWeight("public/lib", "github.com/foo"); w != 1 {
		t.Errorf("edge public/lib->github.com/foo weight = %d, want 1", w)
	}
	// Intra-group edges are never emitted (apps/alpha/sub -> nothing in-group here,
	// but guard the invariant anyway).
	for _, e := range gg.Edges {
		if e.From == e.To {
			t.Errorf("self group-edge emitted: %q", e.From)
		}
	}
}

func TestBuildGroupGraphIncludeFilter(t *testing.T) {
	m := testManifest()
	idx := m.BuildIndex()
	// Drop stdlib and external: an internal-only architecture view.
	gg := idx.BuildGroupGraph(testRoot, godep.GroupingOpts{InternalDepth: 2}, func(p *godep.PackageNode) bool {
		return p.Class == godep.ClassInternal
	})
	for _, n := range gg.Nodes {
		if n.Class != godep.ClassInternal {
			t.Errorf("include filter leaked a %q node %q", n.Class, n.Key)
		}
	}
	// The public/lib -> github.com/foo edge must vanish with foo filtered out.
	for _, e := range gg.Edges {
		if e.To == "github.com/foo" || e.From == godep.StdlibGroup || e.To == godep.StdlibGroup {
			t.Errorf("edge touching a filtered node survived: %v", e)
		}
	}
}

func TestSiblingViolations(t *testing.T) {
	m := testManifest()
	idx := m.BuildIndex()
	v := idx.SiblingViolations(testRoot, "apps/")
	if len(v) != 1 {
		t.Fatalf("violations = %d, want 1: %+v", len(v), v)
	}
	got := v[0]
	if got.FromGroup != "apps/alpha" || got.ToGroup != "apps/beta" {
		t.Errorf("violation groups = %q->%q, want apps/alpha->apps/beta", got.FromGroup, got.ToGroup)
	}
	if got.FromPkg != idAlpha || got.ToPkg != idBeta {
		t.Errorf("violation pkgs = %d->%d, want %d->%d", got.FromPkg, got.ToPkg, idAlpha, idBeta)
	}
	// A non-matching prefix yields nothing.
	if v := idx.SiblingViolations(testRoot, "public/"); len(v) != 0 {
		t.Errorf("public/ siblings: got %d violations, want 0", len(v))
	}
}

func TestModuleStats(t *testing.T) {
	m := testManifest()
	idx := m.BuildIndex()
	stats := idx.ModuleStats(testRoot)

	byMod := make(map[string]godep.ModuleStat)
	for _, s := range stats {
		byMod[s.ModulePath] = s
	}
	if len(byMod) != 2 {
		t.Fatalf("module count = %d, want 2 (foo, grpc): %+v", len(byMod), stats)
	}

	foo := byMod["github.com/foo"]
	if !foo.Direct {
		t.Errorf("github.com/foo Direct = false, want true (public/lib imports it)")
	}
	if foo.FanIn != 1 {
		t.Errorf("github.com/foo FanIn = %d, want 1", foo.FanIn)
	}
	if foo.BlastRadius != 4 {
		t.Errorf("github.com/foo BlastRadius = %d, want 4 (lib, alpha, alpha/sub, beta)", foo.BlastRadius)
	}

	grpc := byMod["github.com/grpc"]
	if grpc.Direct {
		t.Errorf("github.com/grpc Direct = true, want false (only foo imports it)")
	}
	if grpc.FanIn != 0 {
		t.Errorf("github.com/grpc FanIn = %d, want 0", grpc.FanIn)
	}
	// Transitive, yet a change still reaches the same first-party set through foo.
	if grpc.BlastRadius != 4 {
		t.Errorf("github.com/grpc BlastRadius = %d, want 4", grpc.BlastRadius)
	}
}

func TestShortestImportPathTo(t *testing.T) {
	m := testManifest()
	idx := m.BuildIndex()

	// Why does apps/alpha depend on the (transitive) grpc module? The witness is
	// alpha → public/lib/util → foo/pkg → grpc/pkg.
	got := idx.ShortestImportPathTo(idAlpha, func(id uint64) bool { return id == idGrpc })
	want := []uint64{idAlpha, idLib, idFoo, idGrpc}
	if len(got) != len(want) {
		t.Fatalf("path = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("path = %v, want %v", got, want)
		}
	}

	// A nearer target stops the walk earlier (foo is one hop closer than grpc).
	if p := idx.ShortestImportPathTo(idAlpha, func(id uint64) bool { return id == idFoo }); len(p) != 3 || p[2] != idFoo {
		t.Errorf("path to foo = %v, want [alpha lib foo]", p)
	}
	// from satisfies the target immediately → [from].
	if p := idx.ShortestImportPathTo(idAlpha, func(id uint64) bool { return id == idAlpha }); len(p) != 1 || p[0] != idAlpha {
		t.Errorf("path to self = %v, want [alpha]", p)
	}
	// Unreachable (grpc imports nothing first-party).
	if p := idx.ShortestImportPathTo(idGrpc, func(id uint64) bool { return id == idAlpha }); p != nil {
		t.Errorf("unreachable path = %v, want nil", p)
	}
}

func TestStronglyConnected(t *testing.T) {
	// The standard manifest's quotient is acyclic.
	base := testManifest()
	gg := base.BuildIndex().BuildGroupGraph(testRoot, godep.GroupingOpts{InternalDepth: 2}, nil)
	if comps := gg.StronglyConnected(); len(comps) != 0 {
		t.Errorf("acyclic quotient: got %d SCCs, want 0: %v", len(comps), comps)
	}

	// A cyclic quotient: public/a ⇄ public/b (group-level cycle through distinct
	// packages, even though package imports stay acyclic).
	pkg := func(id uint64, path string, imports ...uint64) godep.PackageNode {
		return godep.PackageNode{Id: id, ImportPath: path, ModulePath: testRoot, Class: godep.ClassInternal, Imports: imports}
	}
	var m godep.Manifest
	m.Run = godep.CollectionRun{RootModulePath: testRoot}
	m.Packages = []godep.PackageNode{
		pkg(100, testRoot+"/public/a/x", 200), // public/a → public/b
		pkg(200, testRoot+"/public/b/y", 101), // public/b → public/a (a different a package)
		pkg(101, testRoot+"/public/a/z"),      // leaf
	}
	gg2 := m.BuildIndex().BuildGroupGraph(testRoot, godep.GroupingOpts{InternalDepth: 2}, nil)
	comps := gg2.StronglyConnected()
	if len(comps) != 1 {
		t.Fatalf("cyclic quotient: got %d SCCs, want 1: %v", len(comps), comps)
	}
	if len(comps[0]) != 2 || comps[0][0] != "public/a" || comps[0][1] != "public/b" {
		t.Errorf("SCC = %v, want [public/a public/b]", comps[0])
	}
}

func TestModuleImporters(t *testing.T) {
	m := testManifest()
	idx := m.BuildIndex()
	imps := idx.ModuleImporters([]uint64{idFoo})
	if len(imps) != 1 || imps[0] != idLib {
		t.Errorf("ModuleImporters(foo) = %v, want [%d]", imps, idLib)
	}
	// grpc has no direct first-party importer.
	if imps := idx.ModuleImporters([]uint64{idGrpc}); len(imps) != 0 {
		t.Errorf("ModuleImporters(grpc) = %v, want []", imps)
	}
}

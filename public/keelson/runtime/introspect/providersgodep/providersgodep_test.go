package providersgodep

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// fixture is a two-package manifest: a imports b, b imports nothing, plus one
// edge whose target is absent from the package set (the anomaly buildEdges
// keeps rather than drops).
func fixture() (m godep.Manifest) {
	const missing uint64 = 999
	m = godep.Manifest{
		Run: godep.CollectionRun{
			RootModulePath: "github.com/example/mod",
			GoVersion:      "go1.25",
			Scope:          godep.ScopeTransitive,
			NumPackages:    2,
			NumEdges:       2,
			BuildTags:      []string{"tag_a", "tag_b"},
			Roots:          []string{"./..."},
			Ts:             time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC).UnixNano(),
		},
		Packages: []godep.PackageNode{
			{
				Id: 1, ImportPath: "github.com/example/mod/a", Name: "a",
				Dir: "/src/a", ModulePath: "github.com/example/mod", Class: godep.ClassInternal,
				NumGoFiles: 3, NumImports: 2, NumImportedBy: 0,
				Imports: []uint64{2, missing},
			},
			{
				Id: 2, ImportPath: "github.com/example/mod/b", Name: "b",
				Dir: "/src/b", ModulePath: "github.com/example/mod", Class: godep.ClassInternal,
				NumGoFiles: 1, NumImports: 0, NumImportedBy: 1,
			},
		},
	}
	return
}

// readyCache returns a cache already holding the fixture, so table tests do
// not touch the collector.
func readyCache() (inst *cache) {
	inst = &cache{cfg: Config{Root: "/src"}, budget: time.Second}
	man := fixture()
	inst.snap = &snapshot{
		status: statusReady, man: man, edges: buildEdges(man),
		finished: time.Now(), duration: 1400 * time.Millisecond,
	}
	return
}

func TestPackagesTableRendersNodes(t *testing.T) {
	rec := packagesTable(readyCache().snap, nil).Build(introspect.AllColumns(), 2)
	defer rec.Release()
	require.EqualValues(t, 2, rec.NumRows())
	assert.Equal(t, "github.com/example/mod/a", stringAt(t, rec, "import_path", 0))
	assert.Equal(t, "internal", stringAt(t, rec, "class", 0))
	assert.Equal(t, "/src/b", stringAt(t, rec, "dir", 1))
	assert.EqualValues(t, 1, uint64At(t, rec, "id", 0))
	assert.EqualValues(t, 3, int64At(t, rec, "num_go_files", 0))
	assert.EqualValues(t, 2, int64At(t, rec, "num_imports", 0))
	assert.EqualValues(t, 1, int64At(t, rec, "num_imported_by", 1))
}

// The edge table is the query shape ADR-0064 §SD2's embedded adjacency is
// not: one row per relation, both endpoints named.
func TestImportsTableFlattensAdjacency(t *testing.T) {
	rec := importsTable(readyCache().snap).Build(introspect.AllColumns(), 2)
	defer rec.Release()
	require.EqualValues(t, 2, rec.NumRows())
	assert.Equal(t, "github.com/example/mod/a", stringAt(t, rec, "src_path", 0))
	assert.Equal(t, "github.com/example/mod/b", stringAt(t, rec, "dst_path", 0))
	assert.EqualValues(t, 2, uint64At(t, rec, "dst_id", 0))
	// The unresolved target keeps its edge and reports an empty path, so a
	// query can find it; dropping it would hide the anomaly.
	assert.EqualValues(t, 999, uint64At(t, rec, "dst_id", 1))
	assert.Equal(t, "", stringAt(t, rec, "dst_path", 1))
}

func TestCollectionTableReportsReadyRun(t *testing.T) {
	c := readyCache()
	rec := collectionTable(c.snap, c.cfg).Build(introspect.AllColumns(), 1)
	defer rec.Release()
	require.EqualValues(t, 1, rec.NumRows())
	assert.Equal(t, statusReady, stringAt(t, rec, "status", 0))
	assert.Equal(t, "", stringAt(t, rec, "error", 0))
	assert.Equal(t, "/src", stringAt(t, rec, "root_dir", 0))
	assert.Equal(t, "github.com/example/mod", stringAt(t, rec, "root_module", 0))
	assert.Equal(t, "2026-08-01T10:00:00Z", stringAt(t, rec, "collected_at", 0))
	assert.EqualValues(t, 1400, int64At(t, rec, "duration_ms", 0))
	assert.Equal(t, []string{"tag_a", "tag_b"}, stringListAt(t, rec, "build_tags", 0))
}

// A collection that has not produced rows still yields exactly one
// go_collection row — the table a reader consults when the others are empty.
func TestCollectionTableAlwaysHasOneRow(t *testing.T) {
	for _, s := range []*snapshot{
		{status: statusCollecting},
		{status: statusFailed, errText: "no go.mod above /tmp"},
	} {
		rec := collectionTable(s, Config{}).Build(introspect.AllColumns(), 1)
		require.EqualValues(t, 1, rec.NumRows())
		assert.Equal(t, s.status, stringAt(t, rec, "status", 0))
		// Never a 1970 timestamp for a run that has not happened.
		assert.Equal(t, "", stringAt(t, rec, "collected_at", 0))
		rec.Release()
	}
}

// Collection past the wait budget answers with zero rows rather than
// blocking the query, and the eventual result replaces it.
func TestCacheDegradesToCollectingPastBudget(t *testing.T) {
	release := make(chan struct{})
	c := &cache{budget: 10 * time.Millisecond}
	c.load = func(context.Context) (godep.Manifest, error) {
		<-release
		return fixture(), nil
	}

	s := c.get()
	assert.Equal(t, statusCollecting, s.status)
	assert.Empty(t, s.man.Packages, "no rows while collecting")

	close(release)
	require.Eventually(t, func() bool { return c.get().status == statusReady }, 2*time.Second, 5*time.Millisecond)
	assert.Len(t, c.get().man.Packages, 2)
}

// A failed collection is sticky: the tables stay empty and the reason stays
// readable, without re-running the toolchain once per query.
func TestCacheFailureIsStickyAndCollectsOnce(t *testing.T) {
	calls := 0
	c := &cache{budget: time.Second}
	c.load = func(context.Context) (godep.Manifest, error) {
		calls++
		return godep.Manifest{}, eh.Errorf("toolchain unavailable")
	}
	for range 3 {
		s := c.get()
		assert.Equal(t, statusFailed, s.status)
		assert.Contains(t, s.errText, "toolchain unavailable")
	}
	assert.Equal(t, 1, calls, "collection runs at most once per process")
}

// Off-repo, `go list ./...` reports a synthetic package for the unresolved
// pattern rather than failing, so a lone junk row would otherwise reach the
// tables. No main module ⇒ failed collection with a pointer to the fix.
func TestCacheRejectsACollectionWithNoMainModule(t *testing.T) {
	c := &cache{cfg: Config{Root: "/nowhere"}, budget: time.Second}
	c.load = func(context.Context) (godep.Manifest, error) {
		return godep.Manifest{
			Run:      godep.CollectionRun{RootModulePath: ""},
			Packages: []godep.PackageNode{{Id: 1, ImportPath: "./...", Class: godep.ClassStdlib}},
		}, nil
	}
	s := c.get()
	assert.Equal(t, statusFailed, s.status)
	assert.Contains(t, s.errText, "BOXER_GODEP_ROOT")
	assert.Empty(t, s.man.Packages, "the synthetic row never reaches the tables")
}

func TestRegisterAddsTheFourTables(t *testing.T) {
	r := introspect.NewRegistry()
	require.NoError(t, Register(r, Config{Root: t.TempDir()}))
	var names []string
	for _, p := range r.Providers() {
		names = append(names, p.Name())
	}
	assert.ElementsMatch(t,
		[]string{"go_packages", "go_imports", "go_collection", "go_package_props"}, names)
}

// The props table is compiled in, so it answers without any collection.
func TestPackagePropsTableIsStatic(t *testing.T) {
	p := packagePropsProvider{}
	assert.Equal(t, introspect.FreshnessStatic, p.Freshness())
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	assert.Positive(t, rec.NumRows(), "the generated whole-repo table is not empty")
	for _, col := range []string{"import_path", "wasm_wasi", "wasm_js", "wasm_freestanding", "wasm_compiles", "kind"} {
		assert.Len(t, rec.Schema().FieldIndices(col), 1, "column %q", col)
	}
}

func column(t *testing.T, rec arrow.RecordBatch, name string) arrow.Array {
	t.Helper()
	idx := rec.Schema().FieldIndices(name)
	require.Len(t, idx, 1, "column %q", name)
	return rec.Column(idx[0])
}

func stringAt(t *testing.T, rec arrow.RecordBatch, name string, row int) string {
	t.Helper()
	return column(t, rec, name).(*array.String).Value(row)
}

func uint64At(t *testing.T, rec arrow.RecordBatch, name string, row int) uint64 {
	t.Helper()
	return column(t, rec, name).(*array.Uint64).Value(row)
}

func int64At(t *testing.T, rec arrow.RecordBatch, name string, row int) int64 {
	t.Helper()
	return column(t, rec, name).(*array.Int64).Value(row)
}

func stringListAt(t *testing.T, rec arrow.RecordBatch, name string, row int) (out []string) {
	t.Helper()
	list := column(t, rec, name).(*array.List)
	values := list.ListValues().(*array.String)
	start, end := list.ValueOffsets(row)
	for i := start; i < end; i++ {
		out = append(out, values.Value(int(i)))
	}
	return
}

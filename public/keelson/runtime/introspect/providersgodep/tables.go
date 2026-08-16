package providersgodep

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/code/analysis/golang/codevol"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/packageprops"
	"github.com/stergiotis/boxer/public/packageprops/proptable"
)

// go_packages — one row per package in the transitive closure. These are
// ADR-0064's PackageNode fields as flat columns; the adjacency it embeds
// lives in go_imports instead.
type packagesProvider struct{ cache *cache }

func (packagesProvider) Name() string                         { return "go_packages" }
func (packagesProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (packagesProvider) Schema() *arrow.Schema                { return packagesTable(nil, nil).Schema() }

func (inst packagesProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	s := inst.cache.get()
	return packagesTable(s, inst.cache.vol).Build(proj, len(s.man.Packages)), nil
}

func packagesTable(s *snapshot, vol *volumeCache) *introspect.Table {
	var rows []godepPackageRow
	if s != nil {
		rows = packageRows(s)
	}
	// The volume columns (ADR-0173 §SD3) are read through a separate cache
	// that populates on first access. Introspect skips the getters of
	// unprojected columns, so a query that does not select a volume column
	// never triggers the ~2 s counting pass.
	v := func(i int) codevol.Volume { return vol.get(rows[i].importPath) }
	return introspect.NewTable().
		// id is FNV-1a-64 of the import path (ADR-0064 §SD3): stable across
		// runs, and the join key go_imports carries.
		Uint64("id", func(i int) uint64 { return rows[i].id }).
		String("import_path", func(i int) string { return rows[i].importPath }).
		String("name", func(i int) string { return rows[i].name }).
		// dir is empty for packages whose files go/packages did not report.
		String("dir", func(i int) string { return rows[i].dir }).
		// module_path is "std" for the standard library.
		String("module_path", func(i int) string { return rows[i].modulePath }).
		// class is stdlib | internal | external, relative to the root module.
		String("class", func(i int) string { return rows[i].class }).
		Int64("num_go_files", func(i int) int64 { return rows[i].numGoFiles }).
		// Out- and in-degree, denormalised at collection (ADR-0064 §SD4) so a
		// fan-in/fan-out ranking needs no traversal. Equal by construction to
		// the matching go_imports counts.
		Int64("num_imports", func(i int) int64 { return rows[i].numImports }).
		Int64("num_imported_by", func(i int) int64 { return rows[i].numImportedBy }).
		// Line volume (ADR-0173 §SD3), classified with go/scanner so that a
		// "//" inside a string literal is not counted as a comment. Zero for
		// every package when the counting pass could not run.
		Int64("code_lines", func(i int) int64 { return int64(v(i).CodeLines) }).
		Int64("comment_lines", func(i int) int64 { return int64(v(i).CommentLines) }).
		Int64("blank_lines", func(i int) int64 { return int64(v(i).BlankLines) }).
		// generated_files/generated_code separate machine-written source from
		// what anyone typed — 40% of this module's own compiled lines are
		// generated, and a total that hides it overstates authorship.
		Int64("generated_files", func(i int) int64 { return int64(v(i).GeneratedFiles) }).
		Int64("generated_code", func(i int) int64 { return int64(v(i).GeneratedCode) }).
		// Which tools wrote them (ADR-0173 §SD10), read off the same marker
		// line generated_files is counted from, so it costs nothing extra. A
		// list because a quarter of this repository's generated packages
		// carry more than one tool; group with arrayJoin. Empty for a package
		// with no generated file, and for every package when the pass could
		// not run — the same empty-not-absent shape as the counts above.
		StringList("generators", func(i int) []string { return v(i).Generators }).
		// C, C++, assembly and headers compiled with a cgo package — invisible
		// to any Go-only count.
		Int64("other_lang_lines", func(i int) int64 { return int64(v(i).OtherLangLines) })
}

// godepPackageRow flattens a PackageNode to the column types the table
// builder offers (counts are Int64 there, uint32 on the DTO).
type godepPackageRow struct {
	id            uint64
	importPath    string
	name          string
	dir           string
	modulePath    string
	class         string
	numGoFiles    int64
	numImports    int64
	numImportedBy int64
}

func packageRows(s *snapshot) (out []godepPackageRow) {
	out = make([]godepPackageRow, len(s.man.Packages))
	for i := range s.man.Packages {
		p := &s.man.Packages[i]
		out[i] = godepPackageRow{
			id: p.Id, importPath: p.ImportPath, name: p.Name, dir: p.Dir,
			modulePath: p.ModulePath, class: p.Class,
			numGoFiles: int64(p.NumGoFiles), numImports: int64(p.NumImports),
			numImportedBy: int64(p.NumImportedBy),
		}
	}
	return
}

// go_imports — one row per import relation, both endpoints denormalised.
type importsProvider struct{ cache *cache }

func (importsProvider) Name() string                         { return "go_imports" }
func (importsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (importsProvider) Schema() *arrow.Schema                { return importsTable(nil).Schema() }

func (inst importsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	s := inst.cache.get()
	return importsTable(s).Build(proj, len(s.edges)), nil
}

func importsTable(s *snapshot) *introspect.Table {
	var rows []edge
	if s != nil {
		rows = s.edges
	}
	return introspect.NewTable().
		Uint64("src_id", func(i int) uint64 { return rows[i].srcID }).
		Uint64("dst_id", func(i int) uint64 { return rows[i].dstID }).
		String("src_path", func(i int) string { return rows[i].srcPath }).
		// Empty when the target is absent from go_packages — not expected,
		// and left visible rather than dropped (see buildEdges).
		String("dst_path", func(i int) string { return rows[i].dstPath })
}

// go_collection — always exactly one row: what was collected, when, how long
// it took, and whether it is there yet. It is the table to read first when
// the other three are empty.
type collectionProvider struct{ cache *cache }

func (collectionProvider) Name() string                         { return "go_collection" }
func (collectionProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (collectionProvider) Schema() *arrow.Schema                { return collectionTable(nil, Config{}).Schema() }

func (inst collectionProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	s := inst.cache.get()
	return collectionTable(s, inst.cache.cfg).Build(proj, 1), nil
}

func collectionTable(s *snapshot, cfg Config) *introspect.Table {
	if s == nil {
		s = &snapshot{}
	}
	run := s.man.Run
	// The manifest's own collection timestamp when there is one; otherwise
	// blank rather than a zero-time string, so "not collected yet" reads as
	// absent instead of as 1970. Rendered UTC, so the column does not vary
	// with the host's timezone.
	collectedAt := ""
	if s.status == statusReady && run.Ts != 0 {
		collectedAt = time.Unix(0, run.Ts).UTC().Format(time.RFC3339)
	}
	return introspect.NewTable().
		// collecting | ready | failed. Collection runs once per process: a
		// failure is sticky, so a `failed` row stays until restart.
		String("status", func(int) string { return s.status }).
		String("error", func(int) string { return s.errText }).
		// The resolved module directory — the answer to "which module am I
		// even looking at", which depends on where the process was started.
		String("root_dir", func(int) string { return cfg.Root }).
		String("root_module", func(int) string { return run.RootModulePath }).
		String("go_version", func(int) string { return run.GoVersion }).
		String("scope", func(int) string { return run.Scope }).
		// The tags collection ran under. This repo's tags are load-bearing:
		// an empty list usually means a graph missing tag-gated packages.
		StringList("build_tags", func(int) []string { return run.BuildTags }).
		StringList("roots", func(int) []string { return run.Roots }).
		Int64("num_packages", func(int) int64 { return int64(run.NumPackages) }).
		Int64("num_edges", func(int) int64 { return int64(run.NumEdges) }).
		String("collected_at", func(int) string { return collectedAt }).
		Int64("duration_ms", func(int) int64 { return s.duration.Milliseconds() })
}

// go_package_props — the ADR-0080 declarations, from the generated whole-repo
// table. Static: it is compiled in, needs no collection, and is the one godep
// table that answers for packages `go list` may not reach under the active
// tags. Only surveyed first-party packages appear, so a LEFT JOIN from
// go_packages is the right shape.
type packagePropsProvider struct{}

func (packagePropsProvider) Name() string                         { return "go_package_props" }
func (packagePropsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (packagePropsProvider) Schema() *arrow.Schema                { return packagePropsTable().Schema() }

func (packagePropsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	return packagePropsTable().Build(proj, len(proptable.Table)), nil
}

func packagePropsTable() *introspect.Table {
	rows := proptable.Table
	return introspect.NewTable().
		String("import_path", func(i int) string { return rows[i].ImportPath }).
		// unknown | compiles | blocked, per TinyGo target.
		String("wasm_wasi", func(i int) string { return rows[i].Props.WASMWASI.String() }).
		String("wasm_js", func(i int) string { return rows[i].Props.WASMJS.String() }).
		String("wasm_freestanding", func(i int) string { return rows[i].Props.WASMFreestanding.String() }).
		// How many of the three targets compile — the summary godepview sorts
		// its WASM column by.
		Int64("wasm_compiles", func(i int) int64 { return countCompiles(rows[i].Props) }).
		// unspecified | demo | example | integration-test.
		String("kind", func(i int) string { return rows[i].Props.Kind.String() })
}

func countCompiles(p packageprops.Props) (n int64) {
	for _, s := range [...]packageprops.WASMState{p.WASMWASI, p.WASMJS, p.WASMFreestanding} {
		if s == packageprops.WASMCompiles {
			n++
		}
	}
	return
}

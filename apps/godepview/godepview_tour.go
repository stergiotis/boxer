package godepview

import (
	"slices"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// This file enrolls godepview in the ImZero2 demo registry (ADR-0057) so the
// screenshot TestDriver can capture the explorer. The live dock app collects
// its manifest from go/packages asynchronously (~seconds), so touring the
// real app would capture the transient "Collecting…" frame. Instead the demo
// renders the explorer from a small, deterministic *seeded* fixture manifest
// — fully populated table + focused-neighborhood graph — with no collection.
// As a side effect godepview also appears in the widget gallery with this
// fixture data; the live dock app (app_register.go) is unaffected.

func init() {
	registry.Register(registry.Demo{
		Name:           "godepview",
		Category:       "Tools",
		Title:          "🕸 Go Dependency Explorer",
		Stage:          [2]float32{registry.StandardStageMaxW, registry.LargeAreaStageMaxH},
		Flags:          registry.DemoFlagNeedsLargeArea,
		Kind:           registry.DemoKindMixed,
		Description:    "godepview's master-detail explorer rendered from a seeded fixture dependency graph (no live go/packages collection): a dock of the package etable (master), the focused package's bounded import-neighborhood graph (live egui_graphs engine), and the focus detail pane with click-to-focus import/importer lists. ADR-0064.",
		Init:           makeTourInit(false),
		RenderStateful: tourRenderStateful,
		SourceFunc:     (*App).renderExplorer,
	})
	registry.Register(registry.Demo{
		Name:           "godepview-layered",
		Category:       "Tools",
		Title:          "🕸 Go Dependency Explorer (layered engine)",
		Stage:          [2]float32{registry.StandardStageMaxW, registry.LargeAreaStageMaxH},
		Flags:          registry.DemoFlagNeedsLargeArea,
		Kind:           registry.DemoKindMixed,
		Description:    "The same explorer with the focus neighborhood rendered by the layeredgraph widget (ADR-0069): a Graphviz-dot Sugiyama layout painted in-process, with arrow-headed edges. The 'engine' toggle in the running app switches between this and the live egui_graphs engine.",
		Init:           makeTourInit(true),
		RenderStateful: tourRenderStateful,
		SourceFunc:     (*App).renderGraphLayered,
	})
}

// makeTourInit builds the seeded explorer instance once per gallery/tour
// instance, pinned to the given graph engine. It bypasses Mount and collection:
// the fixture manifest is assigned directly, the index is built from it, and a
// focus is preset so the neighborhood graph is populated at capture time.
func makeTourInit(layered bool) func(ids *c.WidgetIdStack) (state any) {
	return func(ids *c.WidgetIdStack) (state any) {
		inst := &App{
			ids:          ids,
			density:      styletokens.DensityFromEnv(),
			man:          fixtureManifest(),
			showStd:      true,
			showInt:      true,
			showExt:      true,
			sortCol:      colImportedBy,
			sortDesc:     true,
			depth:        2,
			dir:          godep.DirBoth,
			graphHideStd: false, // the fixture includes stdlib; show it for a richer capture
			useLayered:   layered,
			seed:         instanceCounter.Add(1),
			viewDirty:    true,
		}
		inst.idx = inst.man.BuildIndex()
		// Focus a package with both importers and imports so the neighborhood
		// graph shows edges in both directions (index 2 == "store").
		if len(inst.man.Packages) > 2 {
			inst.focus = inst.man.Packages[2].Id
		}
		state = inst
		return
	}
}

func tourRenderStateful(ids *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	inst.ids = ids
	inst.renderExplorer()
}

// fixtureManifest is a small, deterministic dependency graph — a toy module
// importing internal packages, external modules, and stdlib — used only for
// the demo/tour render. It is never collected and never persisted; it exists
// so the screenshot shows a populated explorer without waiting on a live
// go/packages walk.
func fixtureManifest() (m godep.Manifest) {
	type spec struct {
		path    string
		name    string
		class   string
		module  string
		files   uint32
		imports []int // indices into specs (forward import edges)
	}
	const root = "example.com/shop"
	specs := []spec{
		{root + "/cmd/server", "main", godep.ClassInternal, root, 1, []int{1, 2, 7}},
		{root + "/service", "service", godep.ClassInternal, root, 4, []int{2, 3, 8}},
		{root + "/store", "store", godep.ClassInternal, root, 3, []int{3, 9, 5}},
		{root + "/util", "util", godep.ClassInternal, root, 2, []int{7, 10}},
		{root + "/api", "api", godep.ClassInternal, root, 2, []int{1, 8}},
		{"github.com/lib/pq", "pq", godep.ClassExternal, "github.com/lib/pq", 18, []int{9, 6}},
		{"github.com/jackc/pgx/v5", "pgx", godep.ClassExternal, "github.com/jackc/pgx/v5", 40, []int{9}},
		{"fmt", "fmt", godep.ClassStdlib, "std", 12, []int{10}},
		{"context", "context", godep.ClassStdlib, "std", 6, nil},
		{"database/sql", "sql", godep.ClassStdlib, "std", 22, []int{8}},
		{"strings", "strings", godep.ClassStdlib, "std", 9, nil},
	}
	pkgs := make([]godep.PackageNode, len(specs))
	for i := range specs {
		s := &specs[i]
		imports := make([]uint64, 0, len(s.imports))
		for _, j := range s.imports {
			imports = append(imports, uint64(j+1))
		}
		slices.Sort(imports)
		pkgs[i] = godep.PackageNode{
			Id:         uint64(i + 1),
			NaturalKey: []byte(s.path),
			ImportPath: s.path,
			Name:       s.name,
			Dir:        "/src/" + s.path,
			ModulePath: s.module,
			Class:      s.class,
			NumGoFiles: s.files,
			NumImports: uint32(len(imports)),
			Imports:    imports,
		}
	}
	var edges uint32
	indeg := make(map[uint64]uint32, len(pkgs))
	for i := range pkgs {
		edges += uint32(len(pkgs[i].Imports))
		for _, to := range pkgs[i].Imports {
			indeg[to]++
		}
	}
	for i := range pkgs {
		pkgs[i].NumImportedBy = indeg[pkgs[i].Id]
	}
	m = godep.Manifest{
		Run: godep.CollectionRun{
			NaturalKey:     []byte(root),
			RootModulePath: root,
			GoVersion:      "go1.25",
			Scope:          godep.ScopeTransitive,
			NumPackages:    uint32(len(pkgs)),
			NumEdges:       edges,
			BuildTags:      []string{"demo"},
			Roots:          []string{"./..."},
		},
		Packages: pkgs,
	}
	return
}

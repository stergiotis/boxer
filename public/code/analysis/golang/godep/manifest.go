package godep

// Class enumerates a package's provenance relative to the root module. It
// is stored as the plain string in PackageNode.Class (a marshallgen
// `symbol` section); these constants are the only values the LiveCollector
// emits.
const (
	ClassStdlib   = "stdlib"
	ClassInternal = "internal"
	ClassExternal = "external"
)

// Scope values for CollectionRun.Scope — what closure the run collected.
const (
	ScopeTransitive     = "transitive"
	ScopeFirstParty     = "firstParty"
	ScopeDirectExternal = "directExternal"
)

// PackageNode is one Go package — one fact of kind "goPackage". Import
// edges are embedded as the Imports adjacency set (ADR-0064 SD2): the ids
// are foreign references to other goPackage rows. The lw: tags make the
// struct marshallgen-serializable; the matching `,unit`/cardinality choice
// on the scalar count columns is finalized with the vdd memberships in the
// deferred facts step (ADR-0064 SD7/SD8).
type PackageNode struct {
	_ struct{} `kind:"goPackage"`

	Id         uint64 `lw:",id"`         // FNV-1a-64(ImportPath); stable across runs
	NaturalKey []byte `lw:",naturalKey"` // ImportPath bytes (human-readable key)
	Ts         int64  `lw:",ts"`         // collection time, unix nanoseconds

	ImportPath string `lw:"goPkgImportPath,stringArray"` // canonical import path
	Name       string `lw:"goPkgName,stringArray"`       // package-clause name
	Dir        string `lw:"goPkgDir,stringArray"`        // on-disk dir ("" when unknown)
	ModulePath string `lw:"goPkgModulePath,stringArray"` // owning module ("std" for stdlib)
	Class      string `lw:"goPkgClass,symbol"`           // stdlib | internal | external

	NumGoFiles    uint32 `lw:"goPkgNumGoFiles,u32Array"`    // .go file count
	NumImports    uint32 `lw:"goPkgNumImports,u32Array"`    // out-degree (== len(Imports))
	NumImportedBy uint32 `lw:"goPkgNumImportedBy,u32Array"` // in-degree (computed at collection)

	Imports []uint64 `lw:"goPkgImports,u64Set"` // ids of imported packages
}

// CollectionRun is one collection run — one fact of kind "goDepCollection".
// It carries the per-run metadata a facts table needs to hold many runs
// over time; all PackageNodes of a run share the run's Ts.
type CollectionRun struct {
	_ struct{} `kind:"goDepCollection"`

	Id         uint64 `lw:",id"`         // FNV-1a-64(RootModulePath ‖ Ts); the run id
	NaturalKey []byte `lw:",naturalKey"` // root module path
	Ts         int64  `lw:",ts"`         // collection time, unix nanoseconds

	RootModulePath string   `lw:"goDepRootModule,stringArray"`
	GoVersion      string   `lw:"goDepGoVersion,symbol"`
	Scope          string   `lw:"goDepScope,symbol"`
	NumPackages    uint32   `lw:"goDepNumPackages,u32Array"`
	NumEdges       uint32   `lw:"goDepNumEdges,u32Array"`
	BuildTags      []string `lw:"goDepBuildTag,symbolArray"`
	Roots          []string `lw:"goDepRoot,stringArray"`
}

// Manifest is the value that crosses the collection<->visualization seam
// (ADR-0064). Its two fields are exactly the serialized DTO kinds;
// everything else (see Index) is derived on load and never stored.
type Manifest struct {
	Run      CollectionRun
	Packages []PackageNode
}

// Direction selects which edges a neighborhood walk follows.
type Direction uint8

const (
	DirImports   Direction = iota // out-edges: packages this one imports
	DirImporters                  // in-edges: packages that import this one
	DirBoth
)

// Index is the derived lookup the visualization side builds on load via
// BuildIndex. It is fully reconstructable from Manifest.Packages — never
// serialized, never stored (ADR-0064 SD6).
type Index struct {
	byID      map[uint64]*PackageNode
	importers map[uint64][]uint64
}

// BuildIndex constructs the id->node map and the reverse-adjacency
// (importers) map from the forward Imports edges. The returned Index
// borrows pointers into m.Packages; callers must not append to or reorder
// the slice for the Index's lifetime.
func (m *Manifest) BuildIndex() (idx *Index) {
	idx = &Index{
		byID:      make(map[uint64]*PackageNode, len(m.Packages)),
		importers: make(map[uint64][]uint64, len(m.Packages)),
	}
	for i := range m.Packages {
		p := &m.Packages[i]
		idx.byID[p.Id] = p
	}
	for i := range m.Packages {
		p := &m.Packages[i]
		for _, to := range p.Imports {
			idx.importers[to] = append(idx.importers[to], p.Id)
		}
	}
	return
}

// Node returns the package with the given id, if present.
func (idx *Index) Node(id uint64) (p *PackageNode, ok bool) {
	p, ok = idx.byID[id]
	return
}

// Importers returns the ids of packages that import id (in-edges). The
// returned slice is owned by the Index and must not be mutated.
func (idx *Index) Importers(id uint64) (ids []uint64) {
	ids = idx.importers[id]
	return
}

// Len reports the number of distinct packages in the index.
func (idx *Index) Len() (n int) {
	n = len(idx.byID)
	return
}

// NeighborhoodOpts bounds and filters a BoundedNeighborhood walk. The zero
// value is an unbounded, unfiltered import-direction walk of depth 0 (just the
// root) — set MaxDepth to expand.
type NeighborhoodOpts struct {
	MaxDepth int       // hops to expand from root; < 1 yields just {root: 0}
	Dir      Direction // which edges to follow
	// MaxNodes caps the reached set (root included). 0 means unbounded. The
	// walk is breadth-first, so the cap keeps the nodes closest to root and
	// drops the farther frontier — the legibility lever for hub packages
	// whose raw neighborhood is most of the closure (ADR-0064 SD5).
	MaxNodes int
	// Include, when non-nil, filters which packages may enter the reached set
	// (e.g. to drop stdlib from the graph). A filtered node is pruned: neither
	// it nor anything reachable only through it is admitted. The root is always
	// admitted regardless of Include, so focusing a filtered package still
	// shows it.
	Include func(p *PackageNode) bool
}

// Neighborhood returns the package ids reachable from root within maxDepth
// hops, following edges per dir. The result maps each reached id to its hop
// distance from root (root itself at distance 0). maxDepth < 1 yields just
// {root: 0}. It is the unbounded, unfiltered case of BoundedNeighborhood.
func (idx *Index) Neighborhood(root uint64, maxDepth int, dir Direction) (reached map[uint64]int) {
	reached, _ = idx.BoundedNeighborhood(root, NeighborhoodOpts{MaxDepth: maxDepth, Dir: dir})
	return
}

// BoundedNeighborhood is Neighborhood with a node cap and a class/predicate
// filter. It returns the reached set (id → hop distance, root at 0) and
// truncated: the number of distinct packages that the walk would have admitted
// but dropped because MaxNodes was hit (a lower bound — the cap also stops
// those nodes' descendants from being discovered). truncated > 0 is the signal
// the view shows as "graph capped — narrow the neighborhood".
//
// Because the collected production-import graph is acyclic (Go forbids import
// cycles; test edges are not collected — ADR-0064 SD10), the walk terminates
// without an explicit visited-cycle guard beyond the reached set.
func (idx *Index) BoundedNeighborhood(root uint64, opts NeighborhoodOpts) (reached map[uint64]int, truncated int) {
	reached = map[uint64]int{root: 0}
	capped := opts.MaxNodes > 0
	var elided map[uint64]struct{}
	frontier := []uint64{root}
	for d := 1; d <= opts.MaxDepth && len(frontier) > 0; d++ {
		var next []uint64
		visit := func(to uint64, d int) {
			if _, seen := reached[to]; seen {
				return
			}
			if opts.Include != nil {
				if p, ok := idx.byID[to]; ok && !opts.Include(p) {
					return // intentionally filtered out — not a truncation
				}
			}
			if capped && len(reached) >= opts.MaxNodes {
				if elided == nil {
					elided = make(map[uint64]struct{})
				}
				elided[to] = struct{}{}
				return
			}
			reached[to] = d
			next = append(next, to)
		}
		for _, id := range frontier {
			p, ok := idx.byID[id]
			if !ok {
				continue
			}
			if opts.Dir == DirImports || opts.Dir == DirBoth {
				for _, to := range p.Imports {
					visit(to, d)
				}
			}
			if opts.Dir == DirImporters || opts.Dir == DirBoth {
				for _, from := range idx.importers[id] {
					visit(from, d)
				}
			}
		}
		frontier = next
	}
	truncated = len(elided)
	return
}

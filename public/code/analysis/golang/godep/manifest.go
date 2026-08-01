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
// Index is the derived id→node lookup a reader builds on load via BuildIndex.
// It is fully reconstructable from Manifest.Packages — never serialized, never
// stored (ADR-0064 SD6).
type Index struct {
	byID map[uint64]*PackageNode
}

// BuildIndex constructs the id->node map. The returned Index borrows pointers
// into m.Packages; callers must not append to or reorder the slice for the
// Index's lifetime.
func (m *Manifest) BuildIndex() (idx *Index) {
	idx = &Index{byID: make(map[uint64]*PackageNode, len(m.Packages))}
	for i := range m.Packages {
		p := &m.Packages[i]
		idx.byID[p.Id] = p
	}
	return
}

// Node returns the package with the given id, if present.
func (idx *Index) Node(id uint64) (p *PackageNode, ok bool) {
	p, ok = idx.byID[id]
	return
}

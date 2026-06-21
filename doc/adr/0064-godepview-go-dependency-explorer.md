---
type: adr
status: accepted
date: 2026-05-31
reviewed-by: "p@stergiotis"
reviewed-date: 2026-05-31
---

# ADR-0064: godepview — Go dependency explorer (collector / serializable manifest / viz split)

## Context

We want a keelson app that lets a developer explore the Go dependency graph of this module — packages as nodes, `import` relations as edges — using the ImZero2 `Graph` widget for visual navigation and the `EndETable` widget for a sortable/filterable package list. Scope this iteration is **Go only** and the **full transitive closure** (this module's packages plus every stdlib and external package `go list` reaches), which is on the order of thousands of nodes.

The defining requirement is **a hard seam between data collection and data visualization**. The collection step (walking the Go package graph) and the visualization step (drawing it) must be separable, because the collected data is destined to be **persisted to `runtime.facts` and read back via a `marshallgen`-generated codec** in a later iteration. The data that crosses the seam — the *manifest* — must therefore be expressible in the `marshallgen` DTO grammar today, so that the future facts step is "register memberships + run the generator", not "redesign the data model".

Forces that shape the design:

- **The collected data has a future home in `runtime.facts`.** `runtime.facts` is a leeway-shredded fact table: one row per entity (`id`/`naturalKey`/`ts` plus tagged-value *sections* — `symbol`, `stringArray`, `symbolArray`, `u32Array`, `u64Set`, `foreignKey`, …). A `marshallgen` DTO is one Go struct annotated with `kind:"…"` and `lw:"membership,section[,flags]"` tags; the generator emits SoA columns plus `Marshal`/`Unmarshal` against an Arrow/sparse-CBOR wire (see [ADR-0042](0042-keelson-leeway-codec-soa-generator.md), [`capabilitygrant.go`](../../public/keelson/runtime/codec/capabilitygrant/capabilitygrant.go)). The manifest types must fit this grammar **now**, even though the generator is not run **now**.
- **Collection is expensive and toolchain-coupled.** Loading the transitive closure means `golang.org/x/tools/go/packages` with `NeedImports | NeedDeps` (the pattern already used at [`dev/entrypoints.go`](../../public/dev/entrypoints.go)), which shells out to the `go` toolchain and parses every reachable package. The visualization step must not pay this cost on every frame, and the *render* code must not be bound to `*packages.Package` types — those types are not serializable and will not exist on the future facts-read path.
- **Thousands of nodes will not fit in one force-directed graph.** The `Graph` widget ([`egui2_graphs.go`](../../public/thestack/imzero2/egui2/bindings/egui2_graphs.go), demo [`egui2_hl_graphs_demo.go`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_graphs_demo.go)) renders one `GraphNode`/`GraphEdge` opcode per node/edge per frame; a force-directed layout of the full closure is neither legible nor performant. Legibility at this scale has to come from **filtering and focus**, not from drawing everything.
- **The two future data sources must be interchangeable.** A live in-process collector (now) and a facts-backed reader (later) must both feed the same visualization without the app changing. That argues for a narrow port between them.

Invariants the design must respect:

- **`mappingplan` grammar.** Manifest fields must use only supported shapes: scalar `T`, `option.Option[T]`, `[]T`, `*roaring.Bitmap`, `[]byte`, `[N]byte`; no nested structs, no maps, `Option` only at top level (see [`mappingplan/plan.go`](../../public/semistructured/leeway/mappingplan/plan.go)). One struct = one fact `kind`.
- **AppI lifecycle.** The app implements `Manifest()/Mount()/Frame()/Unmount()` and registers via `app.DefaultRegistry.RegisterFactory()` in `init()`; the runtime owns the window, the app fills the body (see [ADR-0057](0057-demo-registry-and-drivers.md), [`app.go`](../../public/keelson/runtime/app/app.go)).
- **FFFI2 register-drain.** Graph and table opcodes are emitted every frame and drained; widget ids come from the host-supplied `WidgetIdStack`.
- **CGO-free Go build.** Unaffected — entirely Go and existing dependencies (`go/packages` is already in `go.mod`).
- **Design-first for new packages.** This ADR is the design artefact; no app code is written until it is accepted.

## Design space (QOC)

**Question.** How should a Go dependency explorer be structured so that (a) collection and visualization are cleanly separable, (b) the data crossing the seam is `marshallgen`/`runtime.facts`-serializable today and persistable later with no model rework, and (c) thousands of transitively-reachable packages remain legible?

**Options.**

- **O1 — Monolithic app.** Collect in `Mount()` with `go/packages`, hold `[]*packages.Package`, render straight from those types. No manifest, no seam.
- **O2 — Manifest seam, ad-hoc DTOs.** Introduce an in-memory manifest type as the collection↔viz contract, but as ordinary Go structs not constrained to the `marshallgen` grammar. Clean separation; not facts-ready.
- **O3 — Serializable manifest + source port (chosen).** The contract is a `marshallgen`-grammar-compliant manifest (`PackageNode` topology kind + `CollectionRun` header kind, adjacency embedded). The app depends on a `SourceI` port and on the manifest DTOs only; a `LiveCollector` adapter (go/packages) feeds it now, a `FactsSource` adapter feeds it later. The collector package carries zero UI dependencies.
- **O4 — Two-process / one-shot CLI.** A separate `gov`/CLI subcommand serializes the manifest to disk; the app only ever loads a serialized manifest. Maximal process separation.

**Criteria.**

- **C1 — Collection ↔ visualization separation.** Are the two steps independently buildable and testable, with render code free of collection machinery?
- **C2 — `marshallgen` / `runtime.facts` readiness.** Is the seam data grammar-compliant now, and persistable later **without redesigning the data model**?
- **C3 — Legibility at full-transitive scale.** Can thousands of nodes be explored without drawing them all at once?
- **C4 — Usable today.** Does something render now, without the facts infrastructure existing first?
- **C5 — Cost to add the facts path later.** Effort to go from "live" to "facts-backed".
- **C6 — Viz-code decoupling from the go toolchain.** Is the render path free of `*packages.Package` / toolchain types?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | +  | ++ | ++ |
| C2 | −− | −  | ++ | ++ |
| C3 | −  | +  | ++ | ++ |
| C4 | ++ | ++ | ++ | −  |
| C5 | −− | −  | ++ | +  |
| C6 | −− | +  | ++ | ++ |

O3 dominates O1 and O2 on every axis except O1's day-one simplicity (C4), which O3 matches anyway because the live collector renders immediately. O3 beats O4 on C4 and on day-one cost: O4 forces an on-disk serialization format and a two-process workflow *now*, before anything can render, and the eventual canonical store is `runtime.facts` (an Arrow/sparse-CBOR wire) rather than a bespoke file — so O4's file format would be throwaway. The chosen seam (a `SourceI` port) gives O4's separation without inventing an interim file format: `LiveCollector` today, `FactsSource` tomorrow.

## Decision

Build the explorer as three layers with a `marshallgen`-serializable manifest as the contract between them.

### 1. The manifest (the serializable seam)

A new package `public/code/analysis/golang/godep/` holds the manifest DTOs and the port. It imports **neither** `go/packages` **nor** any egui binding — both sides depend on it; it depends on neither.

Two fact kinds. Topology is one kind with adjacency embedded (per the edge-model decision); run metadata is a small header kind.

```go
// PackageNode is one Go package — one fact of kind "goPackage".
// Edges are embedded as the Imports adjacency set (ADR decision: adjacency-in-node).
type PackageNode struct {
	_ struct{} `kind:"goPackage"`

	Id         uint64 `lw:",id"`          // FNV-1a-64(ImportPath); stable across runs
	NaturalKey []byte `lw:",naturalKey"`  // ImportPath bytes (human-readable key)
	Ts         int64  `lw:",ts"`          // collection time, unix nanoseconds

	ImportPath string `lw:"goPkgImportPath,stringArray"` // canonical path; high-card free text
	Name       string `lw:"goPkgName,stringArray"`       // package-clause name
	Dir        string `lw:"goPkgDir,stringArray"`        // on-disk dir ("" for stdlib in some modes)
	ModulePath string `lw:"goPkgModulePath,stringArray"` // owning module ("std" for stdlib)
	Class      string `lw:"goPkgClass,symbol"`           // "stdlib" | "internal" | "external" (low-card enum)

	NumGoFiles    uint32 `lw:"goPkgNumGoFiles,u32Array"`    // .go file count
	NumImports    uint32 `lw:"goPkgNumImports,u32Array"`    // out-degree (denormalized: len(Imports))
	NumImportedBy uint32 `lw:"goPkgNumImportedBy,u32Array"` // in-degree (computed at collection)

	Imports []uint64 `lw:"goPkgImports,u64Set"` // ids of imported packages — foreign refs to other goPackage rows
}

// CollectionRun is one collection run — one fact of kind "goDepCollection".
type CollectionRun struct {
	_ struct{} `kind:"goDepCollection"`

	Id         uint64 `lw:",id"`          // FNV-1a-64(RootModulePath ‖ Ts); the run id
	NaturalKey []byte `lw:",naturalKey"`  // root module path
	Ts         int64  `lw:",ts"`          // collection time (shared by all PackageNodes of this run)

	RootModulePath string   `lw:"goDepRootModule,stringArray"`
	GoVersion      string   `lw:"goDepGoVersion,symbol"`
	Scope          string   `lw:"goDepScope,symbol"`     // "transitive" | "firstParty" | "directExternal"
	NumPackages    uint32   `lw:"goDepNumPackages,u32Array"`
	NumEdges       uint32   `lw:"goDepNumEdges,u32Array"`
	BuildTags      []string `lw:"goDepBuildTag,symbolArray"`
	Roots          []string `lw:"goDepRoot,stringArray"` // query roots, e.g. "./..."
}
```

The in-memory aggregate exchanged across the seam is exactly those two kinds; everything else is derived on load and never stored:

```go
// Manifest is the value that crosses the collection↔visualization seam.
type Manifest struct {
	Run      CollectionRun
	Packages []PackageNode
}

// Index is the derived lookup the visualization side builds on load.
// It is fully reconstructable from Packages — never serialized, never stored.
type Index struct {
	byId      map[uint64]*PackageNode // id → node
	importers map[uint64][]uint64     // reverse adjacency (in-edges), derived from Imports
}

func (m *Manifest) BuildIndex() *Index { /* … */ }
```

### 2. The source port

```go
// SourceI is the collection↔visualization seam. The app depends only on
// this interface and on the manifest DTOs — never on a concrete collector.
type SourceI interface {
	Load(ctx context.Context) (Manifest, error)
}
```

Adapters:

- **`LiveCollector`** — package `public/code/analysis/golang/godep/godepcollect/`, the only adapter this iteration. Imports `go/packages` + `godep`. `Load` runs `packages.Load` over the configured roots/scope, classifies every package, computes degrees, and returns a `Manifest`. This is the **data-collection step**.
- **`FactsSource`** — *deferred*. A future package reading `runtime.facts` via the `marshallgen`-generated `Unmarshal` for the `goPackage`/`goDepCollection` kinds. Drops in behind `SourceI` with no change to the app's render code.

### 3. The app

`apps/godepview/` (alongside `imztop`, `play`, `capinspector`). Implements `AppI`; registered with `RegisterFactory` in `init()`. It imports `godep` (data + port) and, **only in its composition root** (`app_register.go`'s `Mount`), the concrete `godepcollect.LiveCollector` to satisfy `SourceI`. The render path (`Frame` and its helpers) sees only `Manifest`/`Index` — never `go/packages`, never the collector. This is the **data-visualization step**.

Visualization is **master–detail**, which is what keeps the full closure legible:

- **Table (master)** — `EndETable` over the full `Packages` slice (virtual-scrolled; handles thousands of rows — demo [`egui2_hl_etable_demo.go`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_etable_demo.go)). Columns: ImportPath, Name, Class, Module, #Files, Out-deg (`NumImports`), In-deg (`NumImportedBy`). Sortable by any column; text filter on path; class-toggle chips (stdlib/internal/external). Selecting a row sets the *focus* package.
- **Graph (detail)** — `Graph` over the **focused neighborhood** of the selected package only: the focus node plus its imports and/or importers out to a depth of *N* (slider 1–3), direction toggle (imports ▸ / importers ◂ / both), `Hierarchical` top-down layout. The graph never renders the whole closure — at most the focus node's local neighborhood (tens–low-hundreds of nodes). Clicking a graph node (`FetchGraphEvents`) re-focuses, so the user walks the graph hop by hop. Node colour encodes `Class`.
- **Header** — a stats line sourced from `CollectionRun` (root module, go version, scope, package/edge counts, collection time).

### Subsidiary design decisions

- **SD1 — Two kinds: `goPackage` (topology) + `goDepCollection` (run header).** Topology carries embedded adjacency; the header carries per-run metadata so a facts table can hold many runs over time. The header does not violate the "adjacency-in-node / one topology kind" decision — it is run metadata, a separate concern from graph structure.
- **SD2 — Edges are embedded as `Imports []uint64` (`u64Set`).** Per the edge-model decision. The ids are foreign references to other `goPackage` rows; `u64Set` is chosen over `u64Array` because a package's import set is unordered and duplicate-free, and over the `foreignKey` *section* because `foreignKey` in current DTOs models a single optional reference (`option.Option[uint64]`, e.g. [`capabilitygrant.go:45`](../../public/keelson/runtime/codec/capabilitygrant/capabilitygrant.go)); a set-valued `u64Set` is the supported shape for many references. Edge-level attributes (test-only, tool, indirect) are intentionally **not** modelled — adjacency-in-node has no per-edge column. If edge attributes are needed later, that is the documented upgrade path to a separate `goImportEdge` kind (the rejected edge-model alternative), and is an ADR revision, not a silent change.
- **SD3 — Stable content-addressed ids.** `Id = FNV-1a-64(ImportPath)`. Import paths are unique per package, so 64-bit collisions are astronomically unlikely; the collector nonetheless detects a collision (two distinct paths hashing equal) and fails loudly rather than silently merging nodes. Stable ids across runs are what make facts snapshots diffable: a package keeps its id between collections, so added/removed edges are visible as `u64Set` deltas and added/removed packages as row appearances/absences.
- **SD4 — Degrees are denormalized at collection time.** `NumImports` (= `len(Imports)`) and `NumImportedBy` (reverse-edge count) are stored as flat `u32` columns so the table can sort by fan-in/fan-out without a graph traversal — facts columns are flat and a "sort by in-degree" query must not require reconstructing adjacency. `NumImportedBy` is computed by the collector in a single reverse pass; it is the one value that cannot be recomputed from a single row in isolation, which is precisely why it is materialized.
- **SD5 — Legibility = filter + focus, never "draw everything".** The graph widget is fed only the focus neighborhood (SD: depth slider, direction toggle); the table is the scalable full-closure surface (virtual scroll + sort + filter). This is the load-bearing answer to the full-transitive-closure scope: the dataset is thousands of nodes, but no single `Graph` frame emits more than a focus neighborhood's worth of opcodes.
- **SD6 — Derived structures are never serialized.** `Index` (id→node, reverse adjacency) is rebuilt on `Manifest` load from `Packages` alone. The wire/facts shape stays minimal (only forward `Imports` is stored; importers are derived), and the live and facts paths produce byte-identical manifests for the same module state.
- **SD7 — `marshallgen` grammar compliance now; vdd + codegen + ClickHouse deferred.** The DTOs carry valid `kind:`/`lw:` tags today and are verified against the grammar by eye and (when accepted) by a parse-only `marshallgen` dry run. The generator is **not** run and no `runtime.facts` rows are written this iteration. The later facts step is: declare the memberships in `keelson/vdd/` (one per `lw:` tag, à la [`keelson_dimdata_taskcreated.go`](../../public/keelson/vdd/keelson_dimdata_taskcreated.go)), run the `factswrapper` generator to emit `godep/*.out.go`, and add the `FactsSource` adapter. No DTO field changes are anticipated.
- **SD8 — Section assignments, with the one caveat named.** Scalar strings → `stringArray` (matches `cgSubject`, `taskId` precedent); the low-card `Class` enum → `symbol`; the `BuildTags` enum slice → `symbolArray`; `Imports` → `u64Set`; counts → `u32Array`. The scalar-vs-array *builder* choice (the `,unit` flag, which selects `BeginAttributeSingle`) and each membership's declared `Cardinality` are a matched pair settled **with** the vdd declarations in the deferred step — e.g. `battery uint64 lw:"battery,u64Array,unit"` (unit) vs `progressCurrent uint64 lw:"progressCurrent,u64Array"` (not). The tags above are written without `,unit`; whether each scalar count takes `,unit` is finalized against its membership's cardinality when memberships are declared. This is the only tag detail deliberately left open, and it cannot drift the data model — only the wire encoding of single-valued columns.
- **SD9 — App placement and registration.** `apps/godepview/`, files: `app_register.go` (manifest + `Mount` composition root + `init()` factory), `godepview.go` (app struct + `Frame`), `godepview_table.go`, `godepview_graph.go`, `godepview_filter.go`, `doc.go`, and an embedded `help/` corpus. New `.go` files are authored **untagged** (no `//go:build` line) per the repo's llmtag convention; the llmtag tool assigns the generated-by tag.
- **SD10 — Go import graphs are acyclic among non-test packages; the focus subgraph tolerates the exception.** The Go compiler forbids import cycles, so a `Hierarchical` layout is well-defined for the production import graph. Test imports (`_test` packages) can introduce cycles; the collector records **production** imports only by default (test edges are out of scope this iteration and would be a future flag). If a focus subgraph nonetheless contains a cycle, the graph view falls back from `Hierarchical` to `ForceDirected` for that view rather than mis-rendering.
- **SD11 — Temporal facts semantics (forward-looking).** `runtime.facts` is append-only; each run writes one `goDepCollection` row + N `goPackage` rows sharing the run's `Ts`. The default `FactsSource` query is "latest run" (`argMax(ts)`); historical runs enable "what changed between two commits" diffs. The `CollectionRun.Id` ties a snapshot together; linking each `PackageNode` to its run (a `RunId` field, `foreignKey`-eligible) is deferred with the rest of the facts wiring and noted here so the kind can carry it without a later redesign.

## Alternatives

- **O1 — Monolithic app.** Smallest code, but the render path binds to `*packages.Package`, nothing is serializable, and the facts step becomes a rewrite (C2/C5/C6 all fail). Rejected: it spends the future to save a little present.
- **O2 — Manifest seam, ad-hoc DTOs.** Gets the separation (C1) and day-one usability (C4) but the seam types are not grammar-compliant, so the facts step redesigns them and re-threads every consumer (C2/C5 fail). Rejected: a seam that isn't facts-shaped defeats the stated reason for having a seam.
- **O4 — Two-process / one-shot CLI.** Strong separation, but forces an interim on-disk manifest format and a two-step workflow before anything renders (C4), and that file format is throwaway once `runtime.facts` is the canonical store. Rejected in favour of the `SourceI` port, which delivers the same separation, renders immediately, and reuses the eventual facts wire rather than inventing a stopgap. (A `gov godep` subcommand that dumps the manifest for scripting/CI remains an easy future addition on top of `LiveCollector` — it is not precluded, just not the app's data path.)
- **Separate `goImportEdge` edge kind (the other edge-model option).** Richer — supports per-edge attributes (test/tool/indirect) and edge-level filtering — but costs a second kind and a join to reassemble adjacency for every graph render. Rejected for this iteration per the edge-model decision; recorded as the upgrade path if edge attributes become necessary.
- **`*roaring.Bitmap` for `Imports` instead of `[]uint64`.** `marshallgen` supports `*roaring.Bitmap` → `u32Array`/`u32Set`, which packs dense id sets compactly. Rejected because package ids are 64-bit FNV hashes (sparse across the full u64 space), not the dense small-integer ids roaring is built for; a `u64Set` of hashes is the honest shape. (Had ids been dense run-local indices, roaring would win — noted in case a future id scheme changes that trade-off.)

## Consequences

### Positive

- **The seam is real and enforced by the import graph.** `godep` imports neither `go/packages` nor egui; the render code imports neither the collector nor `go/packages`. A test that the manifest package has no toolchain/UI imports makes the separation a build-time invariant, not a convention.
- **Facts-ready by construction.** The data crossing the seam is already in the `marshallgen` grammar, so the deferred facts step is additive (declare memberships, run the generator, add `FactsSource`) with no anticipated DTO change. Live and facts paths yield identical `Manifest` values.
- **Legible at full-transitive scale.** The table scales to thousands of rows via virtual scroll; the graph stays small via focus + depth. Neither widget is asked to do the other's job.
- **Renders today.** `LiveCollector` makes the app fully functional before any facts infrastructure exists — the facts work is decoupled and can land on its own schedule.
- **Snapshots will diff cleanly.** Content-addressed stable ids + append-only facts mean a future "diff two collection runs" feature is a query, not a re-collection.

### Negative

- **Denormalized degrees can disagree with adjacency if hand-edited.** `NumImports`/`NumImportedBy` are materialized; a manually-mutated manifest could carry counts inconsistent with `Imports`. Mitigation: only the collector writes them, and a cheap invariant check (`NumImports == len(Imports)`) runs on load in debug builds.
- **Two layout regimes in the graph view.** `Hierarchical` normally, `ForceDirected` fallback on a cyclic focus subgraph (SD10). Small added surface, but two code paths.
- **Live collection pulls the go toolchain into the app binary.** The composition root links `go/packages`, so the `godepview` binary depends on a working `go` toolchain at runtime for live mode. This is expected (matches `dev entry-points`) and disappears for consumers that use only `FactsSource`, but it means the app binary is not toolchain-free today.
- **The `,unit`/cardinality pairing is genuinely open (SD8).** It is bounded — it affects only the wire encoding of single-valued columns, not the model — but it is a loose end that the facts step must close deliberately rather than mechanically.

### Neutral

- **Manifest-aggregate vs DTO duality.** `Manifest{Run, Packages}` is an ergonomic in-memory wrapper; the serialized units are the two DTO kinds. Consumers work with the aggregate; the codec works with the kinds. This mirrors how other codec packages expose SoA `Columns` alongside row structs.
- **Test imports excluded by default.** Production import graph only this iteration (SD10); test edges are a named future flag, not a silent omission.
- **`gov godep` CLI not built, not precluded.** A scripting/CI manifest dump sits naturally on `LiveCollector` later without touching the app.

### Derived practices

- **New node attribute = one field + one membership.** Adding, say, "lines of code" is a new `lw:`-tagged `PackageNode` field plus (at facts time) one vdd membership. No consumer signature changes.
- **Render code never imports the collector.** The composition root (`Mount`) is the only place that names a concrete `SourceI`. Reviews enforce this; the no-import test backs it.
- **Adjacency stays forward-only on the wire.** Reverse adjacency and any other graph index are derived on load (SD6); they are never added as stored columns.
- **Scope changes are header-recorded.** `CollectionRun.Scope` names what was collected ("transitive" now); a future first-party-only or direct-external mode sets a different value rather than producing an unlabelled different graph.

## Status

Accepted 2026-05-31 (reviewed-by: p@stergiotis). Implemented in `public/code/analysis/golang/godep/` (manifest + `SourceI`), `public/code/analysis/golang/godep/godepcollect/` (`LiveCollector`), and `apps/godepview/` (the keelson app). The `FactsSource` adapter, vdd membership declarations, and `factswrapper` codegen remain deferred per SD7.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-31 — leeway DTO model moved to `mappingplan`

The DTO model and `lw:` grammar this ADR attributes to `marshallgen` were
extracted into the sibling `public/semistructured/leeway/mappingplan`
package (see ADR-0008 Updates). References here to the "`marshallgen`
grammar" and `marshallgen/plan.go` now denote `mappingplan`; `marshallgen`
remains the code generator that consumes a `mappingplan.Plan`, and the
deferred `factswrapper` codegen step is unchanged. The manifest DTOs are
unaffected.

### 2026-06-07 — graph guardrails, detail pane, layered-engine option

Three enhancements within the existing design; no change to the seam, the
manifest DTOs, or the deferred facts path.

- **SD5 is now enforced, not just intended.** `godep.Index` gains
  `BoundedNeighborhood(root, NeighborhoodOpts{MaxDepth, Dir, MaxNodes,
  Include})`, a breadth-first walk that caps the reached set (closest-first)
  and filters nodes by a predicate, returning a `truncated` count. The app
  caps the graph at `maxGraphNodes` (200) and, by default, excludes stdlib via
  `Include` — both load-bearing because a hub package (`fmt`, `errors`)
  reachable by an "importers" walk would otherwise emit thousands of opcodes
  per frame, which SD5 only asserted would not happen. `Neighborhood` is now
  the unbounded/unfiltered case of `BoundedNeighborhood`. The neighborhood is
  cached against a `{focus, depth, dir, hideStd}` signature, so the BFS runs
  once per change, not once per frame.
- **The master–detail layout is realized as a `DockArea`** (packages table /
  neighborhood graph / detail pane), following the schemaview/mappingplanview
  house idiom (a bounded dock leaf lets each pane's `ScrollArea` scroll). The
  new **detail pane** shows the focused package's metadata plus its direct
  Imports and Imported-by lists as click-to-focus entries — the navigation
  that still works when a neighborhood is too large to draw (the lists are
  complete, only display-bounded), complementing the capped graph.
- **A second graph engine is available behind a toggle.** The neighborhood can
  render with the existing **live** egui_graphs widget (default, interactive
  hierarchical layout) or with the **layered** widget from
  [ADR-0069](0069-imzero2-layeredgraph-widget.md): a Graphviz-`dot` Sugiyama
  layout of the same bounded neighborhood, computed in-process (cgo-free WASM)
  and cached against the neighborhood signature, with arrow-headed edges and
  pan/zoom. The dependency neighborhood is a DAG, which `dot` lays out well.
  This does not revisit SD10: the live engine remains hierarchical-on-an-acyclic
  graph; the layered engine simply uses a different layout backend for the same
  edges. Edge attributes are still not modelled (SD2), so the layered edges
  carry direction only.

### 2026-06-21 — group/module derived views + a single view switch

The explorer grows from a per-package navigator into one that also answers
*architecture-altitude* questions — how intertangled the code is, whether
sibling apps stay independent, and what the third-party-dependency surface looks
like. Every addition is a **derived** lens over the existing manifest: a pure
function of the stored `PackageNode` fields, built on load like `Index`. The
serialized seam, the DTOs, and the deferred facts path are unchanged — SD6 holds
(derived structures are never serialized), and `godep` still imports neither the
toolchain nor any UI.

- **Group quotient (`group.go`).** A path-derived grouping (`GroupOf`) folds each
  package into a group — each `apps/<name>` and `public/<area>` (first-party,
  truncated to a grouping depth), each external module (by module path), stdlib
  collapsed. `BuildGroupGraph` returns the quotient: one node per group, one edge
  per ordered group pair, weighted by the number of crossing package imports.
  Unlike the package closure (thousands of nodes), the quotient is small (tens),
  so the architecture view draws it **whole** — the SD5 filter+focus lever is not
  needed at this altitude.
- **Coupling violations (`group.go`).** `SiblingViolations` reports first-party
  import edges that cross between two distinct siblings directly under a prefix;
  with `"apps/"` it is the keelson rule "apps must not depend on each other".
  The check keys on the immediate child of the prefix, so it is **independent of
  the view's grouping depth** — sliding the graph coarser never hides a real
  violation. It flags **direct** app→app edges; transitive coupling
  (`app → lib → app`) is visible as a path in the quotient but not reddened, a
  deliberately simple first cut (a reverse-reachability variant is the noted
  upgrade if it proves necessary). This generalises the lone hand-written seam
  invariant (`godep_seam_test.go`) into a reusable, run-time check.
- **Module rollup (`module.go`).** `ModuleStats` rolls the external packages up
  by owning module and computes, per module: package count, **direct vs
  transitive** (does any first-party package import it, derived structurally from
  the graph rather than parsed from `go.mod`), first-party **fan-in**, and
  **blast radius** (the first-party packages a change to the module would reach,
  via reverse reachability). This is the dependency-surface view the per-package
  table cannot give.

The seam additions are covered by table-driven tests (`group_module_test.go`):
grouping, quotient edge weights, the apps violation, and the module stats.

On the UI side (`apps/godepview`), these surface behind a **single top-level
view switch** — *Packages · Architecture · Modules* — that reconfigures all
three dock panes together, each view organised around one focus object (a
package, a group, a module). A first cut used two independent master/graph
toggles, but that 2×2 matrix produced incoherent pane combinations (a module
table beside a per-package neighbourhood) and a detail pane that silently changed
meaning; the single switch replaced it.

- **Architecture view** — a groups table (class, package count, quotient
  in/out-degree, violation flag) as the master; the quotient rendered with the
  [ADR-0069](0069-imzero2-layeredgraph-widget.md) `layeredgraph` widget,
  class-coloured, edges labelled with crossing-import counts and **forbidden
  app→app edges in the error tone**; the violations list plus the selected
  group's member packages in the detail pane.
- **Modules view** — the rollup table as the master, the quotient with external
  modules folded in (selected module highlighted), and the selected module's
  first-party importers + blast set in the detail pane.

The architecture graph reuses ADR-0069's engine with **no new widget
capability** — the quotient is a flat graph whose nodes happen to be groups.
Nested group *clusters* (Graphviz `subgraph cluster_…`, drawn as nested boxes)
would need a cluster field on the `layeredgraph` model and box-painting in its
view; that is a possible future ADR-0069 enhancement, deliberately out of scope
here (the light cut ships; the heavy 10% is deferred).

No change to the seam, the manifest DTOs, or the deferred facts path. The
`gov godep` subcommand floated in the original Alternatives is now the natural
home for these same derived queries (group coupling, module rollup) as a
CI/scripting surface — still not built, still not precluded.

## References

- [ADR-0042](0042-keelson-leeway-codec-soa-generator.md) — `marshallgen` SoA codec generator; the grammar the manifest DTOs target.
- [ADR-0057](0057-demo-registry-and-drivers.md) — demo registry + AppI/registration pattern followed by the app layer.
- [`public/keelson/runtime/codec/capabilitygrant/capabilitygrant.go`](../../public/keelson/runtime/codec/capabilitygrant/capabilitygrant.go) — reference DTO: `kind:`, `lw:` tags, `id`/`naturalKey`/`ts` plain columns, `foreignKey` for an optional reference.
- [`public/keelson/runtime/codec/inflightsnapshotreply/inflightsnapshotreply.go`](../../public/keelson/runtime/codec/inflightsnapshotreply/inflightsnapshotreply.go) — reference DTO: `[]string`→`stringArray`/`symbolArray`, `[]uint64`→`u64Array` array sections.
- [`public/keelson/vdd/keelson_dimdata_taskcreated.go`](../../public/keelson/vdd/keelson_dimdata_taskcreated.go) — membership declaration shape (`MustBegin(...).MustAddRestriction(section, spec, cardinality)`) for the deferred facts step.
- [`public/keelson/runtime/factsschema/factsschema.go`](../../public/keelson/runtime/factsschema/factsschema.go) — the `runtime.facts` section vocabulary the `lw:` tags must name.
- [`public/keelson/runtime/codec/factswrapper/factswrapper.go`](../../public/keelson/runtime/codec/factswrapper/factswrapper.go) — the wrapper that turns the DTOs into facts `Marshal`/`Unmarshal` (run in the deferred step).
- [`public/dev/entrypoints.go`](../../public/dev/entrypoints.go) — established `packages.Load(NeedImports|NeedTypes|…)` pattern the `LiveCollector` follows.
- [`public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_graphs_demo.go`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_graphs_demo.go) — `Graph`/`GraphNode`/`GraphEdge` usage, layout selection, `FetchGraphEvents`.
- [`public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_etable_demo.go`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_etable_demo.go) — `EndETable` virtual-scroll, deferred cells, 10k-row demo.
- [`public/keelson/runtime/app/app.go`](../../public/keelson/runtime/app/app.go) — `AppI` interface and `Mount`/`Frame` context surface.

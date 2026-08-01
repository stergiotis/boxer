---
type: adr
status: proposed
date: 2026-08-01
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0157: The Go dependency explorer as keelson tables plus a SQL applet suite

## Context

[ADR-0064](0064-godepview-go-dependency-explorer.md) built `godepview`: a
windowed app that collects this module's Go package graph with
`go/packages` and renders three views behind one switch — Packages (the
full-closure table, a bounded neighbourhood graph, a detail pane),
Architecture (the group quotient, sibling-app violations, group cycles) and
Modules (the external rollup, direct-vs-transitive, fan-in, blast radius,
witness path). Its three dated Updates added the guardrails, the derived
group/module lenses and a second graph engine. It works, and it is ~2 550
lines of Go in `apps/godepview/` plus the derived analysis in
`public/code/analysis/golang/godep/`.

Two things have changed since.

**Applets exist.** [ADR-0132](0132-sqlapplet-sql-defined-applets.md) mints a
real app from a committed markdown document whose first `sql` fence is the
play buffer, with `tabs:` binding result panels to named CTEs. Two books
ship today (`apps/sqlapplet/book`, `apps/sqlapplet/booktopo`); the six-doc
topology suite showed the shape holds for a whole observability surface. F4
of that ADR is load-bearing here: **no Go per applet, ever** — a view that
needs Go graduates to an embedder app instead. So every godepview view has
to be expressible as SQL over tables, or it does not move.

**The repository is already queryable.** `keelson('adr')`, `subtask`,
`coderef`, `adrcontent` ([`providers/adr.go`](../../public/keelson/runtime/introspect/providers/adr.go))
are introspection tables that answer *what the repository contains* rather
than what the process contains — they do filesystem I/O at query time, are
`Live`, degrade to empty off-repo, and pin their root with an env var.
[ADR-0122](0122-play-kanban-panel.md) §SD4 records that tension and why it
was accepted rather than resolved. `keelson('sbom')` and
`keelson('extbin')` are further build-artefact tables in the same family.

Against that, the dependency graph is the one substantial repo-corpus
dataset that is *not* reachable from SQL. Its derived analyses live only as
Go methods on `godep.Index` with a single consumer, so nothing else can ask
"which ADRs cite a package in the blast radius of this module", and the
quotient/blast-radius/witness answers cannot be joined against
`keelson('coderef')` or `keelson('sbom')` at all.

Forces that shape the decision:

- **The dataset is small — measured, not assumed.** `go list -deps -json`
  under the repo's build tags, warm: **1.4 s**, **1 411 packages**, **13 030
  import edges**, 292 stdlib packages, 122 modules, 484 first-party
  packages. Every derived view is a `WITH RECURSIVE` over a few tens of
  thousands of rows, which the in-process engine already runs (the topology
  suite's `component-deps` applet is a recursive closure, live-verified).
- **Collection is toolchain-coupled.** It shells out to `go` through
  `golang.org/x/tools/go/packages`. `x/tools` must not become a dependency
  of appliance builds ([ADR-0128](0128-imzero2-mesh-draw-stream-codec-lane.md)'s
  musl-static/gokrazy lane), so where the provider is *registered* matters
  as much as what it does.
- **`Provider.Snapshot` takes no context.** A slow collect blocks the query
  path with no cancellation
  ([`introspect.go`](../../public/keelson/runtime/introspect/introspect.go)).
  A warm 1.4 s is tolerable once; a cold toolchain run is unbounded, and
  clicking around a Live applet must not pay it repeatedly.
- **play cannot do three things godepview does.** A Network-tab vertex click
  publishes nothing — [ADR-0129](0129-play-layered-graph-panel.md) §SD4
  records that emitting the row-index `selection` signal was *tried*, was
  erased by `syncSelectionClamp`, and was reduced to a local highlight, with
  cross-panel selection deferred in §SD7. The Table tab has no header sort.
  And graph colour is `group`→auto-palette only, so "forbidden edges in the
  error tone, cycle edges in the warning tone" has no expression.

## Design space (QOC)

**Question.** How should the Go dependency graph reach a SQL applet, given
that no applet may carry Go, and what is the smallest set of play
extensions that preserves godepview's navigation?

**Options** (the data route; the applet and play halves follow from it):

- **O1 — keelson providers over a cached live collector.** Register
  `go_packages` / `go_imports` / `go_collection` / `go_package_props` as
  introspection tables backed by the existing `godepcollect.LiveCollector`,
  with a provider-side cache.
- **O2 — collector app publishing ad-hoc datasets.** A windowed app collects
  and publishes Arrow datasets over `adhoc.publish`, then embeds the applet
  with `datasets:` bindings — the
  [ADR-0134](0134-adhoc-datasets.md) `adhocdemo` shape.
- **O3 — build-time generated, embedded table.** Generate the manifest with
  `go generate` and embed it, as `packageprops`
  ([ADR-0080](0080-packageprops-per-package-declarations.md)) embeds
  `proptable`.
- **O4 — persist to `boxer.facts`.** Finish ADR-0064 §SD7 (vdd memberships,
  `factswrapper` codegen, a writer) and query the shredded facts table.

**Criteria.**

- **C1 — Freshness.** Does a query see the working tree as it is now?
- **C2 — Reach.** Can any play session query it and join it with the other
  repo-corpus tables, or only the app that produced it?
- **C3 — Weight on non-dev builds.** Does `x/tools` (or a facts pipeline)
  land in binaries that have no use for it?
- **C4 — Per-query latency.** What does an ordinary click cost?
- **C5 — Cost to build now.** Work before the first applet renders.
- **C6 — Precedent.** Does the repo already have this shape working?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | +  | ++ | −− | −  |
| C2 | ++ | −  | ++ | ++ |
| C3 | +  | ++ | ++ | −  |
| C4 | +  | ++ | ++ | +  |
| C5 | +  | +  | −  | −− |
| C6 | ++ | +  | +  | −− |

O3 fails the point of the app: a graph that is only as current as the last
`go generate` cannot answer "did my change just couple these two apps".
O4 is the largest build by a wide margin — it needs the deferred facts
pipeline *and* a comfortable way to read shredded facts back as columns —
and this box has no ClickHouse-backed facts persistence wired, so it would
render nothing today; it stays the recorded long-term home for *historical*
runs, where it is the only option that answers "what changed between two
commits". O2 wins on freshness and latency and is the honest choice if the
data should be session-scoped, but it fails C2: the graph would exist only
inside the collector app's window, which is exactly the property that keeps
the derived analyses unjoinable today. O1 takes O2's reach concern off the
table at the cost of a cache and a registration-site constraint, and it has
the closest precedent (`adr`/`coderef` are repo-corpus tables already).

## Decision

Rebuild the explorer as **four keelson tables plus a four-document applet
book**, and close the three play affordance gaps the views depend on.

### 1. The tables

A new provider package (`public/keelson/runtime/introspect/providersgodep/`)
exposing:

| Table | Rows (this repo) | Content |
|---|---|---|
| `go_packages` | 1 411 | one row per package: `id`, `import_path`, `name`, `dir`, `module_path`, `class`, `num_go_files`, `num_imports`, `num_imported_by` |
| `go_imports` | 13 030 | one row per edge: `src_id`, `dst_id`, `src_path`, `dst_path` |
| `go_collection` | 1 | the run header: root module, go version, scope, tags, roots, counts, `collected_at`, plus `status` / `duration_ms` / `error` |
| `go_package_props` | ~ first-party | the ADR-0080 declarations (WASM verdicts per target) from the embedded `proptable` |

`go_packages` and `go_imports` are the `PackageNode` fields of ADR-0064
projected to flat columns; `go_collection` is its `CollectionRun`. The
manifest DTOs and the `SourceI` seam are unchanged — the provider is a third
consumer of the same `Manifest`, beside the app and the deferred
`FactsSource`.

### 2. The applet book

`apps/sqlapplet/bookgodep/`, registered as a third `RegisterBook("godep", …)`
so the four documents are each other's help corpus:

- **`go-overview.md`** — the collection header, counts by class and module,
  and what the other three answer.
- **`go-packages.md`** — `tabs: [table, network, detail]`: the full-closure
  table, the focused package's bounded neighbourhood as `vertices`/`edges`
  CTEs, and the focused row's metadata.
- **`go-architecture.md`** — the group quotient, sibling-app violations and
  group cycles.
- **`go-modules.md`** — the external rollup with fan-in, direct-vs-transitive,
  blast radius and the witness path.

All four are `endpoint: introspection`, classify **read** under ADR-0132
§SD5, and expose their knobs as unbound `{name:Type}` params in the params
strip ([ADR-0124](0124-play-param-editing-widgets.md)).

### 3. The play extensions

- **`selection_key`** — a String signal carrying the clicked entity's key,
  published by a Network vertex click and by a Table row click.
- **Table header sort** — click to sort, click again to reverse.
- **Graph colour tokens** — an optional `tone` column on the `vertices` /
  `edges` CTEs naming a design-system token.

### Subsidiary design decisions

- **SD1 — A flat edge table beside the node table.** ADR-0064 §SD2 embedded
  adjacency in the node (`Imports []uint64` → `u64Set`) because that is the
  right *fact* shape. It is the wrong *query* shape: every recursive walk
  and every join wants pairs, and `arrayJoin` on each hop is both slower and
  harder to read. `go_imports` therefore materialises the edge list, and
  carries the endpoints' import paths beside their ids so the common query
  never joins back to `go_packages` twice. The two are redundant by
  construction and produced from one `Manifest` in one pass, so they cannot
  disagree.
- **SD2 — Live freshness with a provider-side collect-once cache, collected
  asynchronously.** `FreshnessStatic` is wrong: the engine may cache a
  provider's Arrow bytes for the whole run, so the first (empty) snapshot
  would be frozen in. Synchronous collection is wrong: `Snapshot` has no
  context, and a cold toolchain run would block a query indefinitely. So the
  providers declare `FreshnessLive` and answer from a cached `Manifest`;
  the first touch starts collection in the background and returns **zero
  rows** — the `sbom`/`adr` "empty rather than erroring" behaviour — while
  `go_collection` reports `status='collecting'`. A Live applet fills in on
  its next tick. Recollection is deliberately **not** wired to a query: it
  is a process-lifetime cache, which is exactly godepview's own snapshot
  semantics ("collection runs once when the window opens"). A refresh
  affordance is deferred with a trigger (below).
- **SD3 — Registered from the carousel, not from `introspecthost`'s static
  set.** `introspecthost` links into hosts that must stay free of
  `golang.org/x/tools`. The carousel already builds one shared registry (the
  ad-hoc dataset service and the introspection host share it), so the godep
  providers register there, in the dev composition root. `providersgui` is
  the existing precedent for a provider package excluded from the static
  set.
- **SD4 — Root and tags are env-pinned, per ADR-0009.** `BOXER_GODEP_ROOT`
  (empty resolves the nearest `go.mod` above the working directory) and
  `BOXER_GODEP_TAGS` (empty falls back to the root's `tags` file, then
  inherited `GOFLAGS`) — the `BOXER_ADR_DIR` idiom, and the same resolution
  `godepview` does today. Off-repo the tables are empty, not an error.
- **SD5 — Four documents, not one buffer with a view switch.** Each lens
  gets its own buffer, tabs, params and prose, as in the topology suite. The
  cost is godepview's cross-view jump ("click a violation → see that package
  in the Packages view"), which becomes: open the other applet and paste the
  path. Recorded as a deferral, not as parity.
- **SD6 — `selection_key` is a new signal, not the row-index `selection`.**
  ADR-0129 §SD4 established that publishing `selection` from the Network
  panel is erased by `syncSelectionClamp` *and* would jerk Table and Detail
  to row 0, and §SD7 deferred cross-panel selection to "when the graph's CTEs
  become observable nodes". A String `selection_key` carrying a *value*
  rather than a *cursor* sidesteps both: it is not node-scoped, nothing
  clamps it, and no existing panel reads it. The Network panel emits the
  clicked vertex's `id`; the Table panel emits the clicked row's `key` column
  when the result has one (the ADR-0122 named-column contract, not
  detection). An applet reads it as `{selection_key:String}`, so a click in
  either panel refocuses every lane that references it. This is a smaller
  change than the observable-node path and does not preclude it.
- **SD7 — Header sort is a view permutation, not a re-query.** The Table tab
  sorts the Arrow record it already holds, so sorting works for every result
  including lanes whose SQL cannot be re-issued, and costs no round trip.
  The selection row index maps through the permutation, so a sorted click
  still selects the row the user clicked.
- **SD8 — Colour is an optional token column, and the palette decision
  governs.** `vertices` / `edges` may carry a `tone` column naming a
  design-system token; absent it, the `group` auto-palette applies unchanged.
  [ADR-0156](0156-qualitative-palette-dark-surface.md) governs which tones
  exist and how they behave against the dark spine — this adds a way to
  *name* a tone, not a new palette.
- **SD9 — What is not kept, stated plainly.** The live/layered **engine
  toggle** goes: play's Network tab is layered-only (ADR-0129), and a second
  engine there is that ADR's decision to make, not this one's. The **detail
  pane's clickable Imports / Imported-by lists** go: the neighbourhood graph
  plus `selection_key` covers the same navigation, and play has one Detail
  tab bound to one node. The **cross-view jump** goes (SD5). Everything else
  in the help corpus is in scope for parity.
- **SD10 — Parity is verified twice: in a test, then live.** Every buffer
  runs verbatim through `introspectengine.Query` in a book test — the
  technique the topology suite established, where the applet's own `SET
  param_` prelude binds server-side — and the four applets are then driven in
  the GUI against a running `godepview` for a view-by-view comparison.
- **SD11 — `godepview`'s fate is decided after parity, not before.** The app
  and its Go-side derived analyses (`group.go`, `module.go`, with their
  table-driven tests) stay until the applets are verified; the outcome is
  recorded as a dated Update on ADR-0064. `godep` + `godepcollect` survive
  either way — the providers depend on them.

## Alternatives

- **O2, the collector app with ad-hoc datasets.** Rejected as the primary
  route because the data would live only inside the collector app's session
  (C2), which is the limitation this rebuild exists to remove. It remains
  the recorded upgrade path for exploring a module *other* than the one the
  process was started in — the trigger is wanting a second root without
  restarting.
- **O3, a generated embedded table.** Rejected on freshness: a table that is
  as old as the last `go generate` cannot answer questions about the change
  you just made.
- **O4, persist to `boxer.facts`.** Not rejected — deferred, and still the
  only route that gives *historical* runs and cross-run diffs (ADR-0064
  §SD11). It is out of scope here because it needs the deferred facts
  pipeline and a read-back path, and would render nothing on a box with no
  facts persistence wired.
- **One applet with a `{view:String}` param.** Keeps the single-window feel
  and the cross-view jump, at the cost of a buffer whose CTEs branch three
  ways. Rejected: the book form is how the repo already packages a suite,
  and a `multiIf`-shaped buffer is precisely the "pasteable-complete" property
  ADR-0132 asks each document to keep.
- **Making the Network panel's CTEs observable nodes** (the ADR-0129 §SD7
  path) so the standard `selectionStamper` applies. Deferred rather than
  rejected: it is the general fix and a larger one; `selection_key` is the
  smaller mechanism that unblocks this suite, and it stays useful afterwards
  because a vertex id is a value, not a cursor.
- **Re-issuing the query to sort** instead of permuting the record. Rejected:
  it makes sorting cost a round trip and unavailable on lanes that cannot be
  re-issued.

## Consequences

### Positive

- The dependency graph becomes joinable with the rest of the repo corpus —
  `keelson('adr')`, `coderef`, `sbom`, `extbin` — from any play session, not
  only from one app's window.
- The derived analyses stop being private methods with one consumer: the
  quotient, the violation check, the blast radius and the witness path
  become queries a reader can inspect, edit and paste.
- Three play affordances land that every applet gets: value-carrying
  selection, header sort, and named graph tones — the first two are gaps any
  table/graph applet hits.
- The ADR-0132 §F4 boundary is respected: no Go is added per view.

### Negative

- **A repo-corpus table that shells out to the toolchain.** ADR-0122 §SD4
  already accepted filesystem I/O at query time; this goes further by
  spawning `go list`. The cache and the async first collect bound the cost,
  but a query's answer now depends on where the process was started *and* on
  a working toolchain.
- **Two representations of the same graph** (`go_packages.num_imports` and
  `go_imports` rows) — denormalised on purpose (SD1), produced in one pass,
  but a hand-written provider change could still drift them.
- **The process-lifetime cache means stale data has no in-app remedy**: a
  collection taken at start-up is what you get until restart. That is
  godepview's behaviour too, so it is not a regression, but the applet form
  makes it more visible because the window stays open longer.
- **Three named parity losses** (SD9). The engine toggle in particular was a
  deliberate ADR-0064 addition.
- **`x/tools` in the carousel binary** stays a real link cost; SD3 confines
  it, it does not remove it.

### Neutral

- The four documents are pasteable-complete against a running boxer process,
  which is what makes them useful outside the applet window — but they are
  useless against a plain ClickHouse, because `keelson()` resolves only at
  the introspection endpoint.
- Applet slugs carry dashes, so `--launch go-packages` needs the WHERE form
  (`--launch "subject_alias = 'go-packages'"`), as the topology suite found.

### Derived practices

- **A new node attribute is a column, not a view change.** Adding a field to
  `PackageNode` surfaces as a `go_packages` column and is immediately
  queryable by every applet; no consumer signature moves.
- **A new lens is a document.** The next question about the graph is a fifth
  markdown file, not a Go pane.
- **Repo-corpus providers state their cost.** `adr.go` documents its per-query
  cost in prose; the godep providers do the same, including what "empty"
  means while a collection is in flight.

## Status

Proposed 2026-08-01.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- [ADR-0064](0064-godepview-go-dependency-explorer.md) — the app this rebuilds; its manifest DTOs and `SourceI` seam are unchanged.
- [ADR-0132](0132-sqlapplet-sql-defined-applets.md) — SQL-defined applets; §F4 (no Go per applet) and §SD5 (query security class).
- [ADR-0129](0129-play-layered-graph-panel.md) — the Network panel; §SD4/§SD7 record the local-selection cut and the colour/selection deferrals this ADR closes.
- [ADR-0122](0122-play-kanban-panel.md) — §SD4 accepts repo-corpus introspection tables; the named-column panel contract.
- [ADR-0094](0094-keelson-introspection-tables.md) — the introspection table family and its two transports.
- [ADR-0134](0134-adhoc-datasets.md) — the ad-hoc dataset route kept as the recorded alternative.
- [ADR-0080](0080-packageprops-per-package-declarations.md) — the per-package declarations behind `go_package_props`.
- [ADR-0124](0124-play-param-editing-widgets.md) — the params strip the applet knobs render in.
- [ADR-0156](0156-qualitative-palette-dark-surface.md) — the palette the `tone` column names into.
- [`providers/adr.go`](../../public/keelson/runtime/introspect/providers/adr.go) — the repo-corpus provider this family follows.
- [`godepcollect/collector.go`](../../public/code/analysis/golang/godep/godepcollect/collector.go) — the collector the providers reuse.
- [`play_layeredgraph_panel.go`](../../apps/play/play_layeredgraph_panel.go) — the `vertices`/`edges` CTE contract the graph applets target.

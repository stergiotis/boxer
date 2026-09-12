---
type: adr
status: accepted
date: 2026-09-12
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-12
---

# ADR-0229: an in-process graph analytics engine — CSR core, Ligra-shaped iteration, results as facts

## Context

`graphview` ([ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md))
and its navigation layer
([ADR-0225](./0225-graphview-navigation-layer-and-radial-layout.md)) draw a
graph and walk it by hops. Nothing in the tree computes over one: no
centrality, no cores, no cliques, no PageRank, no components outside the
patch repository's Tarjan. The metrics a reader wants when a graph is on
screen — which vertex is the bridge, which cluster is dense, what is the
core — have no source, and the navigation layer's relevance, radial centre
and node radius have only degree and the query's own `weight` column to
draw on.

The survey behind this record
([graph analytics engine survey](../adr-background-work/graph-analytics-engine-survey.md))
traced the shared-memory literature from Ligra and X-Stream to the papers
that adjudicate between them, and read the result against three facts of
the tree:

- The house already moved its Go-side graph lenses into SQL once
  ([ADR-0064](./0064-godepview-go-dependency-explorer.md), 2026-08-01
  Updates), for the reason that a lens with one consumer belongs where the
  corpus can join it. ClickHouse carries the bounded walks, closures and
  quotients well. It cannot carry Brandes' dependency accumulation or
  maximal-clique enumeration, and its recursive CTEs are append-only, so
  every fixpoint in the applet books is depth-bounded by hand.
- The bar for a reimplementation is isomorphism or closed-loop
  observability, not sovereignty alone
  ([ADR-0069](./0069-imzero2-layeredgraph-widget.md)). Both hooks exist
  here: one implementation must serve the widget and the fact table, and
  the metric, its budget and its truncation must land as facts.
- The workload is repository-, vault-, profile- and topology-sized —
  thousands to low millions of edges — and the draw side is capped three
  orders of magnitude below that
  ([ADR-0227, play panel](./0227-play-graphview-panel.md) §SD9). The
  out-of-core and distributed literature is not the operating point; the
  single-thread baseline of *Scalability! But at what COST?* is.

Two constraints carry over from the widget ADRs. Every layout and walk in
graphview is a pure function of the topology, and the gallery capture
relies on that; the engine inherits the rule. And the graph contract is
SQL-side and settled: the `edges` / `vertices` CTEs of
[ADR-0129](./0129-play-layered-graph-panel.md) §SD2, hoisted into one
renderer-neutral model by ADR-0227 (play panel). The engine consumes that
model, not a new one.

## Design space (QOC)

**Question.** Where do whole-graph metrics — centralities, cores, cliques,
components, PageRank — get computed, and in what shape?

**Options.**

- **O1** — SQL only: extend the applet books; no Go engine.
- **O2** — Reference `gonum/graph` behind an adapter over the network model.
- **O3** — A first-party CSR core and a Ligra-shaped iteration engine in
  Go, algorithms as thin functions over them, results published as facts.
- **O4** — A native C engine (SuiteSparse:GraphBLAS / LAGraph) compiled
  to WebAssembly under wazero, as Graphviz is.
- **O5** — A dedicated analytics worker process, Go-owned and spawned
  through `extbin`, driven by the FFFI2 IDL, generators and pipe channel
  but not by the frame clock; the worker in Rust (`rayon`, SIMD) or in Go
  through the `goserver` stubs.
- **O6** — A first-party Rust engine compiled to wasm32 and run in-process
  under wazero, the shape [ADR-0003](./0003-h3-wasm-bridge.md) chose for
  H3.

**Criteria.**

- **C1** — Performance at 10³–10⁶ edges, interactive on one machine.
- **C2** — Maintainability: lines owned and seams crossed per algorithm.
- **C3** — Sovereignty per why-boxer P1: no cgo, airgapped build, every
  load-bearing line auditable, foreign interfaces not bound into the
  representation.
- **C4** — Correctness: an oracle exists, and results are deterministic
  under parallelism.
- **C5** — Covers the metrics SQL cannot: betweenness, maximal cliques,
  unbounded fixpoints.
- **C6** — Runs where the data is, on every tier: in the Go host beside
  the network model, natively on desktop and appliance, and under the
  browser tier of [ADR-0077](./0077-keelson-browser-wasm-execution.md).

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 | O6 |
|----|----|----|----|----|----|----|
| C1 | −  | −  | ++ | +  | +  | −  |
| C2 | ++ | ++ | −  | −− | −− | −  |
| C3 | ++ | +  | ++ | −− | +  | ++ |
| C4 | +  | ++ | +  | +  | +  | +  |
| C5 | −− | +  | ++ | +  | ++ | ++ |
| C6 | ++ | ++ | ++ | +  | −− | −  |

O1 is dominated on C5: it cannot produce the two metrics that prompted the
question, and it stays the home of what it does well under every option.
O2 is the cheapest route to C5 but pays an interface call and a map lookup
per edge, is single-threaded, offers no sampled betweenness, and binds the
representation to its interfaces — the callback-shape coupling P1 names.
It stays the test oracle. O4 fails the `CGO_ENABLED=0` gate except through
wasm, which forfeits the vectorised kernels that justify the library, and
does not list cliques. O5 is the one option O3 does not dominate: it has
the higher ceiling, it isolates an exponential-output algorithm in a
process the host can kill, and the shape is one the tree has — the
one-shot worker pool of [ADR-0028](./0028-chlocal-low-latency-sql-cap.md)
over the [`extbin`](./0118-extbin-external-process-chokepoint.md)
chokepoint, with the runtime's synchronous call rather than egui2's frame
cadence ([ADR-0062](./0062-imzero2-render-cadence.md)). It loses on
maintainability (an IDL node and apply code per algorithm, a binary, a
pool, and a second language on the data path if Rust, which amends the
architecture's render-only statement), on the browser tier (no wasm
threads under ADR-0077), and its advantage is confined to the
super-linear kernels because copying the edge list down a pipe costs as
much as a linear kernel runs. O6 keeps the process boundary and the
build gate, but wazero gives a module no threads, so parallelism is one
graph copy per pooled instance; under the browser tier the host is itself
wasm and the engine would be interpreted; and the two-toolchain cost
ADR-0003 accepted for a finished upstream crate would be paid on every
edit of an engine boxer writes itself. O3 loses only on lines owned, that
cost is bounded and serves two consumers, and its surface is the contract
O5 would implement — so O3 is the base and O5 the deferred accelerator
(SD8).

## Decision

We build a first-party in-process graph analytics engine in Go, as a
`graph` package under [`public/analytics`](../../public/analytics/) beside
[`public/analytics/timeseries`](../../public/analytics/timeseries/). It
owns a CSR container, a Ligra-shaped iteration layer, and the algorithms
below; it consumes the play network model and publishes its results as
columns keyed by vertex id. SQL keeps the bounded walks, closures and
quotients it already carries.

**SD1 — One CSR, built once per topology.** A `csr` package maps `uint64`
vertex ids to dense `int32` slots and holds forward and reverse adjacency
as offset and target arrays, with optional `float32` edge weights, and
neighbours sorted by id within each row, so every walk is a function of
the topology alone. It is built by counting sort from an edge list in the
shape of the network model (`source`, `target`, optional `weight`), with
parallel edges collapsed and self-loops kept and flagged. Undirected
graphs are stored as both directions once; the container records which it
is. Nothing else in the engine touches an adjacency map.

**SD2 — Ligra-shaped iteration with direction as a schedule and
deterministic parallelism.** An `engine` layer exposes a vertex subset
(sparse index list or dense bitmap, converted on a size threshold) and an
edge map whose direction — sparse push from the subset, or dense pull
over all destinations — is an argument of the call, never a property of
an algorithm, and is chosen per sweep on the subset's edge count against
a threshold that is a parameter, not an inherited constant. The dense
sweep pulls, because every write is private to its destination and the
per-vertex fold runs in id order, so it is deterministic without help.
A push sweep is made deterministic by partitioning destinations so each
worker owns the slots it writes, or by per-worker buffers folded in
worker order; never by atomics. Parallelism is contiguous-chunk fan-out
with per-worker scratch and an inline path under a size floor — the
shape graphview's `parallelRows` has — so the parallel result is
bit-identical to the single-thread one. Sampling is seeded;
vertex-order-dependent algorithms fix their order by id.

**SD3 — The first cut of algorithms.** Degree; BFS distances and hop
neighbourhoods; connected components by union–find; strongly connected
components by iterative Tarjan (the shape `pushoutgraph/algo` already
proved); PageRank in pull form with a fixed iteration count and a
convergence readback; k-core decomposition and degeneracy ordering by
bucket peeling; triangle count by degree-ordered intersection; betweenness
by Brandes, exact when the vertex count is under a caller-set bound and
otherwise by seeded pivot sampling with the sample size as the budget;
maximal cliques by Bron–Kerbosch with pivoting, the outer level in
degeneracy order, with an output cap. This is the GAP six less weighted
shortest paths, plus the two the question named.

**SD4 — Budgets and truncation are part of every result.** Each algorithm
takes a `context.Context` and a budget (iterations, pivots, output cap,
wall-clock) and returns, beside its columns, whether it was truncated and
by which limit. A truncated result is a valid result with a flag, never an
error — the `capped` convention of the play network model. Nothing in the
engine allocates proportional to output without a cap.

**SD5 — Results are columns, and they become facts.** Every algorithm
returns struct-of-arrays slices aligned with the CSR's slot index plus the
id slice, so a consumer joins back to ids without a map. Persisting them
to `boxer.facts` through a facts-bound record store
([explanation](../explanation/facts-bound-record-stores.md)) — one fact
kind per metric family, keyed by the graph's content fingerprint and the
vertex id, carrying the budget and truncation flag — is the closed-loop
hook and is **descoped to a follow-up ADR** so this one does not gate on
the schema. Until then the columns are consumed in-process.

**SD6 — Consumers, in order.** The play graphview panel gains a metric
column source for node radius and tone; the navigation layer's radial
centre and focus relevance may take a centrality instead of degree; both
are additive against ADR-0225. The three private CSRs in graphview, nav
and pushoutgraph are candidates to migrate to `csr` and are not migrated
by this decision.

**SD7 — Deferred, with triggers.** Louvain/Leiden communities (when a
consumer asks for clusters rather than cores); weighted shortest paths
(when a weighted `edges` contract has a consumer); dynamic updates to a
built CSR (when a consumer's topology changes faster than a rebuild
amortises — the counting sort is linear); a `simd` inner loop for the
dense sweeps (the same trigger as ADR-0224 §SD6, the build environment
setting the experiment); hoisting the chunked parallel-for into a shared
package (when a third caller appears); the O5 worker tier (when a kernel
measured on a real workload, after the `simd` experiment lands, exceeds
the interactive budget in Go, or when an algorithm's output growth wants
a process boundary — and the worker's language is decided then, with a
Rust worker needing an amendment to the render-only statement in
[ARCHITECTURE](../ARCHITECTURE.md) as its own record).

**SD8 — The engine surface is IDL-expressible, so a worker tier is a
transport change.** Every algorithm takes plain values and
struct-of-arrays slices and returns the same: a graph addressed by its
content fingerprint, budgets as numbers, truncation as a flag, columns
aligned to slots. No callback, no interface, no in-process state an
algorithm depends on beyond the CSR it was handed. That is the shape an
FFFI2 procedure and fetcher already carry, so the O5 worker — Go behind
`goserver` stubs or Rust behind the generated interpreter — can implement
the same Go API behind a pool, one-shot per job with cancellation by
kill, without the algorithms' consumers changing. Its trigger is in SD7.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API under `public/` | added: `public/analytics/graph/{csr,engine,algo}` | the play graphview panel as first consumer; no existing exported API reshaped |
| `boxer.facts` schema | unchanged by this ADR | the metric fact kinds are the follow-up ADR named in SD5 |
| Build and toolchain gates | unchanged: `CGO_ENABLED=0`, no new module dependency | `gonum/graph` subpackages are imported by tests only, from a module already in `go.mod` |

## Alternatives

- **SQL only (O1).** Cannot express Brandes or clique enumeration, and
  every fixpoint is depth-bounded by hand under append-only recursive
  CTEs; kept for the walks and joins it carries well.
- **`gonum/graph` as the engine (O2).** Interface-and-map per edge,
  single-threaded, no sampled betweenness, and its interfaces would become
  the representation; kept as the test oracle.
- **GraphBLAS / LAGraph through wasm (O4).** Fails the cgo gate natively,
  loses the vectorised kernels under wazero, and does not list cliques.
- **A dedicated worker process over FFFI2 (O5).** Deferred, not
  rejected: its advantage is confined to the super-linear kernels because
  the edge list crosses a pipe per job, it loses the browser tier's
  threads, and a Rust worker amends an architectural statement; SD8 keeps
  the Go API in the shape the worker would implement, and SD7 names the
  trigger.
- **A Rust engine via wasm under wazero (O6).** Right for wrapping a
  finished upstream crate that the tree never edits, as H3 is; wrong for
  code boxer writes and revises, and single-threaded per module with an
  interpreted path in the browser tier.
- **Edge-centric streaming (X-Stream shape).** Answers the out-of-core
  question, which ClickHouse answers for boxer; in memory it costs a full
  edge scan per iteration where frontier algorithms need a fraction of one
  (Malicevic et al. 2017).
- **Extend `pushoutgraph/algo` instead of a new package.** Its interface
  and 40-byte `NodeID` are the patch theory's, not a CSR's; its Tarjan is
  reused by shape, not by import.

## Consequences

### Positive

- The two metrics that prompted the question, and the rest of the GAP
  set, become available over every graph the applet books already emit.
- One implementation serves the widget and, after the follow-up ADR, the
  fact table — the isomorphism the ADR-0069 bar asks for.
- Determinism is designed in, so the gallery capture and the golden tests
  stay stable under parallelism.
- No new module dependency and no cgo; the airgapped build is untouched.

### Negative

- Roughly three thousand lines of owned algorithmic Go, plus tests of the
  same order, whose correctness the tree must earn rather than inherit.
- A fourth CSR exists until the migration SD6 leaves for later.
- Betweenness and cliques carry budgets, so a consumer must read the
  truncation flag; a reader who ignores it can draw a wrong picture.

### Neutral

- The engine has no opinion on where the graph came from: the network
  model is its input, and the applet books stay the way a graph is
  declared.
- SQL and the engine overlap on components and BFS. The overlap is
  deliberate: SQL's form is joinable, the engine's is fast, and the trial
  named in the verification plan measures the gap.

## Migration — Tier 1

Nothing to migrate. The surface is additive; no existing consumer,
encoding or registry changes shape.

## Verification plan — Tier 1

- **Lane.** Default `go test`: property tests
  ([`pgregory.net/rapid`](https://pkg.go.dev/pgregory.net/rapid), the
  house property-test library) over random small graphs comparing every
  algorithm against a brute-force oracle and against `gonum/graph`; a
  determinism test that runs each algorithm at one worker and at
  `GOMAXPROCS` workers and bit-compares the outputs; benchmarks on a
  synthetic RMAT graph at 10⁵ and 10⁶ edges. A trial under
  [`doc/trials`](../trials/) is the place for citable numbers against the
  SQL forms and is a follow-up, not a gate.
- **What would fail.** A parallel result that differs from the
  single-thread one; a metric that disagrees with the oracle on any
  generated graph; a budgeted algorithm that allocates past its cap.
- **Gap.** The oracle is only as strong as the brute force it can afford,
  so the property tests bound graphs at tens of vertices; the RMAT
  benchmarks check speed, not correctness, at scale. Acceptable because
  every algorithm in SD3 is size-independent in its logic.

## Status

Accepted 2026-09-12.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-09-12 — the first cut shipped

SD1–SD4 and SD8 are in the tree as
[`public/analytics/graph`](../../public/analytics/graph/): `csr` (the
container), `engine` (subset, edge map, chunked parallelism) and `algo`
(every algorithm SD3 names). The verification plan's lane runs: property
tests against a brute-force oracle and against `gonum/graph` on generated
graphs, the one-worker-versus-many bit comparison on an R-MAT graph, and
benchmarks. SD5's persistence and SD6's consumers are not started.

Two refinements found in implementation:

- **The push sweep is serial.** SD2 allowed a destination-partitioned
  parallel push; the first cut runs push serially and pulls in parallel,
  because push is chosen only while the frontier's edges are under the
  dense threshold, so its cost is bounded by construction and a serial
  push is deterministic for free. A parallel push is a later refinement
  with the same contract.
- **Building the CSR costs more than sweeping it.** On one laptop
  (2026-09-12, an 8-thread mobile part that throttles; figures moved by up
  to 2× between runs, so read the order of magnitude), an R-MAT graph of
  2¹⁷ vertices and 2²⁰ edges built in about a quarter of a second, of
  which the id-to-slot map lookups and the counting sort's scatter passes
  are the bulk; a BFS on it took under ten milliseconds, twenty PageRank
  sweeps about a tenth of a second, connected components and k-core tens
  of milliseconds, triangle counting well under a second on all cores,
  betweenness from 64 pivots about a second on all cores and two and a
  half single-threaded, and maximal cliques capped at 200 000 about a
  second. The ATC'17 finding holds at this size: the build is the cost to
  watch. A sort-based slot relabel was prototyped the same day — radix
  sort of (id, index) pairs per column, a merge walk assigning slots — and
  agreed with the map path bit for bit; it saved about a quarter of the
  relabel stage on dense ids and under a tenth on random 64-bit ids, where
  each column needs every radix pass, for a stage that is a third of the
  build. Not adopted. It and a dense-id fast path stay deferred until a
  consumer rebuilds often enough to notice; a trial under
  [`doc/trials`](../trials/) is where these figures become citable.
- **The slot map stays.** Slots are assigned in id order, so the sorted
  id slice is itself a lookup structure and the map is redundant in
  memory. Measured the same day on two million random 64-bit ids over
  131 k keys: the map answered in about 36 ns, an Eytzinger-laid copy of
  the id slice with a branchless search in about 57 ns, and a plain binary
  search in about 150 ns. The layout's remaining gain in the literature
  comes from prefetching three levels ahead, which Go has no intrinsic
  for; emulating it with a load put the cost near 190 ns. If the map is
  ever dropped for memory, the Eytzinger slice is the replacement, at
  roughly 1.6× the map's lookup cost.

## References

- [graph analytics engine survey](../adr-background-work/graph-analytics-engine-survey.md) — the literature trace, the tree inventory and the costed options behind the QOC.
- [ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md), [ADR-0225 (nav)](./0225-graphview-navigation-layer-and-radial-layout.md), [ADR-0227 (play panel)](./0227-play-graphview-panel.md) — the widget, the navigation layer and the graph contract the engine consumes.
- [ADR-0064](./0064-godepview-go-dependency-explorer.md) — the precedent for moving graph lenses into SQL.
- [ADR-0069](./0069-imzero2-layeredgraph-widget.md) — the bar a reimplementation must clear.
- [why-boxer P1](../explanation/why-boxer.md) — the dependency rule the assessment's C3 applies.
- [ARCHITECTURE](../ARCHITECTURE.md) — the render-only doctrine and the process boundaries C6 and O5 are judged against.
- [ADR-0003](./0003-h3-wasm-bridge.md), [ADR-0077](./0077-keelson-browser-wasm-execution.md) — the Rust-via-wasm precedent, the FFFI2 transport, and the deferred Rust-host fork.
- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md), [ADR-0062](./0062-imzero2-render-cadence.md), [ADR-0118](./0118-extbin-external-process-chokepoint.md), [ADR-0206](./0206-gokrazy-appliance-image.md) — the one-shot worker pool, the frame cadence, the external-process chokepoint and the appliance image, which together bound O5.
- Shun & Blelloch, *Ligra*, PPoPP 2013; Dhulipala, Blelloch & Shun, *GBBS*, SPAA 2018; Beamer, Asanović & Patterson, *The GAP Benchmark Suite*, 2015; Malicevic, Lepers & Zwaenepoel, USENIX ATC 2017; McSherry, Isard & Murray, *Scalability! But at what COST?*, HotOS 2015; Besta et al., *To Push or To Pull*, HPDC 2017; Zhang et al., *GraphIt*, OOPSLA 2018 — full citations in the survey's references table.

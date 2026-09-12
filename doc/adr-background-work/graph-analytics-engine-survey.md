---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-12 to feed
> [ADR-0226](../adr/0226-graph-analytics-engine.md); nothing here is a
> decision. Provenance is three-tiered: (a) claims about this repository were
> checked against the working tree on the compile date; (b) claims about
> papers and libraries come from the cited sources, fact-checked the same day
> by web lookup, with two papers read in full (Malicevic et al. 2017; Besta et al. 2017); (c)
> line counts, running-time bounds applied to boxer-sized graphs, and the
> workload sizes in §3 are estimates and are marked with a tilde. No
> measurement was taken for this page.

# Graph analytics in-process — what the literature settles, and what the tree already decided

## 1 Question and scope

`graphview` ([ADR-0224](../adr/0224-graphview-go-graph-widget-painter-lane.md))
and its navigation layer
([ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md))
give boxer a way to *look at* a graph. The question here is what it should
take to *compute over* one: betweenness and other centralities, clique
discovery, cores, components, and the rest of the standard kit — and where
that computation should live so that it stays on the Pareto front of
performance, maintainability, sovereignty and correctness rather than
winning one axis and losing the others.

The user's framing names two papers as the root of the literature to trace:
Ligra (shared-memory, vertex-centric, frontier-based) and X-Stream
(edge-centric, streaming). §4 traces both lines forward to the papers that
adjudicate between them, and §5 reads the result against boxer's premises.
§6 costs the options; §7 is the recommendation the ADR turns into a
decision.

Out of scope: graph *drawing* (settled by ADR-0224/0225 and
[graph forms and magnitude](./graph-forms-and-magnitude.md)), graph
*storage* as a first-class ClickHouse shape (the edge list *is* the storage,
see §2.3), and distributed processing (§4.3 explains why).

## 2 What the tree already has

Three findings shape everything below. They are stated first because the
literature reads differently once they are known.

### 2.1 Three CSRs and one algorithm package

The compressed-sparse-row adjacency has been written three times, each
private to its consumer and each deterministic by construction (neighbours
sorted by id, so a walk is a function of the topology alone):

- [`widgets/graphview`](../../public/thestack/imzero2/egui2/widgets/graphview/)
  keeps an undirected CSR (`adjStart` / `adjList` / `adjEdge`) over
  struct-of-arrays node state, rebuilt on topology change, and walks it in
  the force, hierarchical and radial layouts. The hierarchical layout builds
  a second, out-edge-only CSR of its own.
- [`widgets/graphview/nav`](../../public/thestack/imzero2/egui2/widgets/graphview/nav/)
  keeps another, with a per-entry direction flag, and runs a bounded
  breadth-first walk over it for the visible subset.
- [the pushout graph package](../../public/algebraicarch/pushout/pushoutgraph/) is the
  one general algorithm package: an iterative Tarjan SCC (deliberately not
  recursive, because recursion "blows the goroutine stack on long files"),
  Kahn's topological sort, reachability, and a union–find — all over an
  interface (`GraphReaderI`) with Go iterators, keyed by a 40-byte
  `NodeID`, and consumed only by the patch repository.

Absent in Go anywhere in the tree (2026-09-12): PageRank, any centrality,
k-core, clique enumeration, shortest paths, triangle counting, community
detection. There is also no shared parallel-for helper: `graphview` has a
package-private `parallelRows` (contiguous chunks, one goroutine each,
per-worker scratch, an inline path under a size threshold), and three
other packages carry their own channel worker pools. Go 1.27's `simd`
package is used nowhere; ADR-0224 §SD6 defers it until the build
environment sets the experiment.

### 2.2 The house already moved graph lenses *out* of Go once

[ADR-0064](../adr/0064-godepview-go-dependency-explorer.md)'s 2026-08-01
Updates record the deletion of `godep`'s derived lenses — bounded
neighbourhood, group quotient, sibling violations, **strongly connected
components**, reverse reachability, witness paths — and their re-expression
as SQL over the `keelson('go_*')` tables, "so they can be read, edited and
joined with the rest of the repository corpus rather than being methods
with one consumer." The SQL that replaced them lives in the applet books:

- [go-packages](../../apps/sqlapplet/bookgodep/go-packages.md): a
  depth-bounded, direction-switchable BFS neighbourhood, closest-first and
  capped.
- [go-architecture](../../apps/sqlapplet/bookgodep/go-architecture.md): a
  quotient graph with cycle detection by depth-bounded mutual reachability.
  Its comment states the constraint precisely: "a ClickHouse recursive CTE
  has no DISTINCT across iterations, so a cyclic walk would otherwise spin.
  Measured on this repository, the answer stops changing at depth 6."
- [go-modules](../../apps/sqlapplet/bookgodep/go-modules.md): reverse
  reachability to a bounded depth.
- [topology-queries](../howto/topology-queries.md): transitive closure of
  `component-needs`.
- [profile-callgraph](../../apps/sqlapplet/bookpprof/profile-callgraph.md):
  a call graph *derived* from stack arrays by `arrayJoin` over adjacent
  pairs — the edge list never exists as a table.
- [markdown-facts-obsidian-queries](../howto/markdown-facts-obsidian-queries.md):
  the wikilink graph resolved by a three-way join, and backlinks as a
  `groupUniqArray`. One hop, no recursion.

So the precedent is not "graph algorithms belong in Go". It is: **a lens
with one consumer belongs in SQL where the corpus can join it**. Any Go
engine has to argue for the algorithms SQL cannot carry, and has to return
its results to the corpus rather than keep them.

### 2.3 The bar for a reimplementation, and the graph contract

[ADR-0069](../adr/0069-imzero2-layeredgraph-widget.md) states the house bar
when it rejects a clean-room Sugiyama: "reimplementations are justified by
**isomorphism** (ImZero ⇄ ImGui) or **closed-loop observability**
(spinnaker) — *not* sovereignty alone." Sovereignty
([why-boxer P1](../explanation/why-boxer.md)) makes a dependency a liability
to be carried, not a reason to write code; the working rule is coupling,
not volume, and a "boring" dependency — stable surface, vetted licence, no
cgo, no service tether, no callback shape binding boxer's internals — is
referenced, not rewritten.

The graph contract itself is settled and is SQL-side:
[ADR-0129](../adr/0129-play-layered-graph-panel.md) §SD2's two
convention-named CTEs, `edges` (`source`, `target`, optional `label`,
`tone`, `weight`) and `vertices` (`id`, optional attributes), hoisted by
[ADR-0225 (play panel)](../adr/0225-play-graphview-panel.md) into one
renderer-neutral model with endpoint synthesis, parallel-edge collapse and
caps as an argument. An engine that consumes that model consumes every
graph the applet books already produce.

## 3 The workload, sized

Everything the tree draws or queries as a graph is *repository-, vault-,
profile- or topology-sized*: package import graphs, wikilink graphs,
call graphs derived from profiles, process/socket topologies, capability
maps. Those are ~10³–10⁶ edges (estimate; no census was taken). The draw
side is capped far below that — ADR-0225 (play panel) §SD9 caps the
graphview tab at 2 000 vertices and 6 000 edges, with its stated
measurement that the parallel Barnes–Hut repulsion "takes about 0.5 ms at
1 000 nodes and about 3.4 ms at 5 000" on one laptop.

Two consequences. First, the engine's input fits in memory by orders of
magnitude, so the out-of-core literature is not the operating point.
Second, the *analytics* size and the *drawing* size differ by up to three
orders of magnitude, so the engine computes over the whole graph and the
widget draws a subset — exactly the split ADR-0225 (nav) already makes,
whose stated negative consequence is that "a consumer with millions of
nodes feeds the helper a neighbourhood, not the whole graph." Metrics are
what decide which neighbourhood.

## 4 The literature, traced

### 4.1 The Ligra line — vertex subsets and edge maps

**Ligra** (Shun & Blelloch, PPoPP 2013) reduces shared-memory graph
processing to two operations over a CSR: a `vertexSubset` (a frontier, kept
sparse as an index list or dense as a bitmap) and `edgeMap`, which applies
a function along every edge leaving the frontier whose target passes a
filter and returns the new frontier. `edgeMap` switches between a sparse
(push, from the frontier) and a dense (pull, over all vertices) sweep on a
threshold of the frontier's edge count — the direction-optimising BFS of
Beamer, Asanović & Patterson (SC 2012) generalised to every frontier
algorithm. Ligra's claim is that BFS, betweenness, radii, components,
PageRank and Bellman-Ford each fit in a few dozen lines against that
abstraction while matching hand-tuned code.

The line continues through **Ligra+** (compressed adjacency, 2015),
**Julienne** (bucketing for priority-ordered work: k-core, Δ-stepping,
2017) and **GBBS** (Dhulipala, Blelloch & Shun, SPAA 2018 / TOPC 2021),
whose "theoretically efficient" thesis is that work-efficient parallel
algorithms with polylogarithmic depth are also the fast ones in practice.
GBBS (C++, MIT) implements over twenty problems on those abstractions:
connectivity, biconnectivity, SCC, MST, spanning forest, low-diameter
decomposition, maximal matching and independent set, colouring, set
cover, PageRank, triangle and k-clique counting, densest subgraph,
**k-core and degeneracy ordering**, k-truss, BFS, Bellman-Ford,
integer-weight SSSP, **single-source betweenness**, widest path, k-spanner,
SCAN and hierarchical agglomerative clustering. It is the closest thing to
a canonical inventory of what a shared-memory engine should offer, and its
per-algorithm choices are a ready reference for each one.

**GraphIt** (Zhang et al., OOPSLA 2018) is the same abstraction as a DSL
with a separate schedule language; **Gemini** (OSDI 2016) carries the
push/pull dual to distributed memory. Neither changes the abstraction; both
confirm it as the fixed point of the line.

### 4.2 The X-Stream line — edge-centric streaming

**X-Stream** (Roy, Mihailovic & Zwaenepoel, SOSP 2013) keeps the
scatter–gather model with state on vertices but iterates over *edges*,
streaming a completely unordered edge list in sequential passes, because
sequential bandwidth exceeds random-access bandwidth on every medium (RAM,
SSD, disk). Vertices are partitioned so each partition's state fits in
cache; updates are streamed to per-partition buffers and gathered. The
line runs from **GraphChi** (parallel sliding windows, OSDI 2012) through
X-Stream to **GridGraph** (2-D edge grid, ATC 2015).

What X-Stream settles: when the graph does not fit in memory, or when an
algorithm touches every edge every iteration, a streaming layout beats
random access and needs no preprocessing beyond partitioning. What it gives
up: there is no cheap way to touch *only the frontier*, so every iteration
costs a full edge scan.

### 4.3 The adjudicators

Three later papers measure the two lines against each other and against a
baseline. They, not the originals, decide the question for boxer.

**Malicevic, Lepers & Zwaenepoel, USENIX ATC 2017** ("Everything you
always wanted to know about multicore graph processing but were afraid to
ask") implements the techniques of Ligra, X-Stream, GridGraph, Polymer and
others in one system and toggles them in isolation, measuring end to end
(load, preprocess, run). Findings, from the paper:

- "The cost of pre-processing in many circumstances dominates the cost of
  algorithm execution." Building adjacency lists from an in-memory edge
  list is best done by radix sort; elaborate layouts (grids, Hilbert
  order, NUMA partitioning) pay off only for whole-graph algorithms on
  large machines.
- Vertex-centric CSR wins for frontier algorithms (BFS) "because during an
  iteration BFS only works on a limited subset of the graph. Edge arrays
  are not well suited for this type of computation, as all edges of the
  graph are read at every iteration." For PageRank the two tie end to end;
  for a single pass (SpMV) edge-centric wins because CSR construction is
  never amortised.
- Push–pull switching improves BFS algorithm time but "the overall
  execution is completely dominated" by the extra preprocessing (a second,
  reverse CSR), 1.5× worse end to end on their input.

**McSherry, Isard & Murray, HotOS 2015** ("Scalability! But at what
COST?") define COST as the configuration a scalable system needs to
outperform a competent single thread, and show that for PageRank and
label-propagation connectivity on billion-edge graphs, published
distributed systems at 128 cores mostly do not reach it. Their single-thread
Rust over a CSR runs 20 PageRank iterations on the 1.4-billion-edge Twitter
graph in about 300 s — roughly 10 ns per edge-iteration, single-threaded,
on a 2015 laptop — and Hilbert-curve edge ordering cuts label propagation
a further 9.4×. The lesson is not "never parallelise" but "measure against
one good thread first."

**The GAP Benchmark Suite** (Beamer, Asanović & Patterson, 2015) fixes six
kernels and their reference algorithms: direction-optimising BFS,
Δ-stepping SSSP, pull PageRank, Afforest / Shiloach–Vishkin connected
components, Brandes betweenness, and order-invariant triangle counting
with degree relabelling. It is the smallest set a new engine should be
measured on, and the reference implementations (C++/OpenMP, BSD) are the
oracle for "state of the art at this size."

### 4.4 Push, pull, and the switch between them

The push/pull dichotomy is the one design axis inside the Ligra line that
the originals leave implicit and the later literature makes explicit. It
decides the engine's inner loop, its synchronisation, and — for boxer —
its determinism, so it is traced on its own.

**The two directions.** A *push* sweep iterates the sources on the
frontier and writes into each out-neighbour's state: cheap when the
frontier is sparse, but the writes are random and contended, so parallel
push needs a compare-and-swap or a lock per update. A *pull* sweep
iterates destinations and reads each one's in-neighbours: the reads are
random but every write is to the destination's own slot, so pull needs no
atomics and is race-free by construction, at the cost of touching every
destination's in-edges whether or not anything changed, and of a reverse
CSR for a directed graph. Beamer, Asanović & Patterson (SC 2012) made
this a BFS with two phases, top-down and bottom-up, and a switch on the
frontier's size; the GAP reference keeps their heuristic with two
parameters, going bottom-up when the frontier's out-edges exceed the
unexplored edges divided by α, and back when the frontier shrinks below
the vertex count divided by β (α = 15, β = 18 in the reference code).
Ligra generalised the switch to every frontier algorithm through the
`edgeMap` threshold on the frontier's edge count.

**Direction is a schedule, not an algorithm.** GraphIt (OOPSLA 2018)
separates the algorithm from a schedule that names the direction
(`SparsePush`, `DensePush`, `DensePull`, and the `SparsePush-DensePull`
hybrid) and the frontier representation (bitvector, boolean array, sparse
array), and shows the best choice varies by graph and by iteration, not
by algorithm. Its finding that `DensePull` "does random reads and mostly
sequential writes, which are cheaper" than push's random writes is the
cache-level reason pull tends to win once the frontier is dense. The
design consequence for an engine is that the direction belongs on the
edge-map call, chosen per sweep, and no algorithm hard-codes it.

**What the systematic study found.** Besta, Podstawski, Groner,
Solomonik & Hoefler (HPDC 2017, "To Push or To Pull") derive push and
pull variants of eleven algorithms and count their atomics, locks, reads
and writes: "pushing usually suffers from excessive amounts of
atomics/locks while pulling entails more memory reads/writes." Pull
removes the atomics entirely for BFS, PageRank and SSSP; push wins where
the frontier is small or where convergence is faster in push form (their
graph-colouring result consistently favours push). Their generic
strategies to reduce both costs — partition-aware pushing so a thread
owns the destinations it writes, and switching schemes — are the ones
later engines adopted.

**The vector-era refinements.** Grazelle (Grossman, Litz & Kozyrakis,
PPoPP 2018) found pull engines slow for reasons of engineering rather
than algorithm — poor inner-loop parallelisation and a CSR that defeats
vectorisation — and fixed both with a scheduler-aware parallel loop and a
"Vector-Sparse" layout, as a hybrid push/pull engine. Wedge (Grossman &
Kozyrakis, 2019) went pull-only by transforming the source-oriented
frontier into a pull-friendly "Wedge Frontier", beating Ligra by up to
4.9× and a naive pull by orders of magnitude on sparse iterations.
Graptor (Vandierendonck, ICS 2020) is the first to vectorise push, by a
graph partitioning ("CleanCut") that rules out inter-thread races so a
thread's pushes never collide, and reports 2.7× over Ligra. All three
depend on a vector ISA; they mark what a Go engine without `simd` leaves
on the table and a Rust one could reach (§6 O5, O6).

**The preprocessing cost of pull.** Pull on a directed graph needs the
reverse CSR. Malicevic et al. (§4.3) measured that second CSR as the
dominant end-to-end cost of push–pull BFS on their billion-edge inputs,
1.5× worse overall despite a faster algorithm phase. The finding scales
with the graph: at the §3 sizes a second counting sort is sub-millisecond
and the objection does not apply.

**Which of the §4.8 metrics it touches.** The direction question is live
only for the frontier and fixpoint family: BFS and the forward pass of
Brandes (hybrid, switch on frontier size), Brandes' backward dependency
pass (pull over the transpose, level by level, as Ligra's BC does),
PageRank (pull, the GAP reference: no atomics, each vertex sums its
in-neighbours), label-propagation components (pull). It does not touch
union–find components, Tarjan, k-core peeling in its sequential form,
triangle intersection or clique recursion, which have no frontier.

**Why it matters more to boxer than to the papers: determinism.** The
papers weigh push against pull by time. Under the house rule that a
result is a function of the topology alone (§5), the two are not
symmetric: pull is deterministic by construction, since every write is
private to its destination and a per-vertex float sum runs in id order;
push under atomics is not, because concurrent float adds commute only
approximately and a CAS race decides which thread's value lands. A
deterministic push therefore needs either per-worker private buffers
folded in worker order (memory times workers) or Graptor's shape,
destinations partitioned so each worker owns the slots it writes. Both
are available; pull is simply free. That tilts an engine toward pull as
the default dense sweep and sparse push only where its frontier is small
enough that a serial or destination-partitioned push costs nothing.

### 4.5 The database side

**Fan, Raj & Patel, CIDR 2015** ("The Case Against Specialized Graph
Analytics Engines") translate vertex-centric programs into SQL over a
column store (Grail, on Vertica) and find it competitive with GraphLab and
Giraph on BFS, PageRank, SSSP and connected components — the iterative,
whole-graph kernels. Betweenness and clique enumeration are not in the
comparison, and the paper does not claim them.

ClickHouse supports `WITH RECURSIVE` since 24.4 (new analyzer), with
append-only semantics: each iteration's rows are appended, there is no
settled set across iterations, and cycle guards must carry path arrays
per row. An RFC opened 2026-06-10
([ClickHouse#107067](https://github.com/ClickHouse/ClickHouse/issues/107067))
names the consequence — "no settled set → exponential re-derivation" — and
proposes keyed recursion after DuckDB's `USING KEY`, with a prototype
branch and measured speedups; it is a proposal, not a release. The tree's
own depth-bounded walks (§2.2) are the working answer to the same
limitation.

What this settles for boxer: SQL over ClickHouse carries the bounded
walks, closures, quotients and joins well, and is the right home for them
for the reason ADR-0064 gave. It does not carry a fixpoint to convergence
without a depth bound, cannot express Brandes' dependency accumulation
(a reverse pass over a BFS DAG with per-vertex path counts) without
materialising the DAG per source, and has no idiom for the combinatorial
recursion of maximal-clique enumeration. Those are the algorithms an
engine exists for.

### 4.6 Linear-algebra engines and embedded graph databases

**GraphBLAS** (SuiteSparse:GraphBLAS, Apache-2.0, C) and **LAGraph** (BSD)
express algorithms as sparse matrix operations over semirings; LAGraph
carries BFS, PageRank, connected components, triangle counting and a
*batched* betweenness (simultaneous BFS from a set of sources). Two
objections are decisive here. Both are C behind cgo, and the build is
`CGO_ENABLED=0` ([ADR-0215](../adr/0215-retire-mimalloc-reproducible-builds.md));
the only sanctioned route is WebAssembly through wazero, as
[the layeredgraph Graphviz engine](../../public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine/)
does for Graphviz, which forfeits the vectorised kernels that are the
whole point. And maximal-clique enumeration is not a semiring operation;
the line's answer to combinatorial problems is k-clique *counting*, not
listing.

**Kùzu** (embedded property-graph database, C++) and **DuckPGQ** (SQL/PGQ
in DuckDB) add a second query language and a second storage engine beside
ClickHouse. Both fail the cgo rule and the one-durable-place premise;
neither is considered further.

### 4.7 Go libraries

`gonum.org/v1/gonum` is already a module dependency (for `mat` and
`dsp/fourier`); its `graph` subpackages are imported nowhere in the tree
(2026-09-12). They carry, under BSD-3: `topo` (Bron–Kerbosch, degeneracy
ordering, Tarjan SCC, connected components, topological sort), `network`
(betweenness, closeness, harmonic, PageRank, HITS), `community` (Louvain
modularity), `path` (Dijkstra, Bellman-Ford, Floyd–Warshall, A*). Its
shape is interface-based — `graph.Graph`, `Node` with `ID() int64`,
iterator objects, map-backed simple graphs — so every algorithm pays an
interface call and a map lookup per edge, is single-threaded, and orders
its output by map iteration unless the caller sorts. The dependency is
"boring" in P1's sense for a *test oracle*; as the engine it binds the
graph representation to its interfaces, which is the callback-shape
coupling P1 names. No other Go graph library is in the module graph.

### 4.8 The algorithms themselves

Per metric, the reference algorithm and its cost at the §3 scale. Bounds
are the papers'; the applied figures are estimates.

| Metric | Reference algorithm | Cost | Note at ~10⁵ vertices / 10⁶ edges |
| --- | --- | --- | --- |
| Degree, in/out | CSR offsets | O(1) per vertex | free |
| BFS distances, hop neighbourhoods | Ligra frontier with dense/sparse switch (Beamer 2012) | O(n + m) | milliseconds |
| Strongly connected components | iterative Tarjan (as `pushoutgraph/algo`) | O(n + m) | milliseconds; the parallel alternative (GBBS) is not needed at this size |
| Connected components | union–find, or Afforest (Sutton, Ben-Nun & Barak, IPDPS 2018) | ~O(m α(n)) | milliseconds |
| PageRank | pull, Jacobi or Gauss–Seidel, fixed iteration count (GAP) | O(k · m) | ~10 ns/edge-iteration single-thread (COST); 20 iterations ≈ 0.2 s |
| k-core / degeneracy ordering | Batagelj & Zaveršnik 2003, bucket peeling | O(m) | milliseconds; also the ordering cliques need |
| Triangle count | order-invariant, degree-ordered intersection (GAP) | O(m · d_max) worst, ~O(m^1.5) | sub-second |
| Betweenness, exact | Brandes 2001: one BFS + reverse dependency pass per source | O(n · m) unweighted | ~10¹¹ edge visits: minutes to hours; exact is for ≤ ~10⁴ vertices |
| Betweenness, approximate | pivot sampling (Bader, Kintali, Madduri & Mihail 2007); sample-size bounds via VC-dimension (Riondato & Kornaropoulos 2014/2016); adaptive (KADABRA, Borassi & Natale 2016/2019) | O(s · m) for s sources | s ≈ 100–1 000 pivots: seconds; error bound is a parameter |
| Maximal cliques | Bron–Kerbosch with Tomita pivoting (2006); outer level in degeneracy order (Eppstein, Löffler & Strash 2010) | O(d · n · 3^{d/3}) for degeneracy d | fast on sparse real graphs; output can be exponential, so the listing must be capped |
| Communities | Louvain (2008) / Leiden (Traag, Waltman & van Eck 2019) | ~O(m log n) per level | seconds; result depends on vertex order — determinism needs a fixed order |

Two properties recur. Every whole-graph metric in the table is a frontier
or a full sweep over a CSR — the Ligra shape — and none needs the
edge-streaming shape. And two of the user's named metrics, betweenness and
cliques, are precisely the ones with a super-linear cost, so the engine's
contract must carry a *budget* (pivots, output cap, iteration cap) and a
truncation flag, in the way the play network model already carries
`capped`.

## 5 Reading it against the premises

**Scale (COST).** At 10⁶ edges a single well-written thread over a CSR
finishes every linear-time metric in milliseconds and PageRank in a
fraction of a second. Parallelism is worth having for the super-linear
metrics (betweenness pivots are embarrassingly parallel; clique branches
are, with care) and for the dense sweeps, but it is the second step, and
the single-thread implementation is the oracle the parallel one must
match bit for bit. This also means goroutine fan-out over contiguous
chunks — the `parallelRows` shape the tree already has — is enough; a
work-stealing scheduler is not on the path.

**Abstraction (Ligra), with direction as a schedule (§4.4).** A
`VertexSubset` plus `EdgeMap` with the dense/sparse switch is the internal
API that makes each algorithm short, keeps the CSR walks in one place, and
gives parallelism one seam. The direction of each sweep is an argument of
that call, not a property of the algorithm, as GraphIt's schedule
language separates it. The dense sweep pulls by default because pull is
race-free and deterministic without help; a push sweep is used where the
frontier is sparse, and is made deterministic by destination partitioning
or per-worker buffers folded in a fixed order, never by atomics. This
costs one extra CSR (the reverse adjacency for pull), which ATC'17 found
to dominate end to end on their inputs — but at the §3 scale a second
counting sort is sub-millisecond, and the tree builds directed CSRs
anyway. The switch threshold is a parameter to measure, not to inherit.

**Streaming (X-Stream).** The edge-centric line answers a question boxer
has already answered differently: the durable, larger-than-memory edge list
lives in ClickHouse, which streams it, and an engine loads a working graph
from the `edges` contract. X-Stream's contribution — no preprocessing,
sequential passes — is what the SQL side does; importing it in-process
would duplicate the column store's job at the wrong layer.

**Preprocessing (ATC'17).** The one preprocessing step an engine needs is
the counting sort from an edge list into a CSR, which the tree performs
twice already. Anything beyond that (relabelling by degree for triangle
counting, degeneracy ordering for cliques) is algorithm-local and
linear-time. Hilbert ordering and NUMA layouts are out of scope at this
size.

**Determinism (the house rule).** ADR-0224 and ADR-0225 make every layout
and walk a pure function of the topology, and the gallery capture relies
on it. An engine inherits that: results may not depend on goroutine
scheduling, float reductions are folded in a fixed order, sampling is
seeded, and any vertex-order-dependent algorithm (Louvain, label
propagation) fixes its order by id. This rules out the atomic-race style
of parallel connectivity that GBBS and GAP use for speed, and is the
reason to prefer union–find or a per-chunk reduction over Shiloach–Vishkin
with races.

**Where results go (P3/P4).** The godep precedent says a metric with one
consumer belongs where the corpus can join it. An engine whose results
stay in a Go struct repeats the mistake ADR-0064 undid. Its results are
columns keyed by vertex id, and they belong in `boxer.facts` through a
facts-bound store ([explanation](../explanation/facts-bound-record-stores.md))
so the applet books can join `betweenness` onto `edges` the way they join
`weight` today. That is also the isomorphism hook ADR-0069 asks for: one
implementation serves the widget (relevance, radial centre, node radius)
and the data-engineering side (a fact table), and the closed-loop hook —
the metric, its budget and its truncation flag land as facts.

## 6 Options, costed

Line counts are estimates of hand-written Go excluding tests; tests are
assumed to roughly double them.

**O1 — SQL only.** Extend the applet books; no Go engine. Cost ~0 lines
owned. Covers bounded walks, closures, degree, quotients; PageRank and
components only to a depth bound. Cannot carry Brandes or clique
enumeration (§4.5). Keeps everything joinable. This is the status quo and
it stays the home of what it does well whatever else is chosen.

**O2 — reference `gonum/graph`.** An adapter from the network model to
`graph.Graph`, sort every output by id, expose the result columns. Cost
~300 lines. Pays an interface call and a map lookup per edge, no
parallelism, and binds the engine's representation to gonum's interfaces
— the coupling P1 names. Betweenness on 10⁵ vertices would be the
exact O(nm) form with no pivot sampling, which gonum does not offer.
Correctness is upstream's, which is the strongest argument for it, and it
stays the right *oracle* under any option.

**O3 — a first-party CSR core and a Ligra-shaped engine.** One CSR package
(uint64 ids → int32 slots, forward and reverse adjacency, optional weights,
id-sorted neighbours), one iteration package (`VertexSubset`, `EdgeMap`,
chunked parallelism with fixed-order reduction), and the algorithms of
§4.7 as thin functions over them. Cost ~2 500–3 500 lines (CSR ~300;
engine ~400; BFS, CC, SCC, PageRank, k-core, triangles ~100 each; Brandes
with sampling ~350; cliques ~250; Louvain deferred). Fastest option that
honours `CGO_ENABLED=0`; every line auditable; determinism designed in;
correctness owned and therefore to be proven by property tests against O2
and brute force. The three private CSRs become candidates to migrate to
it, which is a later decision, not this one.

**O4 — a native engine through WebAssembly.** SuiteSparse:GraphBLAS +
LAGraph, or a subset, compiled to wasm and run under wazero as Graphviz
is. Cost: a build pipeline for a C library into wasm, an FFI seam for
matrices, and the loss of the native vectorisation that justifies the
library. Cliques are not covered. Not on the front on any axis but
expressiveness.

**O5 — a dedicated analytics worker process on the FFFI2 machinery.**
Not the render client: a separate child process, spawned and owned by the
Go host, driven by the same IDL, code generators and pipe channel that
egui2 uses, and free of the frame clock. Five facts of the tree bound the
assessment.

- *The runtime has no frame of its own.* The frame cadence is egui2's
  ([ADR-0062](../adr/0062-imzero2-render-cadence.md)); the runtime under
  [`fffi2/runtime`](../../public/thestack/fffi2/runtime/) is a channel
  with fire-and-forget messages, a synchronous call that waits for its
  return values, and a flush — request/response is native to it, and a
  worker that answers one call at a time needs nothing the runtime lacks.
- *The worker can be Go or Rust.* The
  [the Go-side server stub generator](../../public/thestack/fffi2/compiletime/goserver/)
  generator emits Go-side server stubs for the same IDL (the egui2 driver
  uses them), so the process boundary and the language are separate
  decisions: a Go worker buys isolation and cancellation-by-kill with no
  second language; a Rust worker adds `rayon`, stable SIMD and the
  vectorised push/pull refinements of §4.4.
- *The process shape has a precedent.* The `clickhouse-local` pool
  ([ADR-0028](../adr/0028-chlocal-low-latency-sql-cap.md)) is a Go-owned
  pool of one-shot workers over pipes, chosen over reusable workers
  because one-shot makes cancellation a kill, framing trivial and state
  leakage impossible; every external process flows through the
  [`extbin`](../adr/0118-extbin-external-process-chokepoint.md) registry;
  the appliance image already ships a Rust host beside the Go binary and
  prices that at "small" ([ADR-0206](../adr/0206-gokrazy-appliance-image.md)).
- *The graph crosses a pipe.* Two `uint64` per edge is ~16 MB at 10⁶
  edges; at pipe bandwidth of the order of a gigabyte per second that is
  ~10 ms per job, which is as long as a whole linear-time kernel takes in
  Go (§4.8). Keeping the graph in a sticky worker keyed by a Go-issued id
  avoids the copy but re-imports the reusable-worker ledger ADR-0028
  rejected: health checks, output framing, and a cancel that invalidates
  the worker. Either way the tier pays for itself only on the
  super-linear kernels — exact betweenness, cliques — and a long job
  needs either kill-to-cancel or a submit/poll/cancel model with the
  computation off the interpreter thread, which the egui2 pattern does not
  have.
- *The browser tier.* Under [ADR-0077](../adr/0077-keelson-browser-wasm-execution.md)
  the host and renderer are wasm modules bridged over synthetic
  descriptors; a third module is the same mechanism, but wasm threads are
  out of that record's scope, so `rayon` is gone where the tier would run
  and the performance reason with it.

What it buys beyond speed: process isolation. An exponential-output
algorithm that blows its budget takes a worker down, not the host, and a
worker's memory is returned by exit. What it costs beyond lines: a Rust
worker puts a second language on the data path, which the architecture's
statement that "the Rust side renders and nothing else"
([ARCHITECTURE](../ARCHITECTURE.md)) would have to be amended to admit —
its own decision, not a leaf's — and it carries the velocity costs
ADR-0077 recorded against a Rust host (whole-monolith builds in seconds;
generated code lands correct more often in Go). Cost ~3 000 lines of
engine in either language, plus an IDL node and apply code per algorithm
(~100 lines each), a binary, its build script, an `extbin` program and a
pool (~500 lines; the chlocal pool is the template).

**O6 — a Rust engine compiled to wasm32, in-process under wazero.** The
shape [ADR-0003](../adr/0003-h3-wasm-bridge.md) chose for H3: an
upstream Rust crate behind a struct-of-arrays ABI, the `.wasm` committed
and byte-checked by a CI parity script, a pool of module instances, one
copy of the input into linear memory per batch. It keeps `CGO_ENABLED=0`,
stays in the Go process, and Rust's memory safety holds inside the
sandbox. What it cannot do: wazero implements the core spec 1.0 and 2.0 —
`v128` SIMD is in its V2 feature set, threads are not — so a module is
single-threaded and parallelism means one graph copy per pooled module;
its compiler backend is native code on amd64/arm64 but how well it lowers
`v128` is unmeasured here; and in the browser tier the Go host is itself
wasm, so the engine would run under wazero's interpreter, which ADR-0077
priced at 5–15× behind a JIT when it killed nested execution. ADR-0003
accepted the two-toolchain cost because H3 is a finished upstream crate
the tree never edits; an engine boxer writes and revises pays that cost
on every change. O6 is the right shape for wrapping a *finished* Rust
engine with the metric set, and no such crate is in the module graph.

|                           | O1 SQL only | O2 gonum | O3 first-party Go | O4 C via wasm | O5 worker process over FFFI2 | O6 Rust via wasm |
| ---                       | ---         | ---      | ---               | ---           | ---                | ---              |
| performance at §3 scale   | −           | −        | ++                | +             | ++ on super-linear kernels; − on linear ones (pipe copy) | − (single-thread per module) |
| maintainability (lines owned, seams) | ++ | ++    | −                 | −−            | − (IDL per algorithm, a binary, a pool; two languages if Rust) | − (two toolchains, ABI) |
| sovereignty (P1, no cgo)  | ++          | +        | ++                | −−            | + (owned; a Rust worker amends the render-only doctrine) | ++ (the h3 precedent) |
| correctness (oracle, determinism) | +   | ++       | + (must be earned) | +            | + (isolation; oracle across a pipe) | + |
| covers betweenness, cliques, fixpoints | −− | +    | ++                | +             | ++                 | ++               |
| runs where the data is, on every tier | ++ | ++    | ++                | +             | − (native tiers yes; browser without threads) | − (browser: interpreted) |

## 7 Recommendation

Build O3, keep O1 for what it does, and use O2 as a test oracle. The
Pareto argument in one paragraph: O1 is dominated on expressiveness — it
cannot produce the two metrics that prompted the question. O2 is the
cheapest way to get them but binds the representation to a foreign
interface and leaves both the super-linear metrics without the sampling
and parallelism they need at 10⁵ vertices. O4 fails the build gate. O6 keeps
the process boundary and the build gate but forfeits threads and, in the
browser tier, native execution, and pays a second toolchain on every
edit of code boxer itself writes.

O5 is the one option that is not dominated by O3. It has the higher
performance ceiling, it isolates an exponential-output algorithm in a
process the host can kill, and it is architecturally legitimate — a
worker kind the process taxonomy already has. It loses on maintainability
and on the browser tier, and its advantage is confined to the
super-linear kernels because the pipe copy costs as much as a linear
kernel. That confinement is what settles the order: O3 first, as the base
and as the *contract* — struct-of-arrays columns in and out, a graph
addressed by a fingerprint, budgets and truncation as plain values, no
callbacks — which is exactly an IDL-expressible surface, so O5 can slot
in behind the same Go API as an accelerator tier without redesign. O5 is
then a deferral with a trigger, not a rejection: a kernel that, measured
on a real workload after the `simd` experiment lands, exceeds the
interactive budget in Go, or an algorithm whose output growth wants a
process boundary. The worker's language is decided at that point, and
the CSR-plus-edge-map shape carries over either way.

What the ADR should decide, and this page should not: the package
location and the CSR's public shape; the algorithm set of the first cut
(the GAP six less SSSP, plus k-core and cliques, is the natural one);
the budget-and-truncation contract; that results are published as facts,
with the fact kind itself as a follow-up ADR so this one does not gate on
the schema; and the verification lane. Louvain/Leiden, weighted shortest
paths and dynamic updates are deferrals with a stated trigger.

What a trial would measure, once the engine exists: on the repository's
own import graph and a synthetic RMAT graph at 10⁵ and 10⁶ edges, the
single-thread time for each kernel against gonum (O2) and against the SQL
form where one exists (O1), then the parallel speedup at the machine's
core count, with determinism checked by bit-comparing the two. A trial
protocol under [doc/trials](../trials/) would make the numbers citable;
this page deliberately carries none.

## References

| Source | What it settles |
| --- | --- |
| Shun & Blelloch, *Ligra: A Lightweight Graph Processing Framework for Shared Memory*, PPoPP 2013 | `vertexSubset` / `edgeMap`, the sparse/dense switch |
| Beamer, Asanović & Patterson, *Direction-Optimizing Breadth-First Search*, SC 2012; [GAP `bfs.cc`](https://github.com/sbeamer/gapbs/blob/master/src/bfs.cc) | the push/pull switch, its α/β heuristic and the reference defaults |
| Besta, Podstawski, Groner, Solomonik & Hoefler, *To Push or To Pull: On Reducing Communication and Synchronization in Graph Computations*, HPDC 2017 | the atomics-versus-reads trade per algorithm; partition-aware push |
| Zhang, Yang, Baghdadi, Kamil, Shun & Amarasinghe, *GraphIt: A High-Performance Graph DSL*, OOPSLA 2018 | direction and frontier representation as a schedule separate from the algorithm |
| Grossman, Litz & Kozyrakis, *Making Pull-Based Graph Processing Performant* (Grazelle), PPoPP 2018; Grossman & Kozyrakis, *A New Frontier for Pull-Based Graph Processing* (Wedge), arXiv:1903.07754, 2019; Vandierendonck, *Graptor: Efficient Pull and Push Style Vectorized Graph Processing*, ICS 2020 | what vectorised pull and race-free vectorised push cost and buy |
| Dhulipala, Blelloch & Shun, *Theoretically Efficient Parallel Graph Algorithms Can Be Fast and Scalable*, SPAA 2018 / ACM TOPC 2021; [GBBS](https://github.com/ParAlg/gbbs) | the canonical inventory of shared-memory algorithms on the Ligra abstraction |
| Roy, Mihailovic & Zwaenepoel, *X-Stream: Edge-centric Graph Processing using Streaming Partitions*, SOSP 2013 | the edge-centric, sequential-bandwidth argument |
| Kyrola, Blelloch & Guestrin, *GraphChi*, OSDI 2012; Zhu, Han & Chen, *GridGraph*, ATC 2015 | the rest of the out-of-core line |
| Malicevic, Lepers & Zwaenepoel, *Everything you always wanted to know about multicore graph processing but were afraid to ask*, USENIX ATC 2017 | preprocessing dominates; CSR for frontier algorithms; edge arrays for single passes |
| McSherry, Isard & Murray, *Scalability! But at what COST?*, HotOS 2015 | single-thread baseline; Hilbert ordering |
| Beamer, Asanović & Patterson, *The GAP Benchmark Suite*, arXiv:1508.03619, 2015 | the six kernels and their reference algorithms |
| Fan, Raj & Patel, *The Case Against Specialized Graph Analytics Engines*, CIDR 2015 | iterative kernels are competitive in a column store |
| ClickHouse issue #107067, *RFC: Keyed recursive CTEs*, opened 2026-06-10 | append-only recursive CTE semantics and their cost for traversal |
| Brandes, *A Faster Algorithm for Betweenness Centrality*, J. Math. Sociol. 2001 | O(nm) exact betweenness |
| Bader, Kintali, Madduri & Mihail, *Approximating Betweenness Centrality*, WAW 2007; Riondato & Kornaropoulos, *Fast approximation of betweenness centrality through sampling*, WSDM 2014 / DMKD 2016; Borassi & Natale, *KADABRA*, ESA 2016 / JEA 2019 | sampled betweenness with error bounds |
| Tomita, Tanaka & Takahashi, *The worst-case time complexity for generating all maximal cliques*, TCS 2006; Eppstein, Löffler & Strash, *Listing All Maximal Cliques in Sparse Graphs in Near-Optimal Time*, ISAAC 2010 | pivoting and degeneracy ordering, O(d n 3^{d/3}) |
| Batagelj & Zaveršnik, *An O(m) Algorithm for Cores Decomposition of Networks*, 2003 | k-core peeling |
| Sutton, Ben-Nun & Barak, *Optimizing Parallel Graph Connectivity Computation via Subgraph Sampling* (Afforest), IPDPS 2018 | the GAP connected-components reference |
| Traag, Waltman & van Eck, *From Louvain to Leiden*, Sci. Rep. 2019 | community detection, deferred |
| Davis, *SuiteSparse:GraphBLAS*, ACM TOMS 2019; [LAGraph](https://github.com/GraphBLAS/LAGraph) | the linear-algebra line and its batched betweenness |
| [gonum.org/v1/gonum/graph](https://pkg.go.dev/gonum.org/v1/gonum/graph) | the Go reference implementation and test oracle |
| [ADR-0003](../adr/0003-h3-wasm-bridge.md), [ADR-0077](../adr/0077-keelson-browser-wasm-execution.md), [ARCHITECTURE § boundaries](../ARCHITECTURE.md) | the Rust-via-wasm precedent, the FFFI2 transport and the deferred Rust-host fork, the render-only doctrine |
| [ADR-0028](../adr/0028-chlocal-low-latency-sql-cap.md), [ADR-0062](../adr/0062-imzero2-render-cadence.md), [ADR-0118](../adr/0118-extbin-external-process-chokepoint.md), [ADR-0206](../adr/0206-gokrazy-appliance-image.md) | the one-shot worker pool, the frame cadence as egui2's rather than the runtime's, the external-process chokepoint, a Rust binary in the appliance image |
| [wazero `api.CoreFeatures`](https://pkg.go.dev/github.com/tetratelabs/wazero/api#CoreFeatures) | which Wasm proposals the in-process runtime implements (SIMD in V2; no threads) |

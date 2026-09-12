---
type: adr
status: accepted
date: 2026-09-12
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-12
---

# ADR-0230: the neighbour graph as the one object — a *k*-NN producer in the engine, a neighbour-embedding force model in graphview, HDBSCAN over both

## Context

`play`'s Projection tab draws a 2-D UMAP scatter of a result. It computes
sixteen shape features per leeway card, runs `github.com/nozzle/umap-go`
end to end, and plots the coordinates through implot. The neighbour graph
UMAP builds on the way is discarded; the layout is a stochastic gradient
descent with negative sampling; the spectral initialisation is a dense
eigendecomposition capped at two thousand rows, above which the init is
random; the parameters live in panel state, unrecorded and unreplayable
([play timeseries survey](../adr-background-work/play-timeseries-analysis-survey.md)
§2.2). Nothing else in the tree can read that graph, cluster on it, or lay
it out.

Two records changed what the tree can do with a graph since that panel was
written. [ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md) put a
force layout on the painter lane — Fruchterman–Reingold with Barnes–Hut
repulsion above a threshold, per-edge `Length` and `Strength` (§SD13),
declared and held pins (§SD10), auras from a group id (§SD11), a headless
scene lane — and [ADR-0229](./0229-graph-analytics-engine.md) put a CSR
container and a deterministic iteration layer under `public/analytics/graph`
with components, cores, PageRank, betweenness and cliques over it, every
result a slot-aligned column with a truncation flag.

The [Graphistry analysis](../adr-background-work/graphistry-umap-feature-engineering-analysis.md)
read the product whose whole design is "UMAP's neighbour graph is an edge
set" against those two records and found the widget and the engine already
consume such a graph; what is missing is the producer. Its §5.8 then
recorded the result that decides the shape of this record. Böhm, Berens &
Kobak (JMLR 2022) show that t-SNE, UMAP, ForceAtlas2 and Laplacian
eigenmaps are one algorithm — attraction along a *k*-nearest-neighbour
graph's edges, repulsion between all points — separated only by the ratio
of the two, and that UMAP sits where it does because negative sampling
lowers its effective repulsion to roughly `k·m/n`, an accident of the
optimiser rather than a property of the objective. The trade-off is one
sentence: stronger attraction keeps continuous structure, stronger
repulsion separates clusters and raises neighbour recall. They also read
early exaggeration as an annealing schedule from infinite attraction, whose
limit is Laplacian eigenmaps — the spectral initialisation the Projection
lane cannot afford.

So the Projection lane's UMAP and graphview's force step are the same
computation on the same object, and the tree holds two copies of it with
the wrong one exposed. Three constraints carry over. Every layout is a pure
function of its inputs and the gallery capture relies on that (ADR-0224
§SD2, ADR-0229 §SD2); umap-go's negative-sampling SGD is not. One
implementation must serve the widget and the fact table (the ADR-0069 bar,
restated in ADR-0229). And the dependency rule of
[why-boxer P1](../explanation/why-boxer.md) lets a dependency be referenced
while it stays cheap to trust; umap-go is 1 200 lines of a Python port
whose only consumer would be one call, and the tree already owns the
harder half of what it does.

## Design space (QOC)

**Question.** How does a feature matrix become a picture and a partition,
and which package owns each step?

**Options.**

- **O1** — Status quo: umap-go end to end in the Projection lane; the
  neighbour graph stays private to the library.
- **O2** — Producer as an adapter: keep umap-go for its fuzzy-graph
  construction, convert its CSR to the engine's, lay out in graphview.
- **O3** — First-party producer under the graph engine: exact *k*-NN,
  smooth-kNN scaling, fuzzy union, emitted as a `csr.Graph`; a
  neighbour-embedding force model in graphview; HDBSCAN in `algo`;
  umap-go retired.
- **O4** — Port UMAP's optimiser into graphview as the force model
  (negative sampling, per-edge epoch schedule).
- **O5** — PaCMAP first-party: three pair kinds, a phased weight schedule,
  PCA initialisation.

**Criteria.**

- **C1** — Determinism: bit-identical at any worker count, no sampled
  repulsion, so the gallery capture and the golden tests hold.
- **C2** — One object: the graph the widget lays out is the graph the
  engine clusters and the fact table will key.
- **C3** — Sovereignty per P1 / ADR-0229 C3: lines owned versus a
  dependency whose trust must be maintained.
- **C4** — Interactive at the Projection lane's row cap, ten thousand
  rows, on one machine.
- **C5** — Global structure: where a cluster sits relative to the others.
- **C6** — Reach: the same layout serves any declared graph, not only a
  *k*-NN graph.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | +  | ++ | −− | −  |
| C2 | −− | +  | ++ | +  | +  |
| C3 | −  | −  | ++ | +  | +  |
| C4 | +  | +  | +  | +  | +  |
| C5 | +  | +  | +  | +  | ++ |
| C6 | −− | ++ | ++ | ++ | −  |

O1 fails the two criteria that prompted the question. O4 reproduces the
accident the paper diagnosed: negative sampling is where UMAP's
non-determinism and its hidden attraction both come from, and a widget that
already sums repulsion exactly has no reason to import it. O2 is O3 with
the graph half rented: the conversion is an afternoon, but the dependency
stays on the data path for one call whose inputs and outputs the tree
already has types for, and the widget half is the same work under both.
O5 is the one option that beats O3 on a criterion — global structure —
and it does so with a second, sampled edge set and a schedule, which is
expressible as a variant of O3's producer and a `Strength` schedule on the
same force model rather than a separate algorithm; it is deferred with a
trigger (§SD7), not rejected. O3 wins on the two criteria the tree's own
records already fixed and loses nowhere; its cost is lines owned, and the
lines are the ones ADR-0229 already decided to own for graphs.

## Decision

We build the neighbour graph as a first-party producer under the graph
engine, lay it out in graphview under a second force model whose one knob is
the attraction-to-repulsion ratio, cluster it with HDBSCAN in the engine, and
move the Projection lane onto those three; umap-go is retired once the
first-party producer has been checked against it.

**SD1 — A `knn` package under `public/analytics/graph` produces the
neighbour graph as a `csr.Graph`.** Input is a row-major `float32` matrix
with a row per entity and an id per row; options are `K`, a metric
(Euclidean and cosine in the first cut, as an enum, not an interface),
`LocalConnectivity`, `SetOpMixRatio` and a row budget. The producer runs an
exact *k*-NN over rows — brute force, chunked across workers with per-worker
scratch, each row's candidates ordered by distance then by slot so ties are
a function of the input — then UMAP's smooth-kNN scaling (a per-row binary
search for the bandwidth that makes the membership sum `log₂ k`, with the
nearest neighbour's distance as the offset), then the membership weights
`exp(−(d − ρ)/σ)`, then the fuzzy union `W + Wᵀ − W∘Wᵀ`. The result carries
the undirected, weighted `csr.Graph` (weights in `(0, 1]`), the per-row
`Sigmas`, `Rhos` and the *k*-th-neighbour distance as `CoreDist`, the raw
neighbour lists as two slot-aligned `n×K` slices (`Indices`, `Dists`) for
the algorithms that want distances rather than memberships, a
`DistanceGraph()` that builds the same topology weighted by distance on
demand, and a `Truncation` naming the row budget when the input was cut.
Brute force is the first cut because the budget is ten thousand rows and
the exact result is what the oracle can check; an approximate index is
deferred with its trigger (§SD7). Distances are computed in `float32`
with squared Euclidean where the metric allows it.

**SD2 — graphview gains a neighbour-embedding force model, selected on
`ForceParams`, with one knob.** `ForceParams.Model` is `ModelFR` (the
default, unchanged to the bit) or `ModelNeighborEmbedding`. Under the
second the step uses the t-SNE kernel: attraction along each declared edge
of `Strength · q_ij · (x_j − x_i)` with `q_ij = 1/(1 + d²_ij)`, the
strengths normalised to sum to one over the edge set per step; repulsion
between all pairs of `q²_ij (x_i − x_j) / Z` with `Z = Σ q_kl`, both sums
accumulated by the same Barnes–Hut walk that the FR model uses — the tree
already sums a kernel over pseudo-bodies, and `Z` is one more accumulator
on the same walk; and `Exaggeration` multiplying the attraction, default 1.
The paper's correspondences give the knob its meaning: about 1 draws
t-SNE, about 4 draws UMAP, about 30 draws ForceAtlas2, and larger tends to
Laplacian eigenmaps. A schedule — `ExaggerationStart` and
`ExaggerationSteps`, the early-exaggeration reading — anneals from a high
value to `Exaggeration` over the first steps, and is what `FastForward` and
the play panel's settle budget run. The integrator stays the widget's
(`Dt`, `Damping`, `MaxStep`, the settle predicate); positions stay world
units, so the kernel is applied to positions scaled by the ideal edge length
`k` of §SD2 of ADR-0224, which keeps `KScale` meaningful and the fit
contract unchanged. An edge without a declared `Strength` has 1. The model
is a layout for any declared graph, not only a *k*-NN graph: a corpus graph
under it reads as a neighbour embedding of its topology, which is the
ForceAtlas2 end of the same spectrum ADR-0224 §SD2 already offers.

**SD3 — HDBSCAN joins `algo`.** It takes a `csr.Graph` weighted by
distance and a `CoreDist` column — the producer's `DistanceGraph()` and
`CoreDist` — plus `MinClusterSize` and the usual budget. It raises each arc
to the mutual reachability distance `max(core_i, core_j, d_ij)`, runs
Kruskal over the arcs in that order with union–find recording merge
heights, condenses the resulting single-linkage hierarchy by
`MinClusterSize` and extracts clusters by stability, the excess of mass
over `λ = 1/d`. It returns a label per slot, `−1` for noise, a stability
per cluster, and a `Truncation`. Over a *k*-NN graph rather than the
complete graph the MST disconnects only what the *k*-NN graph disconnects;
that is the documented approximation and the reason the producer's row
budget is also this algorithm's. HDBSCAN is chosen over the ε-DBSCAN the
reference product documents because an embedding's scale is arbitrary and
`MinClusterSize` is a statement about the data where ε is not. ADR-0229
§SD7's Louvain deferral is untouched: Louvain clusters a topology and
needs no metric, HDBSCAN clusters a metric space and needs no topology
beyond the neighbour graph; a consumer with edges asks for the first, one
with a matrix for the second.

**SD4 — The Projection lane becomes a graphview consumer.** The tab keeps
its feature extractor, its row cap and its row selection, and replaces the
implot scatter with a graphview whose nodes are the rows, whose edges are
the producer's graph with `Strength` set to the membership weight, whose
layout is `ModelNeighborEmbedding` with the annealing schedule under the
settle budget of [ADR-0227 (play panel)](./0227-play-graphview-panel.md)
§SD10, whose fill is the feature bucket the scatter coloured by, and whose
aura ids are the HDBSCAN labels. The spectral-initialisation cap disappears
with the eigensolver: initial placement is the widget's deterministic
hashed placement and the schedule does the rest (§SD2). Whether that
reaches the picture umap-go's spectral init reached is the trial named in
the verification plan, not an assumption of this record. The parameters —
`K`, the metric, `Exaggeration`, `MinClusterSize` — move from panel state
into the tab's controls and status line as ADR-0227 (play panel) does for
its layout; recording them as query settings is the featurization record's
question, not this one's.

**SD5 — umap-go is retired in two steps.** First it becomes the test
oracle: a property test over seeded matrices compares the producer's
`Sigmas`, `Rhos` and arc weights against `FuzzySimplicialSet` within
`float32` tolerance, and it is imported by tests only — the shape ADR-0229
chose for `gonum/graph`. Second, once that comparison has run at the sizes
the lane uses, the oracle's outputs for a handful of seeded inputs are
committed as golden fixtures under the package's testdata, the test reads
the fixtures instead of the library, and the module leaves `go.mod`. The
one capability lost with it — placing a new batch into a fitted embedding
— is re-derived on this record's own terms (§SD7) when a consumer wants
it.

**SD6 — Determinism and budgets are inherited, not re-decided.** The
producer's parallel rows and the force model's Barnes–Hut walk follow the
chunked, per-worker-scratch shape of ADR-0224 §SD6 and ADR-0229 §SD2, so
the result at one worker equals the result at `GOMAXPROCS`. The effective
repulsion is the declared ratio, not `k·m/n`. Every function takes a
context and a budget and returns a `Truncation` (ADR-0229 §SD4). The
producer's result is IDL-expressible per ADR-0229 §SD8 — plain values in,
slot-aligned columns out — so the worker tier deferred there covers it.

**SD7 — Deferred, with triggers.** An approximate *k*-NN index
(NN-descent or a random-projection forest) when a consumer's rows exceed
the exact producer's interactive budget. PaCMAP's mid-near pairs — a
seeded, weak, annealed second edge set from the same producer, a
`Strength` schedule on the same model — when a consumer reads where a
cluster sits relative to the others and the trial shows the exaggeration
schedule alone does not answer it. Placing a new batch against a fitted
graph — the producer's bipartite form, new rows' neighbours among the old,
old nodes declared pinned, new nodes free, a few steps — when a consumer
has batches. Supervision — a label graph intersected with the data graph,
expressed as a `Strength` multiplier on edges whose ends share a label —
when a consumer names a target column. Normalised compression distance as a
metric for text columns, with the pairwise cost that entails, when the
featurization record measures it against hashed n-grams. Persisting the
neighbour graph and the labels as facts, keyed by the matrix's content
fingerprint, in the follow-up ADR-0229 §SD5 already names. A `simd` inner
loop for the distance rows under the same trigger as ADR-0224 §SD6.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API under `public/` | added: a `knn` package under [`public/analytics/graph`](../../public/analytics/graph/); `algo.HDBSCAN`; `graphview.ForceParams.Model`, `.Exaggeration`, `.ExaggerationStart`, `.ExaggerationSteps` and `ModelE` | the Projection lane as first consumer of all three; no existing exported API reshaped |
| `go.mod` | `github.com/nozzle/umap-go` moves to test-only (SD5 step one), then leaves (step two) | `public/semistructured/leeway/card` drops `RunUMAP`, `UMAPOptions` and the spectral-init constants when the lane moves |
| `play` Projection tab | replaced: implot scatter by a graphview; the row-selection channel unchanged | the help corpus entry for the tab; the screenshot tour capture |
| `boxer.facts` schema | unchanged by this ADR | the fact kinds are the follow-up named in ADR-0229 §SD5 and §SD7 here |

## Alternatives

- **Status quo (O1).** Discards the graph, is non-deterministic, and holds
  a dependency for the half of its work the tree already owns. Killed.
- **Adapter over umap-go's graph (O2).** Same widget work, dependency kept
  on the data path for one call; kept as the oracle instead (§SD5).
- **UMAP's optimiser in graphview (O4).** Imports the accident the paper
  diagnosed; a widget that sums repulsion exactly has no use for negative
  sampling. Killed.
- **PaCMAP (O5).** The better answer on global structure; deferred as a
  variant of the producer and a `Strength` schedule, not a second
  algorithm (§SD7).
- **A SQL-side *k*-NN.** A quadratic self-join and a per-row binary search
  ClickHouse expresses badly; the engine expresses both in a loop. Killed,
  as the Graphistry analysis §5.1 already recorded.
- **ε-DBSCAN as the first clustering.** The parameter a user cannot set on
  an embedding whose scale is arbitrary; HDBSCAN's `MinClusterSize` is a
  statement about the data. A half-day fallback if the hierarchy proves
  unwanted, not the decision.
- **Keeping the spectral initialisation with a sparse eigensolver.** Fixes
  the cap by owning an eigensolver; the annealing schedule reaches the same
  limit with code the widget already runs. Deferred to the trial's verdict.
- **A third layout enum value instead of a `ForceParams.Model` field.**
  `LayoutE` mirrors the retired binding's numbering and names a placement
  algorithm; the kernel is a parameter of the force layout, and a
  consumer's `LayoutForceDirectedCG` keeps working with either kernel.

## Consequences

### Positive

- One graph object serves projection, layout, clustering and, after the
  follow-up, the fact table — the isomorphism bar met for the projection
  as ADR-0229 met it for metrics.
- The projection becomes interactive: drag, pin, select, aura and fit are
  the widget's, and the row selection channel is unchanged.
- A single knob replaces `min_dist`, negative-sample rate and repulsion
  strength, and its values have published meanings.
- The gallery capture and the golden tests hold under parallelism because
  no step samples.
- One module dependency and a dense eigensolver leave the tree.

### Negative

- Roughly a thousand lines of owned Go across three packages, plus tests
  and a trial, whose correctness the tree earns against an oracle it then
  removes.
- The neighbour-embedding kernel is a second force model in the widget;
  a regression in the shared Barnes–Hut walk now reaches two layouts.
- The annealing schedule is a claim from the literature until the trial
  measures it against the spectral initialisation it replaces; if it falls
  short, the deferred sparse eigensolver returns to the table.
- HDBSCAN over a *k*-NN graph is an approximation of HDBSCAN over the
  complete graph; a consumer reading its labels should know the graph
  they were computed on.

### Neutral

- The feature extractor and its sixteen card features are untouched; the
  featurization of arbitrary result columns is the separate lane gap the
  Graphistry analysis ranks second and wants its own record.
- The FR model and every existing consumer of graphview are unchanged to
  the bit; the new model is opt-in.

## Migration — Tier 1

- **Breaks.** `card.RunUMAP`, `card.UMAPOptions` and the spectral-init
  constants are removed when the Projection lane moves (SD4); they have no
  consumer outside `apps/play`.
- **Path.** In order: the producer with the oracle test (SD1, SD5 step
  one); the force model with its unit and scene tests (SD2); HDBSCAN with
  its brute-force oracle (SD3); the Projection lane onto the three (SD4);
  the golden fixtures and the `go.mod` removal (SD5 step two). Each step
  is buildable and shippable on its own.
- **Regeneration.** None: no IDL, no generated dispatch; the widget's
  paint surface is unchanged.
- **Old shape.** The implot scatter is removed outright with SD4; there is
  no second consumer to deprecate for.

## Verification plan — Tier 1

- **Lane.** Default `go test`. Producer: a property test
  ([`pgregory.net/rapid`](https://pkg.go.dev/pgregory.net/rapid)) over
  random small matrices against a brute-force *k*-NN and a scalar
  re-derivation of the smooth-kNN search; the umap-go oracle comparison of
  SD5 within `float32` tolerance; the one-worker-versus-many bit
  comparison. Force model: the exact pair sum against the Barnes–Hut walk
  for both kernels; a synthetic dataset (Gaussian blobs and a Swiss roll)
  under which raising `Exaggeration` lowers *k*-NN recall and joins the
  roll, the direction the paper reports; the bit comparison across worker
  counts; a headless scene that declares a *k*-NN graph and settles under
  the schedule. HDBSCAN: a property test against a brute-force
  single-linkage hierarchy on small graphs, and the noise label on a
  planted-clusters fixture. Projection lane: the panel's existing scene
  tests moved to the widget, and the screenshot tour capture.
- **Trial.** A trial under [`doc/trials`](../trials/) measures, at the
  lane's row cap, the hashed-placement-plus-annealing layout against
  umap-go's spectral-init layout on *k*-NN recall and on a global-structure
  score, and the producer's build time against `FuzzySimplicialSet`. Its
  §0 claim is what SD4's "the cap disappears" rests on; until it runs, the
  claim is the paper's, not the tree's.
- **What would fail.** A parallel result that differs from the serial one;
  a producer weight outside the oracle's tolerance; a layout under
  `Exaggeration` that does not move in the reported direction; an HDBSCAN
  label that disagrees with the brute-force hierarchy.
- **Gap.** The oracle bounds the property tests at tens of rows; the trial
  checks the layout's quality at scale, not its correctness, and there is
  no ground truth for a picture beyond the two scores it reports.
  Acceptable because every step is size-independent in its logic and the
  scores are the ones the literature uses.

## Status

Accepted 2026-09-12.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-09-12 — the producer shipped (SD1, SD5 step one)

[`public/analytics/graph/knn`](../../public/analytics/graph/knn/) is in the
tree: exact brute-force *k*-NN with per-worker scratch and a bounded heap
ordered by distance then slot, umap-learn's smooth-kNN search and bandwidth
floor, membership weights, the fuzzy union with the mix ratio, the row
budget as `algo.LimitRows`, and `DistanceGraph()`. The verification lane
runs: a property test against an O(n²) neighbour oracle and a union
re-derived from the result's own σ and ρ, the one-worker-versus-many bit
comparison above the engine's parallel floor, the budget and error paths,
and the umap-go comparison composed from that library's exported stages
(brute-force *k*-NN, `SmoothKNNDist`, `ComputeMembershipStrengths`,
`FuzzySetUnion` of the matrix and its transpose) rather than its top-level
fit, so the comparison is against the construction SD1 names. umap-go is
now imported by tests only.

Two refinements found in implementation:

- **A membership that underflows is no arc.** The reference eliminates
  explicit zeros after the union; here a union weight that rounds to zero
  in `float32` is dropped before the CSR is built, so a row whose
  bandwidth was floored contributes no arc to a far neighbour. The oracle
  tests encode the rule.
- **The row itself counts in the means.** umap-learn's bandwidth floor
  averages the row's `n_neighbors` distances including its own zero; `K`
  here excludes the row, so the floor divides by `K+1`. Without it the
  σ comparison drifted at the floor.

On one laptop (2026-09-12, an 8-thread mobile part; read the order of
magnitude), ten thousand rows of sixteen features at fifteen neighbours
built in about half a second on all cores and one and a half seconds on
one, with under twenty megabytes allocated — the brute-force O(n²·d) at
the lane's cap, and the reason the approximate index stays deferred. A
trial is where the figure becomes citable.

### 2026-09-12 — the force model shipped (SD2)

graphview carries `ForceParams.Model` with `ForceModelFR` (the zero value,
the existing step to the bit) and `ForceModelNeighborEmbedding`:
`Exaggeration`, `ExaggerationStart` and `ExaggerationSteps`, the t-SNE
kernel on the existing Barnes–Hut tree with the normaliser accumulated per
node and folded in slot order, an exact pair sum below the tree threshold,
`Metrics.Exaggeration` reporting the schedule's current value, and a settle
predicate that waits for the schedule. `ResetLayout` restarts the schedule;
a topology change does not, so a consumer that swaps its graph calls it.
The lane runs: exact-versus-tree for both displacement and normaliser,
serial-versus-parallel bit equality, the schedule's endpoints and
monotonicity, a headless scene under a schedule with `PauseOnSettle`, and
the trade-off test over the `knn` producer's graph — on three Gaussian
blobs the ten-neighbour recall fell from about 0.54 at exaggeration 1 to
about 0.07 at 30, and on a helix the distance correlation rose from about
0.20 to 0.28, the directions Böhm, Berens & Kobak report. [ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md)
carries a dated entry pointing here.

Three refinements found in implementation:

- **The learning rate is folded into the force.** t-SNE's gradient is
  O(1/n) per node and its implementations compensate with a learning rate
  of n/12 (Belkina et al. 2019); the widget's integrator is `Dt·Damping`
  with no learning rate. The model multiplies the gradient by `n` and a
  constant chosen so the default integrator moves a node about a twelfth
  of the way to a neighbour per step, so `Dt` and `Damping` keep one
  meaning across both models rather than the model growing a knob of its
  own.
- **Distances enter the kernel in units of the ideal edge length.**
  `q = 1/(1 + (d/k)²)` with `k = sqrt(area/n)·KScale`, so the picture
  fills the canvas as the FR layout does and `KScale` keeps its meaning;
  the deltas stay world units, so no scale returns on the way out.
- **`Length` is ignored under the model.** The kernel has one scale and a
  per-edge ideal length has no reading in it; `Strength` is the affinity
  and is normalised over the edge ends so a neighbour graph's memberships
  sum to one as t-SNE's affinities do.

On one laptop (2026-09-12, an 8-thread mobile part; read the order of
magnitude), one full step — tree, repulsion, attraction, integration — on
the fifteen-neighbour graph of ten thousand rows took about five
milliseconds on all cores, so a thousand-step schedule runs in seconds
under `FastForward` and a live layout steps a few times per frame; the
tree repulsion alone at twenty thousand bodies took about nine
milliseconds. The trial named in the verification plan is where the
schedule is measured against the spectral initialisation it replaces.

### 2026-09-12 — HDBSCAN shipped (SD3)

`algo.HDBSCAN` takes a distance-weighted `csr.Graph` and a core-distance
column — the producer's `DistanceGraph()` and `CoreDist` — with
`MinClusterSize` and `AllowSingleCluster`, and returns a label per slot
with `−1` for noise, a membership probability per slot, a stability per
label, and the single-linkage hierarchy's merge heights. Mutual
reachability is applied per arc, Kruskal runs over the arcs in
(distance, slot pair) order building the dendrogram as it goes, the tree
is condensed by `MinClusterSize` from a virtual root at λ = 0 whose
children are the forest's components, and clusters are selected by excess
of mass with the root excluded unless asked for. The lane runs: a property
test whose merge heights equal a brute-force single-linkage agglomeration
under mutual reachability on random small point sets, with the label
invariants (dense labels, every cluster at least `MinClusterSize`, noise at
probability zero); planted blobs with far outliers on the complete graph;
disconnected components as clusters; the option and error paths; and the
end-to-end run over the producer's graph.

Two refinements found in implementation:

- **A zero distance takes a finite λ.** The reference sets λ to infinity
  for coincident points, which makes a stability sum infinite; here a zero
  merge distance takes twice the largest finite λ, so coincident points
  still merge first and every stability is a number.
- **A bridge point over a neighbour graph is a low-probability member,
  not noise.** Over the complete graph an outlier attaches last and falls
  out of the root. Over a *k*-neighbour graph it has *k* neighbours by
  construction and may be the only edge between two dense groups, so it
  sits inside one group's subtree and takes that label, with a probability
  near zero because it fell out early. This is the documented
  approximation of SD3 made concrete: a consumer that wants the
  complete-graph reading of an outlier reads `Probability`, and the
  Projection lane should map it to the aura's alpha or drop members under
  a threshold rather than trust the label alone.

On one laptop (2026-09-12, an 8-thread mobile part; read the order of
magnitude), HDBSCAN over the fifteen-neighbour graph of ten thousand rows
took about thirty milliseconds single-threaded with under two megabytes
allocated — the sort of the arcs dominates, and nothing in it wants
parallelism at this size.

### 2026-09-12 — the Projection lane moved (SD4, SD5 step one complete)

`play`'s Projection tab is a graphview consumer. The goroutine keeps the
card feature extractor, the uniform subsample and the row cap, and runs
the producer and HDBSCAN in place of the UMAP fit; it publishes a
versioned result — the neighbour graph, the clustering, a slot-to-row
map and the per-slot feature columns — and computes no coordinate. The
render thread builds the declaration once per result and once per
colouring change: a node per slot filled by the colour-by feature's
viridis bucket, the cluster as an aura id for members whose probability
clears a floor, an edge per neighbour pair with the membership as
`Strength`. The widget runs `ForceModelNeighborEmbedding` under a
schedule from 12 down to the slider's value over 250 steps, with a
hundred-step fast-forward on a new result, the play panel's freeze rule,
and `PauseOnSettle`. The row selection channel is unchanged in both
directions: a Table click selects the node, a node click publishes the
row. The knobs — neighbours, minimum cluster size, exaggeration — are
the tab's controls; the status line carries the counts, the noise, the
settle state and the schedule's current exaggeration. `card.RunUMAP`,
its options and the spectral-initialisation cap are gone, and
`github.com/nozzle/umap-go` is imported by the producer's oracle test
only. The help corpus entry is rewritten.

One refinement: the two run knobs are float sliders rounded to integers,
because the bindings carry a value binding for the float slider and none
for the integer one; a binding for the integer slider is a small IDL
addition for later, not this record's.

The implot scatter, its bucketed series and the click-to-nearest-point
translation are removed with no second consumer to deprecate for. The
tab was not exercised live in the session that made the change; the
scene coverage is the widget's (SD2's scene test) and the declaration
builder's unit test, and a live pass against a result of a few thousand
rows is the first thing to do before the trial the verification plan
names.

### 2026-09-13 — umap-go retired (SD5 step two)

The producer's oracle test reads three golden fixtures under the package's
testdata — Euclidean, cosine, and Euclidean with fractional local
connectivity, three hundred rows of eight features at ten neighbours —
each carrying its input matrix, the reference's σ and ρ per row and the
union's arcs, written once from umap-go's exported stages by a generator
that was removed in the same change. `github.com/nozzle/umap-go` is out of
`go.mod` and `go.sum`; nothing in the tree imports it. Regeneration is
documented in the test: re-add the module, restore the generator from
history, run with the environment variable it names. The fixtures are
about a quarter of a megabyte of JSON; a compact encoding was not worth a
reader's trouble at that size. Every Migration step of this record has
now shipped; the trial the verification plan names is the one open item.

## References

- [ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md) — the widget:
  §SD2 force parameters, §SD6 Barnes–Hut, §SD10 pins, §SD11 auras, §SD13
  edge strength; the headless scene lane.
- [ADR-0227 (play panel)](./0227-play-graphview-panel.md) — the settle
  budget, the aura-from-group mapping, the panel's deferrals.
- [ADR-0229](./0229-graph-analytics-engine.md) — the engine: §SD1 CSR,
  §SD2 determinism, §SD4 budgets, §SD5 columns and the facts follow-up,
  §SD7 the Louvain deferral, §SD8 the IDL-expressible surface; the
  gonum-as-oracle shape SD5 copies.
- [Graphistry analysis](../adr-background-work/graphistry-umap-feature-engineering-analysis.md)
  — §2.2 what the neighbour graph is, §5.1 the producer as an engine gap,
  §5.8 the three results this record acts on, §6 the ranked items.
- [play timeseries analysis survey](../adr-background-work/play-timeseries-analysis-survey.md)
  — the Projection panel as counter-precedent.
- [why-boxer P1](../explanation/why-boxer.md) — the dependency rule.
- Böhm, Berens & Kobak, *Attraction-Repulsion Spectrum in Neighbor
  Embeddings*, JMLR 23(95), 2022; arXiv:2007.08902.
- McInnes, Healy & Melville, *UMAP: Uniform Manifold Approximation and
  Projection for Dimension Reduction*, arXiv:1802.03426, 2018 — the
  smooth-kNN scaling and the fuzzy union SD1 re-derives.
- van der Maaten & Hinton, *Visualizing Data using t-SNE*, JMLR 9, 2008;
  van der Maaten, *Accelerating t-SNE using Tree-Based Algorithms*, JMLR
  15, 2014 — the kernel and the Barnes–Hut form of SD2.
- Campello, Moulavi & Sander, *Density-Based Clustering Based on
  Hierarchical Density Estimates*, PAKDD 2013; McInnes & Healy,
  *Accelerated Hierarchical Density Based Clustering*, ICDM Workshops 2017
  — SD3.
- Wang, Huang, Rudin & Shaposhnik, *Understanding How Dimension Reduction
  Tools Work*, JMLR 22(201), 2021; arXiv:2012.04456 — the deferred
  mid-near pairs of SD7.
